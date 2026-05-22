# Container Diagram — GoPengAI Architecture

> Updated container diagram reflecting the gopengai project with async API + SSE event system.
> Replaces: 01-container.md (original nlpcore diagram kept for reference).

```mermaid
C4Container
    title GoPengAI — System Containers

    Person(user, "User", "Sends chat requests via CLI, external client, or direct HTTP")
    Person(admin, "Admin", "Defines agents in .md files, edits gopengai.json")

    Container(cli, "CLI Client", "Go / Cobra", "Commands: chat, session, agent, memory")
    Container(api, "HTTP API Server", "Go / net/http", "Native REST API + OpenAI-compatible + SSE events")
    Container(eventbus, "Event Bus", "Go", "In-memory pub/sub for SSE streaming")
    Container(engine, "Agent Engine", "Go", "Agent loop: reason → tool call → delegate → respond")
    Container(loader, "Agent Loader", "Go", "Parses .md agent configs with YAML frontmatter")
    Container(tools, "Tool Registry", "Go", "web_fetch, memory_save, memory_recall, delegate_agent")
    ContainerDb(sqlite, "SQLite Database", "ncruces/go-sqlite3\nGoose migrations + sqlc", ".gopengai/gopengai.db")

    System_Ext(llm, "LLM Provider", "OpenAI-compatible: Polza AI, OpenAI, Ollama, etc.")
    System_Ext(web, "External Web", "URLs fetched by web_fetch tool")

    Rel(user, cli, "Terminal commands")
    Rel(user, api, "HTTP/JSON (native or OpenAI-compatible)", "HTTPS")
    Rel(cli, api, "HTTP/JSON", "localhost:8080")
    Rel(admin, loader, "Edits .md agent files", "filesystem")
    Rel(api, engine, "Forward parsed request (async)")
    Rel(api, eventbus, "Subscribe/Publish SSE events")
    Rel(engine, eventbus, "Publish status + message events")
    Rel(engine, loader, "Load agent by name")
    Rel(engine, tools, "Invoke tool calls (with permission check)")
    Rel(engine, sqlite, "Read/write history, memory")
    Rel(engine, llm, "POST /v1/chat/completions", "HTTPS")
    Rel(tools, web, "HTTP GET", "HTTPS")
    Rel(tools, sqlite, "memory_save / memory_recall")
```

## Key Differences from nlpcore (01-container.md)

| Aspect | nlpcore (old) | gopengai (new) |
|--------|---------------|----------------|
| API model | Synchronous POST → response | Async POST (202) + SSE event stream |
| Event system | None | In-memory Event Bus (global + per-session) |
| Config | Env vars only | `gopengai.json` + `.md` agent files |
| Database | Raw SQL, manual CRUD | SQLite + Goose migrations + sqlc codegen |
| DB driver | N/A | `ncruces/go-sqlite3` (pure Go, no CGo) |
| Data location | N/A | `.gopengai/gopengai.db` (per-project) |
| Tool permissions | All auto-execute | Per-agent allow/deny per tool |
| OpenAI compat | Mentioned | Full implementation with `/v1/chat/completions` + `/v1/models` |
| CLI client | Cobra HTTP client | Cobra + SSE subscription for streaming |

## Component Interactions

```mermaid
flowchart TD
    Client[Client] -->|POST /session/:id/message| API[HTTP API]
    Client -->|GET /session/:id/events| EventBus[Event Bus]
    Client -->|GET /event| EventBus

    API -->|202 Accepted| Client
    API -->|spawn goroutine| Engine[Agent Engine]
    API -->|publish| EventBus

    Engine -->|load agent| Loader[Agent Loader]
    Engine -->|execute tool| Tools[Tool Registry]
    Engine -->|read/write| DB[(SQLite)]
    Engine -->|chat/completions| LLM[LLM Provider]
    Engine -->|publish events| EventBus

    Tools -->|HTTP GET| Web[External Web]
    Tools -->|save/recall| DB

    style Engine fill:#2196F3,color:#fff
    style EventBus fill:#FF9800,color:#fff
    style API fill:#4CAF50,color:#fff
```
