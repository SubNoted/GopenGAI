# GoPengAI — Agent System

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)
[![Status](https://img.shields.io/badge/Status-Active-success)]()

**GoPengAI** is a Go-native AI agent system with a REST API inspired by [OpenCode](https://github.com/opencode-ai/opencode). It intelligently handles user requests by combining multiple tools — web fetch, memory storage, agent delegation — with LLM generation. Conversations support tree-based branching history for editing and forking.

**Persistence:** SQLite (via `ncruces/go-sqlite3` — pure Go, no CGo), schema migrations via [Goose](https://github.com/pressly/goose), type-safe query generation via [sqlc](https://sqlc.dev/). One `.db` file per project, stored in `.gopengai/`.

## Key Features

- **Async Agent Loop**
  POST a message, get 202 immediately. The agent reasons → calls tools → responds. Results stream via SSE in real-time.

- **Tree-Based Conversation History**
  Conversations branch like git. Editing a message creates a new branch. Fork sessions at any point. Select any branch as active.

- **Agent Delegation**
  Agents can spawn sub-agents with the `delegate` tool. Parent agent tracks delegation results. Cycle detection prevents infinite loops.

- **Memory System**
  Agents store and recall key-value facts scoped to their name. Persisted in SQLite across sessions.

- **Tool Permissions**
  Each agent can allow or deny specific tools via YAML frontmatter configuration.

- **OpenAI-Compatible API**
  `/v1/chat/completions` and `/v1/models` endpoints for drop-in compatibility with existing OpenAI SDK clients.

- **SSE Event Streaming**
  Real-time events for message progress, tool execution, and session status. Global + per-session event streams.

- **Markdown Agent Configs**
  Agents defined as `.md` files with YAML frontmatter. No code required to add new agents.

---

## Architecture

```text
POST /session/:id/message ──→ HTTP API ──→ Agent Engine ──→ [web_fetch | memory | delegate] ──→ LLM Provider
                                          ↑                                                  │
                                          │  ← tool result ←─────────────────────────────────┘
                                          │
                                          └──→ SQLite (tree history, memory, agents)
                                          └──→ Event Bus ──→ SSE streams ──→ Client
                                          (ncruces/go-sqlite3 + Goose + sqlc)
```

See `DOCS/diagrams/` for full architecture diagrams.

---

## Quick Start

### 1. Clone & Build
```bash
git clone https://github.com/.../gopengai
cd gopengai
go build ./cmd/api/
```

### 2. Configure
```bash
cp gopengai.json.example gopengai.json
# Edit gopengai.json — set LLM provider, model, API key
```

**gopengai.json:**
```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080
  },
  "llm": {
    "provider": "openai",
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-...",
    "model": "gpt-4o",
    "max_iterations": 10
  },
  "agents_dir": "./agents",
  "data_dir": "./.gopengai",
  "default_agent": "default"
}
```

### 3. Run
```bash
./api
```

### 4. Verify
```bash
curl http://localhost:8080/health
```

---

## API Usage

### Send a Message (Async + SSE)

```bash
# Subscribe to events
curl -N http://localhost:8080/session/ses_abc/events

# Send message (returns 202 immediately)
curl -X POST http://localhost:8080/session/ses_abc/message \
  -H "Content-Type: application/json" \
  -d '{"content": "Explain Go generics"}'
```

### Create a Session

```bash
curl -X POST http://localhost:8080/session \
  -H "Content-Type: application/json" \
  -d '{"title": "Go help", "agent_name": "default"}'
```

### OpenAI-Compatible

```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "default",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

### List Agents

```bash
curl http://localhost:8080/agents
```

---

## Agent Configuration

Agents are `.md` files in the `agents/` directory with YAML frontmatter:

**agents/researcher.md:**
```markdown
---
name: researcher
model: "anthropic/claude-sonnet-4-20250514"
tools:
  - web_fetch
  - memory_save
  - memory_recall
permissions:
  web_fetch: allow
  memory_save: allow
  memory_recall: allow
parent_agent: ""
---

You are a research assistant. When given a topic, search the web for information,
save key findings to memory, and provide a well-structured summary.
```

---

## Project Structure

```text
├── cmd/
│   ├── api/main.go          # HTTP server entrypoint
│   └── cli/main.go          # CLI client (Cobra)
├── internal/
│   ├── api/                 # HTTP handlers, routes, SSE, middleware
│   │   ├── handler.go
│   │   ├── routes.go
│   │   ├── events.go        # Event bus + SSE writer
│   │   └── middleware.go
│   ├── agent/               # Core: engine loop, .md loader, registry
│   │   ├── engine.go
│   │   ├── loader.go
│   │   ├── registry.go
│   │   └── types.go
│   ├── tools/               # Tool interface + implementations
│   │   ├── registry.go
│   │   ├── web_fetch.go
│   │   ├── memory.go
│   │   └── delegate.go
│   ├── history/             # Tree-based conversation history
│   │   ├── tree.go
│   │   ├── repo.go
│   │   └── branch.go
│   ├── llm/                 # OpenAI-compatible HTTP client
│   │   ├── client.go
│   │   ├── types.go
│   │   └── stream.go
│   ├── db/                  # SQLite + sqlc + Goose
│   │   ├── connect.go       # SQLite connection (ncruces/go-sqlite3, pragmas)
│   │   ├── embed.go         # Embeds migrations/ into binary
│   │   ├── migrations/      # Goose SQL migration files
│   │   ├── sql/             # Raw SQL queries (sqlc input)
│   │   ├── db.go            # sqlc-generated Queries struct
│   │   ├── models.go        # sqlc-generated Go structs
│   │   ├── querier.go       # sqlc-generated Querier interface
│   │   └── *.sql.go         # sqlc-generated query implementations
│   └── config/
│       └── config.go        # gopengai.json loading
├── agents/                  # Agent .md configs
│   ├── default.md
│   └── examples/
│       ├── researcher.md
│       ├── analyst.md
│       └── summarizer.md
├── gopengai.json.example
├── .gitignore                  # Ignores .gopengai/, binary, etc.
├── sqlc.yaml                   # sqlc configuration (SQLite engine)
├── go.mod
└── DOCS/
    └── diagrams/            # Architecture diagrams (Mermaid)
```

---

## API Reference

See `DOCS/diagrams/07-rest-api.md` for the complete endpoint reference.

| Category   | Endpoints                                                    |
|------------|--------------------------------------------------------------|
| Global     | `GET /health`, `GET /event`                                  |
| Sessions   | `GET/POST /session`, `GET/PATCH/DELETE /session/:id`         |
| Messages   | `POST /session/:id/message`, `GET /session/:id/messages`     |
| Branches   | `GET /session/:id/branches`, `POST /session/:id/fork`        |
| Agents     | `GET /agents`, `GET /agents/:name`                           |
| Memory     | `GET /memory`, `GET /memory/:key`                            |
| OpenAI     | `POST /v1/chat/completions`, `GET /v1/models`                |
| Control    | `POST /session/:id/abort`                                    |

---

## Roadmap

| Phase | What                            | Status   |
|-------|---------------------------------|----------|
| 0     | Project bootstrap               | Not started |
| 1     | Config & database layer         | Not started |
| 2     | LLM client layer                | Not started |
| 3     | Agent types & loader            | Not started |
| 4     | History tree (conversations)    | Not started |
| 5     | Tool registry & implementations | Not started |
| 6     | Agent engine (core loop)        | Not started |
| 7     | HTTP API + SSE events           | Not started |
| 8     | CLI client                      | Not started |
| 9     | Testing & quality               | Not started |
| 10    | Documentation & polish          | Not started |

See `TODO.md` for the detailed task breakdown.

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).
