# Database ERD — SQLite Schema (OpenCode-Adapted)

> All tables in the SQLite database and their relationships.
> Storage: `.gopengai/gopengai.db` — single-file SQLite (WAL mode, pure Go via `ncruces/go-sqlite3`).
> Migrations: Goose. Query generation: sqlc.

```mermaid
erDiagram
    SESSIONS ||--o{ MESSAGES : "has tree of"
    SESSIONS ||--o| SESSIONS : "parent_session_id"
    SESSIONS ||--o| MESSAGES : "summary_message_id"
    MESSAGES ||--o{ MESSAGES : "parent_id (tree branches)"
    AGENTS ||--o{ MESSAGES : "used in"
    AGENTS ||--o{ MEMORY : "owns"
    AGENTS ||--o{ DELEGATION_LOGS : "delegates via"
    MESSAGES ||--o{ DELEGATION_LOGS : "triggers"

    SESSIONS {
        text id PK "UUID"
        text parent_session_id FK "nullable, for auto-compact chaining"
        text agent_name "default agent for session"
        text title "auto-generated or user-set"
        text active_leaf_id FK "current active branch leaf (tree extension)"
        text status "idle | working | aborted"
        int message_count "auto-maintained by triggers"
        int prompt_tokens "cumulative prompt tokens"
        int completion_tokens "cumulative completion tokens"
        real cost "cumulative cost estimate"
        int updated_at "Unix timestamp in ms"
        int created_at "Unix timestamp in ms"
        text summary_message_id FK "nullable, for auto-compact summary"
    }

    MESSAGES {
        text id PK "UUID"
        text session_id FK "→ sessions.id"
        text parent_id FK "→ messages.id (NULL for root, enables tree)"
        text role "user | assistant | tool | system"
        text parts "JSON array: text chunks, tool_use, images"
        text content "plain text (for tree queries)"
        text agent_name "which agent produced this"
        text tool_name "if role=tool: which tool"
        text tool_call_id "links tool response to call"
        text tool_args "JSON: tool arguments"
        text model "model used (nullable)"
        int token_count "tokens used for this message"
        int created_at "Unix timestamp in ms"
        int updated_at "Unix timestamp in ms"
        int finished_at "Unix timestamp in ms (nullable)"
    }

    AGENTS {
        text name PK "agent name (filename without .md)"
        text system_prompt "full system prompt from .md"
        text tools "JSON array of allowed tool names"
        text model "model override (nullable)"
        text parent_agent "parent agent name (nullable)"
        text permissions "JSON: tool_name → allow/deny"
        text config_path "absolute path to .md file"
        int loaded_at "Unix timestamp in ms"
    }

    MEMORY {
        text id PK "UUID"
        text agent_name FK "→ agents.name"
        text key "fact key (e.g. 'user_name')"
        text value "fact value (e.g. 'Alex')"
        text category "optional grouping (e.g. 'preferences')"
        int created_at "Unix timestamp in ms"
        int updated_at "Unix timestamp in ms"
    }

    DELEGATION_LOGS {
        text id PK "UUID"
        text parent_message_id FK "→ messages.id"
        text child_agent_name FK "→ agents.name"
        text child_session_id "sub-agent's own session"
        text task_description "what was delegated"
        text result_summary "first 500 chars of result"
        int duration_ms "how long delegation took"
        int created_at "Unix timestamp in ms"
    }
```

## Key Design Decisions

- **OpenCode-compatible base:** Sessions and Messages tables match OpenCode's schema, extended with tree structure (`parent_id`, `active_leaf_id`) and agent/delegation columns.
- **Messages form a tree**, not a linear list. `parent_id` links to parent message (NULL for root).
- **`active_leaf_id`** in SESSIONS points to the current "cursor" — the leaf of the active branch.
- **`parent_session_id`** + **`summary_message_id`** enable auto-compact: when context window fills, summarize → spawn child session.
- **`parts`** column (JSON array) stores rich message content matching LLM API conventions.
- **`content`** column (plain text) is a convenience field for tree queries and display.
- **Tool messages** have `role=tool`, `tool_name`, and `tool_call_id` linking them to the assistant's tool call.
- **MEMORY** is simple key-value, scoped per agent. No vector embeddings for MVP.
- **DELEGATION_LOGS** tracks when an agent spawned a sub-agent, for debugging and transparency.
- **No separate users table** for MVP. Sessions are identified by UUID. Auth is a future concern.
- **Timestamps** are Unix milliseconds (integers), matching OpenCode's convention.
- **Triggers** auto-update `updated_at` and `message_count` on relevant changes.
