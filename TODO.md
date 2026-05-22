# NLP Core — Implementation TODO

> **Based on:** 6 architecture diagrams (01-container through 06-package-structure) + README  
> **Tech Stack:** Go 1.21+, SQLite3, Cobra CLI, net/http  
> **Approach:** Pure Go — no Python. All phases for semester 4 delivery. Local dev deployment.  
> **Order:** Sequential phases. Each phase builds on the previous.

---

## Phase 0: Project Bootstrap
**Dependencies:** None
**Goal:** Initialize Go module, create directory structure, verify build

- [ ] `go mod init nlpcore`
- [ ] Create directory structure per diagram `06-package-structure`:
  ```
  ├── cmd/api/main.go
  ├── cmd/cli/main.go
  ├── internal/
  │   ├── api/       (handler, middleware, routes)
  │   ├── agent/     (engine, loader, registry, types)
  │   ├── tools/     (registry, web_fetch, memory, delegate)
  │   ├── history/   (tree, repo, branch)
  │   ├── llm/       (client, types, stream)
  │   ├── db/        (db, migrations, memory repo, agent repo)
  │   └── config/    (config)
  └── agents/default.md
  ```
- [ ] Add dependencies: `github.com/mattn/go-sqlite3`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`
- [ ] Verify `go build ./cmd/api/` and `go build ./cmd/cli/` succeed (empty main files)

---

## Phase 1: Configuration & Database Layer
**Dependencies:** Phase 0
**Goal:** Config loading from env/flags, SQLite connection, schema migrations, basic CRUD

### 1.1 Configuration (`internal/config/config.go`)
- [ ] Define `Config` struct: `Port`, `LLMEndpoint`, `LLMModel`, `LLMToken`, `DBPath`, `AgentsDir`
- [ ] Load from environment variables with defaults (`NLP_PORT=8080`, `NLP_LLM_MODEL=`)
- [ ] Support CLI flag overrides

### 1.2 Database Connection (`internal/db/db.go`)
- [ ] `Open(path string) (*sql.DB, error)` — open SQLite with WAL mode, foreign keys enabled
- [ ] Connection pooling settings

### 1.3 Migrations (`internal/db/migrations.go`)
- [ ] Create tables per ERD (diagram `05-database-erd`):
  - `sessions` — id PK, agent_name, title, active_leaf_id FK, created_at, updated_at
  - `messages` — id PK, session_id FK, parent_id FK (self), role, content, agent_name, tool_name, tool_call_id, tool_args JSON, token_count, created_at
  - `agents` — name PK, system_prompt, tools JSON, model, parent_agent, config_path, loaded_at
  - `memory` — id PK, agent_name FK, key, value, category, created_at, updated_at
  - `delegation_logs` — id PK, parent_message_id FK, child_agent_name FK, child_session_id, task_description, result_summary, duration_ms, created_at
- [ ] `Migrate(db *sql.DB) error` — idempotent (CREATE TABLE IF NOT EXISTS)

### 1.4 Session Repository (inline in `internal/db/db.go` or separate)
- [ ] `CreateSession(id string, agentName string) error`
- [ ] `GetSession(id string) (*Session, error)`
- [ ] `UpdateSession(id string, updates...) error`

### 1.5 Memory Repository (`internal/db/memory.go`)
- [ ] `SaveMemory(agentName, key, value, category string) error`
- [ ] `GetMemory(agentName, key string) (*Memory, error)`
- [ ] `ListMemory(agentName string) ([]Memory, error)`

### 1.6 Agent Repository (`internal/db/agent.go`)
- [ ] `SaveAgent(agentName, systemPrompt, tools, model, configPath string) error`
- [ ] `GetAgent(name string) (*Agent, error)`
- [ ] `ListAgents() ([]Agent, error)`

---

## Phase 2: LLM Client Layer
**Dependencies:** Phase 1  
**Goal:** OpenAI-compatible HTTP client for LLM calls with tool support

### 2.1 LLM Types (`internal/llm/types.go`)
- [ ] Define structs: `CompletionRequest`, `Message`, `ToolDefinition`, `ToolFunction`, `CompletionResponse`, `Choice`, `ToolCall`, `Usage`
- [ ] Match OpenAI `/v1/chat/completions` request/response format

### 2.2 LLM Client (`internal/llm/client.go`)
- [ ] `Client` struct with base URL, API key, model
- [ ] `ChatCompletion(ctx, *CompletionRequest) (*CompletionResponse, error)` — HTTP POST
- [ ] Support `tool_choice: "auto"` for tool calling
- [ ] Error handling for non-200 responses

### 2.3 Streaming Skeleton (`internal/llm/stream.go`)
- [ ] SSE parsing infrastructure (can be empty struct for now)
- [ ] `StreamCompletion(ctx, *CompletionRequest) (<chan *CompletionResponse, error)`
- [ ] Mark as future feature — focus on non-streaming first

---

## Phase 3: Agent Types & Loader
**Dependencies:** Phase 2  
**Goal:** Define agent data types, parse `.md` config files with YAML frontmatter, build registry

### 3.1 Agent Types (`internal/agent/types.go`)
- [ ] `Agent` struct: `Name`, `SystemPrompt`, `Tools []string`, `Model`, `ParentAgent`, `ConfigPath`
- [ ] `Message` struct (for in-memory): `Role`, `Content`, `ToolCalls`, `ToolCallID`
- [ ] `ToolCall` struct: `ID`, `Name`, `Arguments`
- [ ] `Response` struct: `Content`, `Sources`, `Usage`, `StopReason`

### 3.2 Agent Loader (`internal/agent/loader.go`)
- [ ] `LoadAgent(path string) (*Agent, error)` — read `.md` file, parse YAML frontmatter (between `---` markers)
- [ ] YAML frontmatter fields: `name`, `system_prompt`, `tools`, `model`, `parent_agent`
- [ ] Body of `.md` file = system prompt (if not in frontmatter)
- [ ] `LoadDirectory(dir string) (map[string]*Agent, error)` — scan all `.md` files

### 3.3 Agent Registry (`internal/agent/registry.go`)
- [ ] In-memory `Registry` with `map[string]*Agent`
- [ ] `Register(agent *Agent)`
- [ ] `Get(name string) (*Agent, error)`
- [ ] `List() []Agent`
- [ ] `InitializeFromDir(dir string) error` — convenience wrapper around Loader

### 3.4 Default Agent Config
- [ ] Create `agents/default.md` with YAML frontmatter:
  ```yaml
  ---
  name: default
  system_prompt: You are a helpful AI assistant. Answer questions concisely and accurately.
  tools: []
  model: ""
  ---
  ```

---

## Phase 4: History Tree (Conversation Management)
**Dependencies:** Phase 1, Phase 3  
**Goal:** Tree-structured conversation history with branch support, traversal, and branch selection

### 4.1 Repository (`internal/history/repo.go`)
- [ ] `InsertMessage(db, msg) error` — INSERT into messages table
- [ ] `GetMessagesForSession(db, sessionID) ([]Message, error)` — all messages for session
- [ ] `GetBranchFromRootTo(db, messageID) ([]Message, error)` — root → node traversal

### 4.2 Tree Operations (`internal/history/tree.go`)
- [ ] `BuildTree(messages) *TreeNode` — construct in-memory tree from flat list
- [ ] `FindActiveLeaf(tree) *TreeNode` — longest root→leaf path (default selection)
- [ ] `GetPathFromRoot(tree, node) []Message` — traverse root to node
- [ ] `InsertNode(tree, parentID, newMessage) *TreeNode` — add child, update active_leaf

### 4.3 Branch Management (`internal/history/branch.go`)
- [ ] `GetAllLeaves(tree) []*TreeNode` — find all leaf nodes
- [ ] `SelectBranch(db, sessionID, leafMessageID) error` — set active_leaf_id
- [ ] `EditMessage(db, originalMsgID, newContent) (newMsgID, error)` — insert as new branch from parent of original (immutable messages)

### 4.4 Session Context Builder
- [ ] `BuildContext(db, sessionID) ([]llm.Message, error)` — load branch, convert to LLM messages array
- [ ] Include system prompt from agent

---

## Phase 5: Tool Registry & Implementations
**Dependencies:** Phase 2, Phase 3, Phase 4  
**Goal:** Tool interface, registry, and 3 tool implementations

### 5.1 Tool Interface (`internal/tools/registry.go`)
- [ ] Define `Tool` interface per diagram `06-package-structure`:
  ```go
  type Tool interface {
      Name() string
      Description() string
      Parameters() json.RawMessage  // JSON Schema
      Execute(ctx context.Context, args json.RawMessage) (string, error)
  }
  ```
- [ ] `Registry` struct: `map[string]Tool`
- [ ] `Register(tool Tool)`
- [ ] `Get(name string) (Tool, error)`
- [ ] `ToToolDefinitions() []llm.ToolDefinition` — convert to LLM API format

### 5.2 Web Fetch Tool (`internal/tools/web_fetch.go`)
- [ ] `WebFetchTool` implementing `Tool` interface
- [ ] `Parameters()`: `{ "type": "object", "properties": { "url": { "type": "string" } }, "required": ["url"] }`
- [ ] `Execute`: HTTP GET URL, extract text content (strip HTML), return first N chars
- [ ] User-Agent header, timeout (10s), max response size (50KB)

### 5.3 Memory Tools (`internal/tools/memory.go`)
- [ ] `MemorySave` tool:
  - Parameters: `{ "key": "string", "value": "string", "category": "string" }`
  - Execute: call `db.SaveMemory(agentName, key, value, category)`
- [ ] `MemoryRecall` tool:
  - Parameters: `{ "key": "string" }` (empty = list all)
  - Execute: call `db.GetMemory` or `db.ListMemory`
- [ ] Both scoped to current agent_name from context

### 5.4 Delegate Tool (`internal/tools/delegate.go`)
- [ ] `DelegateTool` implementing `Tool` interface
- [ ] Parameters: `{ "agent_name": "string", "task": "string" }`
- [ ] Execute: load sub-agent from registry, build new context, call engine recursively
- [ ] Log delegation to `delegation_logs` table
- [ ] Timeout protection (30s max for sub-agent)

---

## Phase 6: Agent Engine (Core Loop)
**Dependencies:** Phase 2, Phase 3, Phase 4, Phase 5  
**Goal:** Core agent loop: reason → tool call → respond. This is the heart of the system.

### 6.1 Engine (`internal/agent/engine.go`)
- [ ] `Engine` struct with dependencies: `llm.Client`, `ToolRegistry`, `history.Manager`, `agent.Registry`, `*sql.DB`
- [ ] `Run(ctx, sessionID, message, agentName) (*Response, error)`:
  1. Ensure session exists (create if not)
  2. Load agent from registry
  3. Build context: system prompt + branch history + new user message
  4. Convert to LLM messages array
  5. Loop (max N iterations, e.g., 10):
     a. Call LLM with tool definitions
     b. If `stop_reason == "stop"` → save assistant message → return response
     c. If `tool_calls` → for each tool call:
        - Find tool in registry
        - Execute tool
        - Save tool call message + tool result message to history
        - Append to context
     d. Continue loop
- [ ] Save all messages to SQLite (see flowchart `02-agent-loop`)
- [ ] Update `active_leaf_id` on session after saving

### 6.2 Message Persistence
- [ ] On user message: `InsertMessage` with parent = current active_leaf
- [ ] On assistant message: `InsertMessage` with parent = user message
- [ ] On tool calls: `InsertMessage` (role=assistant, tool_calls) → parent = user message
- [ ] On tool results: `InsertMessage` (role=tool, tool_call_id) → parent = assistant tool call

### 6.3 Token Counting & Usage Tracking
- [ ] Track token_count per message (from LLM response `usage` field)
- [ ] Aggregate in Response struct

---

## Phase 7: HTTP API Server
**Dependencies:** Phase 1, Phase 6  
**Goal:** REST API with native endpoints + OpenAI-compatible endpoints

### 7.1 Routes (`internal/api/routes.go`)
- [ ] `RegisterRoutes(mux, apiHandler, ...)` — wire all routes

### 7.2 Handlers (`internal/api/handler.go`)

**Native endpoints:**
- [ ] `POST /api/v1/health` → `{"status": "ok", "uptime": "..."}`
- [ ] `POST /api/v1/chat` → request: `{user_id, session_id?, message, agent?}` → response: `{id, content, sources, usage, provider}`
- [ ] `POST /api/v1/sessions` → create session → `{id, title, agent_name}`
- [ ] `GET /api/v1/sessions/:id` → get session with active branch
- [ ] `GET /api/v1/sessions/:id/branches` → list all leaves
- [ ] `PUT /api/v1/sessions/:id/branch` → select branch `{leaf_id}`
- [ ] `PATCH /api/v1/messages/:id` → edit message `{new_content}` → creates new branch
- [ ] `GET /api/v1/agents` → list available agents
- [ ] `GET /api/v1/memory` → list memory facts

**OpenAI-compatible endpoints:**
- [ ] `POST /v1/chat/completions` → accept OpenAI format, map to internal engine, return OpenAI format
- [ ] `GET /v1/models` → return available models from config

### 7.3 Middleware (`internal/api/middleware.go`)
- [ ] `LoggingMiddleware` — log method, path, status, duration
- [ ] `CORSHeaders` — Allow-Origin: *, Allow-Methods, Allow-Headers
- [ ] `AuthMiddleware` skeleton (future: API key / JWT — accept but don't enforce for now)

### 7.4 Server Entrypoint (`cmd/api/main.go`)
- [ ] Load config
- [ ] Open database + run migrations
- [ ] Initialize agent registry from `agents/` directory
- [ ] Initialize tool registry
- [ ] Create engine + LLM client
- [ ] Create API handler + register routes
- [ ] Start HTTP server on configured port
- [ ] Graceful shutdown (SIGINT/SIGTERM)

---

## Phase 8: CLI Client
**Dependencies:** Phase 7  
**Goal:** Cobra-based CLI client with chat, agent, history, and memory commands

### 8.1 CLI Entrypoint (`cmd/cli/main.go`)
- [ ] Cobra root command: `nlp` with `--server-url` flag (default `http://localhost:8080`)

### 8.2 Chat Command
- [ ] `nlp chat "message" [--session-id ID] [--agent NAME]` → send message, display response
- [ ] `nlp chat` (interactive mode) → REPL loop with readline, show conversation
- [ ] Auto-create session on first message

### 8.3 Session/History Commands
- [ ] `nlp sessions` → list all sessions
- [ ] `nlp sessions get <id>` → show active branch
- [ ] `nlp sessions branches <id>` → list all leaves
- [ ] `nlp sessions switch <id> --leaf <leaf_id>` → select branch

### 8.4 Agent Commands
- [ ] `nlp agents` → list available agents
- [ ] `nlp agents info <name>` → show agent details

### 8.5 Memory Commands
- [ ] `nlp memory list [--agent NAME]` → show memory facts
- [ ] `nlp memory get <key>` → get specific fact

---

## Phase 9: Testing & Quality
**Dependencies:** Phase 1-8 (parallel with development)  
**Goal:** Unit tests, integration tests, API tests

### 9.1 Unit Tests
- [ ] `internal/db/` — test migrations, CRUD operations (use in-memory SQLite)
- [ ] `internal/history/tree.go` — test tree construction, branch selection, edit→new-branch
- [ ] `internal/agent/loader.go` — test YAML frontmatter parsing
- [ ] `internal/tools/` — test each tool's Execute with mock dependencies
- [ ] `internal/agent/engine.go` — test loop logic with mock LLM client

### 9.2 Integration Tests
- [ ] Test full chat flow: POST /api/v1/chat → engine → LLM → response
- [ ] Test OpenAI-compatible endpoint format
- [ ] Test branch creation via message edit

### 9.3 Test Infrastructure
- [ ] Mock HTTP server for LLM responses (net/http/httptest)
- [ ] Temporary SQLite databases per test

---

## Phase 10: Documentation & Polish
**Dependencies:** Phase 1-9  
**Goal:** README, example agents, .env.example, final clean-up

### 10.1 Documentation
- [ ] Update `README.md` with actual API examples, CLI usage
- [ ] Create `agents/examples/` with pre-built agents:
  - `researcher.md` — tools: web_fetch, memory
  - `analyst.md` — tools: memory, delegate
  - `summarizer.md` — tools: [] (no tools, simple)
- [ ] Create `.env.example` with all configurable variables

### 10.2 Code Quality
- [ ] `go vet ./...` — clean
- [ ] `go fmt ./...` — formatted
- [ ] Add `Makefile` with common commands: `make run`, `make test`, `make lint`, `make build`

---

## Architecture Risk Summary

| Risk | Impact | Mitigation |
|------|--------|------------|
| SQLite concurrency (multiple requests) | Medium | Use WAL mode, connection pool, transactions for writes |
| LLM tool calling format differences across providers | High | Abstract LLM client, test with target provider early |
| Infinite agent loop (LLM keeps calling tools) | Medium | Max iterations (10), timeout per iteration |
| Recursive delegation (agent delegates to itself) | Medium | Detect cycles in delegation chain (visited set) |
| Tree operations become slow with large conversations | Low (MVP) | Add indexes on (session_id, parent_id), pagination for long branches |
| Large context windows exceed LLM limits | Medium | Truncate history, keep only active branch, summarize old messages |

## Dependency Graph (Build Order)

```
Phase 0 (Bootstrap)
  └── Phase 1 (Config + DB)
        ├── Phase 2 (LLM Client)
        │     └── Phase 6 (Agent Engine)
        │           └── Phase 7 (HTTP API)
        │                 └── Phase 8 (CLI)
        └── Phase 3 (Agent Types + Loader)
              └── Phase 5 (Tools)
        └── Phase 4 (History Tree)
  Phase 9 (Testing) — runs in parallel
  Phase 10 (Docs) — last
```

## Priority Stack (if time-constrained)

1. **Must Have:** Phases 0-7 (working API, agent loop, LLM calls, SQLite, basic tools)
2. **Should Have:** Phase 8 (CLI client), Phase 9 (tests)
3. **Nice to Have:** Advanced tool features, delegation, streaming, memory recall
