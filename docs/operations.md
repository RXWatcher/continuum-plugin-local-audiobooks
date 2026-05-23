# Operations runbook — silo-plugin-local-audiobooks

Operator-only plugin. The README covers manifest, capabilities, config
keys, and the source list. This runbook covers the day-to-day workflows
the README intentionally skips: schema bootstrap, scan + enrichment
controls, the standalone listener, and recovery.

## 1. Postgres schema bootstrap

The plugin runs its own migrations on startup against the
`local_audiobooks` schema. Provision the role and schema before the
first Configure call:

```sql
CREATE ROLE plugin_local_audiobooks LOGIN PASSWORD '<set-something-strong>';
CREATE SCHEMA local_audiobooks AUTHORIZATION plugin_local_audiobooks;
```

The DSN must set `search_path=local_audiobooks` so migrations target the
right namespace. Example:

```text
postgres://plugin_local_audiobooks:<pwd>@db.internal:5432/silo?search_path=local_audiobooks&sslmode=disable
```

Migrations are idempotent and tracked in `schema_migration` inside that
same schema; rerunning against an already-migrated schema is a no-op.

Migrations currently shipped: `0001_init`, `0002_metadata`,
`0003_content_sig`, `0004_app_config` (see
`internal/migrate/files/`).

## 2. Library paths and `media_type`

`library_paths` accepts a JSON array of absolute directories visible
**from inside the plugin runtime** (NOT the operator laptop). Paths are
stat-checked at admin-add time and rejected if they are not readable
directories.

```json
["/srv/audiobooks", "/mnt/extra"]
```

Walks are recursive; `.m4b` and `.mp3` files are picked up by extension
(case-insensitive). Symlinks are rejected at ingest time
(`info.Mode().IsRegular()` filter) so a link inside a library root
cannot make the parser follow it out.

Per-library `media_type` is hardcoded to `"audiobook"` for every library
the catalog returns (`/api/v1/catalog/libraries`). The audiobooks portal
uses that value when listing backends; do not expect mixed-media
libraries from this plugin.

Audiobooks are identified by `(library_path_id, path)`; the stable ID is
seeded from `(size, mtime)` on first ingest but the row keeps its
original ID across edits so cover and enrichment FKs survive content
changes.

## 3. Scanning

Three trigger paths:

- **Admin button** — `POST /admin/scan`. Returns `{"scan_event_id": N}`
  immediately. Concurrent triggers serialize on a single in-process
  mutex; the scheduled task's `running` flag also drops overlapping
  ticks.
- **Scheduled** — `library_scan` capability, cron `0 */6 * * *` (every
  six hours).
- **Startup** — no implicit scan. Run the admin trigger once after
  installing to populate the catalog.

`GET /admin/scan/status` returns the most recent 50 `scan_event` rows
(running scans have no `FinishedAt`).

### What a scan does per file

1. Stat the file. Unstattable or non-regular entries are logged at WARN
   and skipped (a dangling symlink does not abort the scan).
2. Compute `(size, mtime)` signature. Unchanged signature → skip.
3. Otherwise parse: `.m4b` via the MP4 atom reader (covers from `covr`,
   chapters from `chap`), `.mp3` via the ID3 reader (covers from
   `APIC`).
4. If no embedded cover, fall back to a sidecar in the same directory:
   `cover.jpg`, `cover.png`, then `folder.jpg`, in that order. Source is
   stored as `embedded` or `sidecar` so you can later distinguish
   which.
5. Upsert audiobook + chapters + cover. Soft-delete any prior row whose
   ID was not seen this run.
6. Best-effort enqueue into `metadata_enrichment_job`. Enqueue errors
   are logged at WARN; they do not abort the scan.

A corrupt/unreadable audio file logs WARN and is skipped — one bad file
never aborts the library scan.

### After the walk

- Stale metadata cache entries are evicted (`Cache.EvictExpired`,
  best-effort).
- If `scan_inline_enrich = true`, the worker drains the queue
  synchronously before the scan returns. Use only for small libraries;
  big libraries will keep the admin trigger blocked for minutes.

## 4. Metadata enrichment worker

The plugin runs a Postgres-backed queue. The
`metadata_enrichment_worker` scheduled task drains it once per minute
(cron `* * * * *`).

### Source selection

- `metadata_sources_enabled` (default: all seven) controls which sources
  the gRPC `Search` fans out to in parallel.
- `metadata_scan_source` controls which **single** source the
  enrichment worker uses for queue jobs. Default `audnexus`. Allowed
  values: `audnexus`, `audimeta`, `itunes`, `storytel`, `bookbeat`,
  `audioteka`. `audiobookcovers` is **not** valid here — it is
  cover-only and rejected at config time.

### Lookup cascade

For each job the worker tries, in order:

1. `ASIN` (if set on the row) — calls `src.Get`.
2. `ISBN` (if set) — calls `src.Search`, takes the first candidate.
3. Title + author text — calls `src.Search`, takes the first candidate.

Sentinel `ErrNotFound` is treated as "no change" (job completes
silently); any other error counts against the retry budget.

### Queue mechanics

- `metadata_enrichment_job` has one row per audiobook (upserted on
  conflict).
- `ClaimNext` selects pending jobs `FOR UPDATE SKIP LOCKED` and leases
  them 15 minutes — concurrent drains cannot double-process, and a job
  whose worker crashed becomes claimable again after the lease.
- Max 5 attempts per job. Backoff is `2^attempts` minutes between
  failures; at 5 the row goes to `failed` with `last_error`.

### Triggering backfill

After adding sources, swapping region, or editing source code:

```text
POST /admin/metadata/backfill
```

Returns `{"queued": N}`. Picks up any audiobook lacking a completed
enrichment row.

### Rate limit and cache

- `metadata_rate_limit_rps` (default `5`) is per-source token-bucket
  burst+rate. `rps < 1` is clamped to 1 (a burst-0 limiter would
  silently drop every request).
- `metadata_cache_ttl_days` (default `30`) controls per-source positive
  + `not_found` cache lifetime; expired entries are evicted at the end
  of every library scan.
- `metadata_default_region` (default `us`) is forwarded to region-aware
  sources (Storytel, BookBeat, Audioteka, iTunes).

## 5. Standalone HTTP listener

When you want mobile clients to stream byte ranges directly from this
plugin (bypassing the audiobooks portal proxy), enable the standalone
listener.

1. **Generate a shared secret** in the audiobooks portal admin UI
   ("Generate streaming secret"). It produces a 32-byte base64 value.
2. Set `standalone_http_listen` (e.g. `127.0.0.1:7879` or `:7879`).
3. Set `stream_signing_secret` to the same base64 value. The plugin
   rejects Configure if `standalone_http_listen` is set without a
   secret, and it strictly requires the decoded secret to be exactly 32
   bytes.
4. Set `cdn_signing_secret` and `cdn_hostname` on the audiobooks portal
   side to the same secret + the public hostname for the CDN.
5. DNS: point `audiobooks-cdn.example.com` at the same host as the
   portal.
6. Reverse proxy: terminate TLS on the CDN hostname and forward to the
   `standalone_http_listen` socket.
7. Restart both plugins.

### What the listener exposes

Only the byte-serving routes answer on the standalone port; everything
else returns 404:

- `GET /api/v1/stream/{book_id}/{file_idx}`
- `GET /api/v1/cover/{book_id}/{size}`

Both require a valid stream-token query param. The handler enforces
`file_idx == 0` — local audiobooks are always single-file-per-book; any
other index returns 404.

### Security details

- The host-proxy gRPC `Handle` path and the standalone `ServeHTTP` path
  share the same chi router but the standalone path strips every
  `X-Silo-*` header before invoking the handler, so a malicious
  client cannot spoof host-trust headers via the public listener.
- Tokens are HS256 JWTs with audience `audiobook_backend`, claims
  `sub`, `book_id`, `file_idx`, and required `exp`. The aud/book/idx
  claims are matched exactly. The portal mints them at request time;
  in-flight tokens expire within 5 minutes.

### Reconfiguration

The listener is started **once per process** via `sync.Once`. Changing
`standalone_http_listen` requires a plugin restart; the admin handler
logs a warning if a new value differs from the running one. The
underlying chi handler (for handler-only changes) is hot-swapped via an
atomic pointer.

### Rotating the streaming secret

Manual: generate a new secret in the audiobooks portal, paste into both
plugin configs, restart both plugins. In-flight stream tokens expire
within 5 minutes regardless.

## 6. Backups and recovery

The `local_audiobooks` schema stores: catalog index, chapters, cover
bytes (embedded + sidecar copies), scan history, enrichment queue, and
metadata cache. **On-disk M4B/MP3 files are canonical**. The schema is
recoverable by rescanning — durations and chapters re-extract, embedded
covers re-extract, sidecar covers re-read.

What is *not* recoverable from disk:

- Enrichment text that came from upstream metadata sources (re-run
  backfill to refetch).
- Scan event history.
- The current `metadata_enrichment_job` queue state (rebuild via
  backfill).

A periodic `pg_dump --schema=local_audiobooks` is enough to avoid the
rescan cost after a DR event.
