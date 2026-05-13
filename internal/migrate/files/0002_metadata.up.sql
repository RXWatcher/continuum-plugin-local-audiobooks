CREATE TABLE metadata_cache (
  cache_key     TEXT        PRIMARY KEY,
  source        TEXT        NOT NULL,
  region        TEXT        NOT NULL DEFAULT '',
  response_json JSONB       NOT NULL,
  not_found     BOOLEAN     NOT NULL DEFAULT FALSE,
  fetched_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX metadata_cache_source_fetched_idx
  ON metadata_cache(source, fetched_at);

CREATE TABLE metadata_enrichment_job (
  audiobook_id TEXT        PRIMARY KEY REFERENCES audiobook(id) ON DELETE CASCADE,
  status       TEXT        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending','completed','failed')),
  attempts     INTEGER     NOT NULL DEFAULT 0,
  run_after    TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error   TEXT        NOT NULL DEFAULT '',
  enqueued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at  TIMESTAMPTZ
);
CREATE INDEX metadata_enrichment_pending_idx
  ON metadata_enrichment_job(run_after)
  WHERE status = 'pending';
