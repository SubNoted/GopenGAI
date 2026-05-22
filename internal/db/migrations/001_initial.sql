-- Phase 1: Initial schema
-- Creates core tables for sessions, messages, agents, memory, and delegation

-- Sessions (OpenCode-compatible base)
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    parent_session_id TEXT,
    agent_name TEXT NOT NULL DEFAULT 'default',
    title TEXT NOT NULL,
    active_leaf_id TEXT,
    status TEXT NOT NULL DEFAULT 'idle',
    message_count INTEGER NOT NULL DEFAULT 0 CHECK (message_count >= 0),
    prompt_tokens INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
    completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
    cost REAL NOT NULL DEFAULT 0.0 CHECK (cost >= 0.0),
    updated_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    summary_message_id TEXT
);

-- Messages (OpenCode-compatible base + tree/tool extensions)
CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    parent_id TEXT,
    role TEXT NOT NULL,
    parts TEXT NOT NULL DEFAULT '[]',
    content TEXT,
    agent_name TEXT,
    tool_name TEXT,
    tool_call_id TEXT,
    tool_args TEXT,
    model TEXT,
    token_count INTEGER DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    finished_at INTEGER,
    FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE
);

-- Agents (loaded from .md files)
CREATE TABLE IF NOT EXISTS agents (
    name TEXT PRIMARY KEY,
    system_prompt TEXT NOT NULL,
    tools TEXT NOT NULL DEFAULT '[]',
    model TEXT,
    parent_agent TEXT,
    permissions TEXT NOT NULL DEFAULT '{}',
    config_path TEXT,
    loaded_at INTEGER NOT NULL
);

-- Memory (per-agent key-value store)
CREATE TABLE IF NOT EXISTS memory (
    id TEXT PRIMARY KEY,
    agent_name TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    category TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (agent_name) REFERENCES agents (name) ON DELETE CASCADE
);

-- Delegation Logs (agent -> sub-agent tracking)
CREATE TABLE IF NOT EXISTS delegation_logs (
    id TEXT PRIMARY KEY,
    parent_message_id TEXT NOT NULL,
    child_agent_name TEXT NOT NULL,
    child_session_id TEXT,
    task_description TEXT NOT NULL,
    result_summary TEXT,
    duration_ms INTEGER,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (parent_message_id) REFERENCES messages (id) ON DELETE CASCADE,
    FOREIGN KEY (child_agent_name) REFERENCES agents (name) ON DELETE CASCADE
);

-- Triggers
CREATE TRIGGER IF NOT EXISTS update_sessions_updated_at
AFTER UPDATE ON sessions
BEGIN
    UPDATE sessions SET updated_at = strftime('%s', 'now') * 1000 WHERE id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS update_messages_updated_at
AFTER UPDATE ON messages
BEGIN
    UPDATE messages SET updated_at = strftime('%s', 'now') * 1000 WHERE id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS update_memory_updated_at
AFTER UPDATE ON memory
BEGIN
    UPDATE memory SET updated_at = strftime('%s', 'now') * 1000 WHERE id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS update_session_message_count_on_insert
AFTER INSERT ON messages
BEGIN
    UPDATE sessions SET message_count = message_count + 1 WHERE id = NEW.session_id;
END;

CREATE TRIGGER IF NOT EXISTS update_session_message_count_on_delete
AFTER DELETE ON messages
BEGIN
    UPDATE sessions SET message_count = MAX(0, message_count - 1) WHERE id = OLD.session_id;
END;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages (session_id);
CREATE INDEX IF NOT EXISTS idx_messages_parent_id ON messages (parent_id);
CREATE INDEX IF NOT EXISTS idx_memory_agent_name ON memory (agent_name);
CREATE INDEX IF NOT EXISTS idx_delegation_logs_parent ON delegation_logs (parent_message_id);
