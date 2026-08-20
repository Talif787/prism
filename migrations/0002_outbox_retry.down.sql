DROP INDEX IF EXISTS outbox_pending_idx;
CREATE INDEX outbox_unpublished_idx ON outbox (created_at) WHERE published_at IS NULL;
ALTER TABLE outbox DROP COLUMN IF EXISTS last_error;
ALTER TABLE outbox DROP COLUMN IF EXISTS attempts;
