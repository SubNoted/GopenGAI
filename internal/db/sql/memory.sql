-- name: CreateMemory :one
INSERT INTO memory (id, agent_name, key, value, category, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMemory :one
SELECT * FROM memory WHERE agent_name = ? AND key = ?;

-- name: ListMemoryByAgent :many
SELECT * FROM memory WHERE agent_name = ? ORDER BY updated_at DESC;

-- name: DeleteMemory :exec
DELETE FROM memory WHERE id = ?;
