-- name: CreateAgent :one
INSERT INTO agents (name, system_prompt, tools, model, parent_agent, permissions, config_path, loaded_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAgent :one
SELECT * FROM agents WHERE name = ?;

-- name: ListAgents :many
SELECT * FROM agents ORDER BY name;

-- name: DeleteAgent :exec
DELETE FROM agents WHERE name = ?;
