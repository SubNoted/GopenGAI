# Go Package Structure — Code Layout

> How the codebase is organized into Go packages.

```mermaid
flowchart TD
    subgraph "cmd/"
        CMD_API["cmd/api/main.go\nHTTP server entrypoint"]
        CMD_CLI["cmd/cli/main.go\nCLI client entrypoint"]
    end

    subgraph "internal/"
        subgraph "internal/api/"
            API_HANDLER["api/handler.go\nRoute handlers (native + OpenAI compat)"]
            API_MIDDLEWARE["api/middleware.go\nLogging, auth (future), CORS"]
            API_ROUTES["api/routes.go\nRoute registration"]
        end

        subgraph "internal/agent/"
            AGENT_ENGINE["agent/engine.go\nCore agent loop (reason → tool → respond)"]
            AGENT_LOADER["agent/loader.go\nParse .md agent configs"]
            AGENT_REGISTRY["agent/registry.go\nIn-memory agent registry"]
            AGENT_TYPES["agent/types.go\nAgent, ToolCall, Message structs"]
        end

        subgraph "internal/tools/"
            TOOLS_REG["tools/registry.go\nTool interface + registry"]
            TOOLS_WEB["tools/web_fetch.go\nHTTP GET + text extraction"]
            TOOLS_MEM["tools/memory.go\nmemory_save + memory_recall"]
            TOOLS_DELEG["tools/delegate.go\nSub-agent delegation"]
        end

        subgraph "internal/history/"
            HIST_TREE["history/tree.go\nTree operations (insert, branch, traverse)"]
            HIST_REPO["history/repo.go\nSQLite CRUD for messages"]
            HIST_BRANCH["history/branch.go\nBranch selection, edit → new branch"]
        end

        subgraph "internal/llm/"
            LLM_CLIENT["llm/client.go\nOpenAI-compatible HTTP client"]
            LLM_TYPES["llm/types.go\nRequest/Response structs"]
            LLM_STREAM["llm/stream.go\nStreaming support (future)")
        end

        subgraph "internal/db/"
            DB_CONNECT["db/connect.go\nSQLite connection (ncruces/go-sqlite3)\nWAL mode, pragmas, pool config"]
            DB_EMBED["db/embed.go\nEmbeds migrations/ into binary via go:embed"]
            DB_MIGRATIONS["db/migrations/\nGoose SQL migration files\n(001_initial.sql, ...)"]
            DB_SQL["db/sql/\nRaw SQL queries for sqlc\n(sessions, messages, agents, memory, delegation)"]
            DB_GENERATED["db/db.go, models.go, querier.go\nsqlc-generated: Queries struct,\nGo types, Querier interface"]
            DB_IMPL["db/*.sql.go\nsqlc-generated typed query\nimplementations"]
        end

        subgraph "internal/config/"
            CFG["config/config.go\nApp config from env / flags"]
        end
    end

    CMD_API --> API_HANDLER
    CMD_CLI --> API_HANDLER
    API_HANDLER --> AGENT_ENGINE
    API_HANDLER --> HIST_REPO
    AGENT_ENGINE --> AGENT_LOADER
    AGENT_ENGINE --> AGENT_REGISTRY
    AGENT_ENGINE --> TOOLS_REG
    AGENT_ENGINE --> LLM_CLIENT
    AGENT_ENGINE --> HIST_TREE
    AGENT_LOADER --> DB_GENERATED
    TOOLS_WEB --> LLM_TYPES
    TOOLS_MEM --> DB_GENERATED
    TOOLS_DELEG --> AGENT_ENGINE
    HIST_REPO --> HIST_TREE
    HIST_BRANCH --> HIST_REPO
    HIST_REPO --> DB_GENERATED
    LLM_CLIENT --> LLM_TYPES
    DB_CONNECT --> DB_GENERATED
    DB_EMBED --> DB_MIGRATIONS
    DB_SQL --> DB_GENERATED

    style CMD_API fill:#4CAF50,color:#fff
    style CMD_CLI fill:#4CAF50,color:#fff
    style AGENT_ENGINE fill:#2196F3,color:#fff
```

## Package Responsibilities

| Package | Responsibility |
|---------|---------------|
| `cmd/api/` | HTTP server startup, wiring dependencies |
| `cmd/cli/` | CLI client using Cobra, HTTP calls to API |
| `internal/api/` | HTTP handlers, request parsing, response formatting |
| `internal/agent/` | **Core**: agent loop, .md config parsing, registry |
| `internal/tools/` | Tool interface + implementations (web_fetch, memory, delegate) |
| `internal/history/` | Message tree CRUD, branch management, traversal |
| `internal/llm/` | OpenAI-compatible HTTP client for LLM calls |
| `internal/db/` | SQLite via ncruces/go-sqlite3, Goose migrations, sqlc-generated CRUD |
| `internal/config/` | App configuration (env vars, flags, defaults) |

## Key Interfaces

```go
// internal/tools/registry.go
type Tool interface {
    Name() string
    Description() string
    Parameters() json.RawMessage  // JSON Schema
    Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// internal/agent/engine.go
type Engine interface {
    Run(ctx context.Context, sessionID string, message string, agentName string) (*Response, error)
}

// internal/llm/client.go
type Client interface {
    ChatCompletion(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

// internal/db/querier.go (sqlc-generated)
type Querier interface {
    CreateSession(ctx, arg CreateSessionParams) (Session, error)
    GetSessionByID(ctx, id string) (Session, error)
    ListSessions(ctx) ([]Session, error)
    CreateMessage(ctx, arg CreateMessageParams) (Message, error)
    ListMessagesBySession(ctx, sessionID string) ([]Message, error)
    GetBranchFromRootTo(ctx, messageID string) ([]Message, error)  // recursive CTE
    GetAllLeaves(ctx, sessionID string) ([]Message, error)
    CreateAgent(ctx, arg CreateAgentParams) (Agent, error)
    GetAgent(ctx, name string) (Agent, error)
    CreateMemory(ctx, arg CreateMemoryParams) (Memory, error)
    ListMemoryByAgent(ctx, agentName string) ([]Memory, error)
    // ... and more
}
```
