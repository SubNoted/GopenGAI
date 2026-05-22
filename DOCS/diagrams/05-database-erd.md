# Database ERD — SQLite Schema

> All tables in the SQLite database and their relationships.

```mermaid
erDiagram
    SESSIONS ||--o{ MESSAGES : "has tree of"
    AGENTS ||--o{ MESSAGES : "used in"
    AGENTS ||--o{ DELEGATION_LOGS : "delegates via"
    MESSAGES ||--o{ DELEGATION_LOGS : "triggers"

    SESSIONS {
        text id PK "UUID"
        text agent_name "default agent for session"
        text title "auto-generated or user-set"
        text active_leaf_id FK "current active branch leaf"
        datetime created_at
        datetime updated_at
    }

    MESSAGES {
        text id PK "UUID"
        text session_id FK "→ sessions.id"
        text parent_id FK "→ messages.id (NULL for root)"
        text role "user | assistant | tool | system"
        text content "message text"
        text agent_name "which agent produced this"
        text tool_name "if role=tool: which tool"
        text tool_call_id "links tool response to call"
        text tool_args "JSON: tool arguments"
        int token_count "tokens used for this message"
        datetime created_at
    }

    AGENTS {
        text name PK "agent name (filename without .md)"
        text system_prompt "full system prompt from .md"
        text tools "JSON array of allowed tool names"
        text model "model override (nullable)"
        text parent_agent "parent agent name (nullable)"
        text config_path "absolute path to .md file"
        datetime loaded_at "when .md was last parsed"
    }

    MEMORY {
        text id PK "UUID"
        text agent_name FK "→ agents.name"
        text key "fact key (e.g. 'user_name')"
        text value "fact value (e.g. 'Alex')"
        text category "optional grouping (e.g. 'preferences')"
        datetime created_at
        datetime updated_at
    }

    DELEGATION_LOGS {
        text id PK "UUID"
        text parent_message_id FK "→ messages.id"
        text child_agent_name FK "→ agents.name"
        text child_session_id "sub-agent's own session"
        text task_description "what was delegated"
        text result_summary "first 500 chars of result"
        int duration_ms "how long delegation took"
        datetime created_at
    }
```

## Key Design Decisions

- **Messages form a tree**, not a linear list. `parent_id` links to parent message.
- **`active_leaf_id`** in SESSIONS points to the current "cursor" — the leaf of the active branch.
- **Tool messages** have `role=tool`, `tool_name`, and `tool_call_id` linking them to the assistant's tool call.
- **MEMORY** is simple key-value, scoped per agent. No vector embeddings for MVP.
- **DELEGATION_LOGS** tracks when an agent spawned a sub-agent, for debugging and transparency.
- **No separate users table** for MVP. Sessions are identified by UUID. Auth is a future concern.
