CREATE TABLE library_path (
  id              BIGSERIAL    PRIMARY KEY,
  path            TEXT         NOT NULL UNIQUE,
  enabled         BOOLEAN      NOT NULL DEFAULT TRUE,
  last_scanned_at TIMESTAMPTZ,
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE audiobook (
  id                TEXT         PRIMARY KEY,
  library_path_id   BIGINT       NOT NULL REFERENCES library_path(id) ON DELETE CASCADE,
  path              TEXT         NOT NULL,
  file_size         BIGINT       NOT NULL,
  mtime             TIMESTAMPTZ  NOT NULL,
  title             TEXT         NOT NULL DEFAULT '',
  author            TEXT         NOT NULL DEFAULT '',
  narrator          TEXT         NOT NULL DEFAULT '',
  album             TEXT         NOT NULL DEFAULT '',
  year              TEXT         NOT NULL DEFAULT '',
  genre             TEXT         NOT NULL DEFAULT '',
  isbn              TEXT         NOT NULL DEFAULT '',
  asin              TEXT         NOT NULL DEFAULT '',
  description       TEXT         NOT NULL DEFAULT '',
  duration_ms       BIGINT       NOT NULL DEFAULT 0,
  deleted           BOOLEAN      NOT NULL DEFAULT FALSE,
  deleted_at        TIMESTAMPTZ,
  scanned_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
  created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX audiobook_path_idx ON audiobook(library_path_id, path);
CREATE INDEX audiobook_active_idx ON audiobook(library_path_id, deleted) WHERE deleted = FALSE;

CREATE TABLE chapter (
  audiobook_id TEXT    NOT NULL REFERENCES audiobook(id) ON DELETE CASCADE,
  idx          INTEGER NOT NULL,
  title        TEXT    NOT NULL DEFAULT '',
  start_ms     BIGINT  NOT NULL DEFAULT 0,
  end_ms       BIGINT  NOT NULL DEFAULT 0,
  PRIMARY KEY (audiobook_id, idx)
);

CREATE TABLE cover (
  audiobook_id TEXT    PRIMARY KEY REFERENCES audiobook(id) ON DELETE CASCADE,
  content_type TEXT    NOT NULL,
  bytes        BYTEA   NOT NULL,
  source       TEXT    NOT NULL CHECK (source IN ('embedded', 'sidecar'))
);

CREATE TABLE scan_event (
  id              BIGSERIAL    PRIMARY KEY,
  library_path_id BIGINT       REFERENCES library_path(id) ON DELETE SET NULL,
  started_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
  finished_at     TIMESTAMPTZ,
  books_added     INTEGER      NOT NULL DEFAULT 0,
  books_changed   INTEGER      NOT NULL DEFAULT 0,
  books_deleted   INTEGER      NOT NULL DEFAULT 0,
  error_text      TEXT
);

CREATE INDEX scan_event_recent_idx ON scan_event(started_at DESC);
