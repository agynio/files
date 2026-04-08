ALTER TABLE files
    ADD COLUMN tenant_id UUID;

DROP INDEX IF EXISTS idx_files_created_at;

CREATE INDEX idx_files_tenant_id_created_at ON files (tenant_id, created_at);
