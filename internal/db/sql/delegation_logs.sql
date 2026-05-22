-- name: CreateDelegationLog :one
INSERT INTO delegation_logs (id, parent_message_id, child_agent_name, child_session_id, task_description, result_summary, duration_ms, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListDelegationLogsBySession :many
SELECT dl.* FROM delegation_logs dl
INNER JOIN messages m ON dl.parent_message_id = m.id
WHERE m.session_id = ?
ORDER BY dl.created_at DESC;
