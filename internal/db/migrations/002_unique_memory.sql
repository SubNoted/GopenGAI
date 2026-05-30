-- +goose Up
-- +goose StatementBegin

-- Add unique constraint on (agent_name, key) for memory table
-- to prevent duplicate rows on concurrent MemorySave calls.
-- Removes any existing duplicates first, keeping the most recent.
DELETE FROM memory WHERE id NOT IN (
    SELECT id FROM memory
    WHERE (agent_name, key) IN (
        SELECT agent_name, key FROM memory
        GROUP BY agent_name, key
        HAVING COUNT(*) > 1
    )
    AND id IN (
        SELECT MAX(id) FROM memory
        GROUP BY agent_name, key
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_agent_name_key ON memory (agent_name, key);

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_memory_agent_name_key;
-- +goose StatementEnd
