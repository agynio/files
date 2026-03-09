CREATE TABLE files (
    id UUID PRIMARY KEY,
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_files_created_at ON files (created_at);
