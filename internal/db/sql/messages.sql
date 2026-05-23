-- name: CreateMessage :one
INSERT INTO messages (id, session_id, parent_id, role, parts, content, agent_name, tool_name, tool_call_id, tool_args, model, token_count, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = ?;

-- name: ListMessagesBySession :many
SELECT * FROM messages WHERE session_id = ? ORDER BY created_at ASC;

-- name: GetBranchFromRootTo :many
WITH RECURSIVE branch AS (
    SELECT * FROM messages WHERE messages.id = ?
    UNION ALL
    SELECT m.* FROM messages m
    INNER JOIN branch b ON m.id = b.parent_id
)
SELECT * FROM branch ORDER BY created_at ASC;

-- name: GetAllLeaves :many
SELECT m.* FROM messages m
WHERE m.session_id = ?
AND m.id NOT IN (SELECT sub.parent_id FROM messages sub WHERE sub.parent_id IS NOT NULL AND sub.session_id = ?);

-- name: UpdateMessage :exec
UPDATE messages
SET content = COALESCE(?, content),
    parts = COALESCE(?, parts),
    finished_at = COALESCE(?, finished_at)
WHERE id = ?;

-- name: DeleteMessage :exec
DELETE FROM messages WHERE id = ?;

-- name: DeleteSessionMessages :exec
DELETE FROM messages WHERE session_id = ?;
