-- Relay retry bookkeeping: track delivery attempts and the last error so the
-- outbox relay can back off poison messages and operators can inspect failures.
ALTER TABLE outbox ADD COLUMN attempts    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE outbox ADD COLUMN last_error  TEXT;

-- Reshape the unpublished index to also exclude exhausted rows from the hot poll.
DROP INDEX IF EXISTS outbox_unpublished_idx;
CREATE INDEX outbox_pending_idx ON outbox (created_at)
    WHERE published_at IS NULL;
