ALTER TABLE files DROP COLUMN tenant_id;
DROP INDEX IF EXISTS idx_files_tenant_created_at;
CREATE INDEX idx_files_created_at ON files (created_at);
