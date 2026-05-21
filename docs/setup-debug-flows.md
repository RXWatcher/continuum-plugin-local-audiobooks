# Setup and debugging — continuum-plugin-local-audiobooks

Operator-only plugin. The README and `operations.md` cover the happy
path. This document focuses on first-time bring-up and the symptoms
that show up when something goes wrong.

Plugin ID: `continuum.local-audiobooks`
Manifest version: see `cmd/continuum-plugin-local-audiobooks/manifest.json`.

## First-time setup checklist

1. Create the Postgres role and `local_audiobooks` schema (see
   `operations.md` §1). Confirm the role can `CONNECT`, `USAGE` on the
   schema, and `CREATE` inside it — migrations create tables on the
   first Configure.
2. Mount audiobook directories so they are visible **from the plugin
   runtime**, not the host. The admin "Browse" tool uses the plugin's
   own `os.ReadDir`, so what you see there is what the scan will see.
3. Set `database_url` and `library_paths` in plugin config. Both are
   required for Configure to succeed. Library paths added via
   `POST /admin/library-paths` are stat-checked first.
4. (Optional) Set metadata source and region preferences.
5. Trigger an initial scan from the admin UI (or
   `POST /admin/scan`). The 6-hour scheduled task does NOT fire on
   first install.
6. In the audiobooks portal, add this plugin as a library source.

## Communication map

- **Inbound from host**: `Configure`, `GetManifest`, and `HttpRoutes.Handle`
  RPC over the SDK gRPC channel. The host plugin proxy translates
  `/api/v1/*` and `/admin/*` requests into `Handle` calls.
- **Outbound to host**: none; the plugin owns its own schema.
- **Inbound from audiobooks portal**: `/api/v1/catalog*`,
  `/api/v1/browse/*`, `/api/v1/cover/{book_id}/{size}`,
  `/api/v1/stream/{book_id}/{file_idx}` (token-gated, single
  `file_idx=0`).
- **Outbound HTTPS**: per-source domains for Audnexus, AudiMeta, iTunes,
  Storytel, BookBeat, Audioteka, Audiobookcovers. Each request has a
  10 s timeout, a 1 MiB body cap (`SoftLimit`), and a custom UA derived
  from the manifest version.

## Route surfaces

Routes registered on the chi handler (see
`internal/server/server.go`):

- `GET /api/v1/catalog`, `/api/v1/catalog/{id}`,
  `/api/v1/catalog/search`, `/api/v1/catalog/libraries`
- `GET /api/v1/browse/authors`, `/api/v1/browse/genres`
- `GET /api/v1/stream/{book_id}/{file_idx}`,
  `GET /api/v1/cover/{book_id}/{size}` (public route, token-gated)
- `GET /api/v1/requests/{externalId}` (stub — this backend does not
  implement requests)
- Admin: `/admin`, `/admin/scan`, `/admin/scan/status`,
  `/admin/library-paths` (CRUD), `/admin/filesystem/browse`,
  `/admin/config` (GET/PUT), `/admin/metadata/backfill`.

On the standalone listener only `/api/v1/stream/...` and
`/api/v1/cover/...` answer; the chi `NotFound` handler returns 404 with
`{"error":{"code":"not_allowed"}}` for everything else.

## Debugging runbook

### Catalog appears empty after a scan

1. `GET /admin/scan/status` — is there a `FinishedAt` for the most
   recent event? Is `ErrorText` set?
2. Confirm `library_paths` are visible inside the runtime (the admin
   "Browse" tab walks the same `os` package the scanner uses).
3. Confirm the files have `.m4b` or `.mp3` extensions. Other formats
   (FLAC, AAC, OGG) are silently ignored.
4. Confirm files are regular (not symlinks) and readable by the plugin
   process. Symlinks are rejected at ingest time by design.
5. Look for `scan: skip unparseable file` / `scan: skip unstattable
   file` WARN logs. Each line names the offending path.

### Scan runs but a file is missing

- Most likely the file's extension is uppercase but with non-Latin
  characters or a leading dot/space confusion — `supportedExtension`
  is case-insensitive but only matches `.m4b` and `.mp3` literally.
- Or the file is a symlink: `info.Mode().IsRegular()` rejects
  non-regular entries.
- Or the file is unreadable by the plugin user (POSIX permissions). The
  WARN log line will name it.

### Scan is slow

- Per-file work is `(stat) + (parse) + (DB upsert) + (optional enqueue)`.
  The parser walks MP4 atoms / ID3 tags directly; cover extraction is
  the heaviest step.
- Disable `scan_inline_enrich` if enabled — synchronous enrichment
  serializes the scan behind dozens of HTTP calls.
- Reduce `metadata_sources_enabled` if `Search` fan-out is the issue
  (it does not affect the scan path itself, only the gRPC search RPC).

### Streams return 401/404

- 401 on `/api/v1/stream/...` means the token did not verify. Common
  causes: secret mismatch with the audiobooks portal, token expired
  (5-minute TTL), wrong `book_id` or `file_idx` claim, or `aud` is not
  `audiobook_backend`.
- 404 with `file index not found` means the client requested
  `file_idx != 0`. Local audiobooks are always single-file.
- 404 `not found` means the audiobook ID is not in this plugin's catalog
  (wrong installation ID selected in the portal, or the row was
  soft-deleted by a scan).
- 500 `file not readable` means the row exists but the file at
  `audiobook.path` is no longer accessible from the runtime.

### Streams work via the portal but fail on the standalone listener

- `stream_signing_secret` mismatch: both plugins must hold the same
  base64 value. Decoded length must be exactly 32 bytes.
- Reverse proxy is rewriting the `?token=` query param, or stripping
  the path before `/api/v1/`.
- The reverse proxy is sending `X-Continuum-*` headers to the public
  port — the standalone listener strips them, but if the proxy is also
  the host proxy you may see admin/auth-flavored bugs.
- The listener address changed without a restart. `standalone_http_listen`
  is bound once per process; the admin logs warn on mismatch but
  cannot rebind without restart.

### Metadata enrichment never completes

- Confirm `metadata_scan_source` is set to a valid scan-capable source.
  `audiobookcovers` is rejected at Configure time.
- Inspect `metadata_enrichment_job`:
  ```sql
  SELECT audiobook_id, status, attempts, run_after, last_error
  FROM local_audiobooks.metadata_enrichment_job
  ORDER BY run_after DESC LIMIT 20;
  ```
  - `status='pending'` with `run_after` in the future → backoff.
  - `status='failed'` → exceeded 5 attempts. `last_error` has the cause.
  - `status='pending'` with `attempts>0` and old `run_after` → lease
    held by a worker; check whether `metadata_enrichment_worker` is
    actually running (cron tick should appear in logs every minute).
- The worker uses a single source. If the source is offline, every job
  retries with backoff and eventually fails — switch
  `metadata_scan_source` to another option.

### 429 / 503 from a metadata source

Upstreams that throttle or sag return non-2xx; `sources.HTTPClient.GetJSON`
maps them to a generic `"source: GET <url> status N"` error:

- **Aggregator path (`Search`)** — the error is swallowed; that source
  contributes zero candidates to the result set. Other sources still
  fan out. No retry, no cache write.
- **Enrichment worker** — the job's attempt counter increments and the
  row is rescheduled with exponential backoff (1, 2, 4, 8, 16 min). At
  5 attempts the row is marked `failed`.
- **404** is handled differently: it is wrapped as `ErrNotFound`, the
  worker treats it as "no change" (job completes), and the aggregator
  caches a `not_found` row for `metadata_cache_ttl_days` to suppress
  repeat requests.

If a source starts 429-ing repeatedly, lower `metadata_rate_limit_rps`
or disable that source via `metadata_sources_enabled` until the
upstream stabilises.

### Configure fails on startup

- `database_url is required` — config is missing the key.
- `stream_signing_secret is required when standalone_http_listen is
  set` — both must be present together.
- `stream_signing_secret: decoded secret must be 32 bytes` — value is
  not a valid 32-byte base64 string. Regenerate via the portal admin.
- `metadata_scan_source "audiobookcovers" is not a valid scan-capable
  source` — pick a different default.
- `pgxpool: ...` — DSN is unreachable or wrong; verify `search_path`
  is set to `local_audiobooks`.
- `migrate: ...` — the configured role cannot create tables. Check
  schema ownership.

## Verifying after changes

1. Restart the plugin installation from the Continuum admin.
2. Open `/admin` on this plugin and check the status strip: paths,
   last scan, stream secret, active scan.
3. Trigger a scan, wait, and confirm it completes via
   `/admin/scan/status`.
4. From the audiobooks portal, browse this backend's library and play
   a title end-to-end.
5. After a config change to the standalone listener, confirm the
   plugin process restarted (the warn log line about a listener
   mismatch tells you it didn't).
