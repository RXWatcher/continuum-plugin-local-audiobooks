CREATE TABLE IF NOT EXISTS app_config (
  id         INT PRIMARY KEY DEFAULT 1,
  data       JSONB NOT NULL DEFAULT '{}'::jsonb,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT app_config_singleton CHECK (id = 1)
);

INSERT INTO app_config (id, data)
VALUES (
  1,
  '{
    "metadata_sources_enabled": [
      "audnexus",
      "audimeta",
      "itunes",
      "storytel",
      "bookbeat",
      "audioteka",
      "audiobookcovers"
    ],
    "metadata_default_region": "us",
    "metadata_cache_ttl_days": 30,
    "metadata_rate_limit_rps": 5,
    "scan_inline_enrich": false,
    "metadata_scan_source": "audnexus",
    "standalone_http_listen": "",
    "stream_signing_secret": ""
  }'::jsonb
)
ON CONFLICT (id) DO NOTHING;
