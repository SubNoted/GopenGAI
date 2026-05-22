-- name: CreateSession :one
INSERT INTO sessions (id, parent_session_id, agent_name, title, active_leaf_id, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSessionByID :one
SELECT * FROM sessions WHERE id = ?;

-- name: ListSessions :many
SELECT * FROM sessions ORDER BY updated_at DESC;

-- name: UpdateSession :one
UPDATE sessions
SET title = COALESCE(?, title),
    agent_name = COALESCE(?, agent_name),
    active_leaf_id = COALESCE(?, active_leaf_id),
    status = COALESCE(?, status),
    summary_message_id = COALESCE(?, summary_message_id)
WHERE id = ?
RETURNING *;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;
