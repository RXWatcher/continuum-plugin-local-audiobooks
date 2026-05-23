# Local Audiobooks for Silo

`silo.local-audiobooks` is the local-filesystem audiobook backend for the [Silo](https://github.com/ContinuumApp/silo) plugin ecosystem. It walks configured library directories for `.m4b` and `.mp3` files, persists their metadata and embedded/sidecar covers into a dedicated Postgres schema, and serves the resulting catalog, browse trees, covers, and byte-range streams to the [`silo.audiobooks`](https://github.com/RXWatcher/silo-plugin-audiobooks) portal. It also bundles a metadata aggregator that searches seven upstream sources to fill in titles, authors, narrators, descriptions, and artwork.

## Category

Lives under **Books / Audiobooks** in the admin sidebar.

## Capabilities

| Type | ID | Purpose |
| --- | --- | --- |
| `audiobook_backend.v1` | `local_audiobooks` | Exposes the on-disk library as a `library_source` to the audiobooks portal. Supports catalog; does not support requests or auto-monitoring. |
| `http_routes.v1` | `api` | Public stream + cover endpoints, authenticated catalog/browse, and admin endpoints for mounts, scan triggers, and metadata diagnostics. |
| `scheduled_task.v1` | `library_scan` | Walks configured library directories every 6 hours and upserts discovered audiobooks. |
| `metadata_provider.v1` | `local_audiobooks_meta` | Aggregator that fans out to seven upstream sources; default audiobook priority `1`. |
| `scheduled_task.v1` | `metadata_enrichment_worker` | Per-minute worker that drains the enrichment queue, filling missing metadata via the bundled aggregator. |

## Dependencies

- Host: [`github.com/ContinuumApp/silo`](https://github.com/ContinuumApp/silo).
- SDK: [`github.com/ContinuumApp/continuum-plugin-sdk`](https://github.com/ContinuumApp/continuum-plugin-sdk).
- Consumed by [`silo-plugin-audiobooks`](https://github.com/RXWatcher/silo-plugin-audiobooks) (the portal), which presents the local library alongside other backends such as [`silo-plugin-bookwarehouse-audio`](https://github.com/RXWatcher/silo-plugin-bookwarehouse-audio) and request providers like [`silo-plugin-audiobook-requests`](https://github.com/RXWatcher/silo-plugin-audiobook-requests).

## External services

- A local filesystem mount containing `.m4b` and `.mp3` files. Audiobooks are identified per `(library_path, relative path)`; covers are read either from the container (`m4b` `covr` atom, `mp3` `APIC` frame) or from `cover.{jpg,png}` sidecar files next to the audio.
- Postgres, with a dedicated `local_audiobooks` schema (the plugin runs its own migrations on startup).
- Outbound HTTPS to the seven metadata upstreams listed below. No API keys required.
- Optional standalone HTTP listener (separate from the host-proxy routes) for HMAC-signed, presigned byte-range streaming through a reverse proxy.

## Configuration

| Key | Required | Description |
| --- | --- | --- |
| `database_url` | yes | DSN for the dedicated `local_audiobooks` schema (Postgres). |
| `library_paths` | yes | JSON array of absolute directories to scan, for example `["/srv/audiobooks"]`. |
| `standalone_http_listen` | no | Optional `host:port` for the presigned-stream listener. |
| `stream_signing_secret` | conditional | 32-byte base64 HMAC secret shared with the audiobooks portal; required when `standalone_http_listen` is set. |
| `metadata_sources_enabled` | no | JSON array of metadata source IDs to query. Defaults to all seven. |
| `metadata_default_region` | no | ISO country code passed to region-aware sources. Defaults to `us`. |
| `metadata_cache_ttl_days` | no | TTL for cached upstream responses. Defaults to `30`. |
| `metadata_rate_limit_rps` | no | Per-source request rate limit. Defaults to `5`. |
| `scan_inline_enrich` | no | Drain enrichment jobs synchronously at the end of each scan. |
| `metadata_scan_source` | no | Scan-capable source used by the enrichment worker. Defaults to `audnexus`. Must be one of `audnexus`, `audimeta`, `itunes`, `storytel`, `bookbeat`, `audioteka`. |

Example DSN:

```text
postgres://plugin_local_audiobooks:password@postgres:5432/silo?search_path=local_audiobooks&sslmode=disable
```

## Metadata providers

The aggregator registers and orchestrates seven upstream sources:

- **Audnexus** (`api.audnex.us`) — ASIN-keyed Audible metadata; the default scan source.
- **AudiMeta** (`api.audimeta.de`) — community-maintained audiobook database.
- **iTunes** (`itunes.apple.com`) — Apple's lookup API, keyed on numeric `trackId`/`collectionId`.
- **Storytel** — regional Storytel catalog, ID slugs like `project-hail-mary-12345`.
- **BookBeat** — region-specific BookBeat domains.
- **Audioteka** — Audioteka catalog across `pl`, `cz`, and other regions.
- **Audiobookcovers** (`api.audiobookcovers.com`) — cover-only source; candidates surface as "improve cover" matches rather than primary results.

Results are cached in Postgres for `metadata_cache_ttl_days`, rate-limited per source, and ranked by the aggregator's confidence formula.

## Detailed docs

- [Setup, debugging, and communication flows](docs/setup-debug-flows.md)
- [Operations runbook](docs/operations.md)

## Build and release

```bash
make build
make test
```

CI builds linux-amd64 binaries on push to main via the reusable workflow in [RXWatcher/silo-plugin-repository](https://github.com/RXWatcher/silo-plugin-repository) and publishes them to the catalog at [`./binaries/`](https://github.com/RXWatcher/silo-plugin-repository/tree/main/binaries).
