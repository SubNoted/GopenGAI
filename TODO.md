# GoPengAI — Implementation TODO

> **Last synced:** 2026-05-27 (Phase 6 + 7 EventBus complete; Phase 7 + 8 partial: sync session CRUD + CLI built; bug fixes: input validation, ToolCalls priority, stuck session defer, atomic DeleteSession tx, sanitized error leakage, Subscribe-after-Close, second-signal force-kill, tool 30s timeout)
> **Based on:** 10 architecture diagrams (01-container through 10-gopengai-container)
> **Tech Stack:** Go 1.21+, SQLite3 (ncruces/go-sqlite3), sqlc, Goose, Cobra CLI, net/http, SSE
> **Approach:** Pure Go — no CGo, no Python. All phases for semester 4 delivery. Local dev deployment.
> **API Design:** Adapted OpenCode hybrid — async message POST (202) + SSE streaming + tree-based history
> **DB Design:** Adapted OpenCode SQLite model — 3 base tables extended for agents, memory, delegation
> **Order:** Sequential phases. Each phase builds on the previous.

## Overall Progress: ~76%

```
Phase 0 (Bootstrap)    ██████████ 100%  (complete)
Phase 1 (Config+DB)    ██████████ 100%  (complete)
Phase 2 (LLM Client)   ██████████ 100%  (complete)
Phase 3 (Agent Types)  ██████████ 100%  (complete)
Phase 4 (History Tree) ██████████ 100%  (complete)
Phase 5 (Tools)        ██████████ 100%  (complete)
Phase 6 (Agent Engine) ██████████ 100%  (engine loop, EventBus, abort, tool execution, wiring)
Phase 7 (HTTP API)     ████████░░  80%  (sync session CRUD + linear chat + EventBus + graceful shutdown built; SSE/async/branches still stubs)
Phase 8 (CLI)          ██░░░░░░░░  20%  (sync chat + session commands built; no SSE streaming/agents/memory/branches)
Phase 9 (Testing)      ░░░░░░░░░░   0%
Phase 10 (Docs)        █████░░░░░  50%  (README, diagrams, Makefile, ЧТО_НУЖНО_ЗНАТЬ done; no agent examples)
```

---

## Phase 0: Project Bootstrap
**Dependencies:** None
**Goal:** Rename to gopengai, initialize Go module, create directory structure, verify build

- [x] `go mod init gopengai`
- [x] Create directory structure per diagram `06-package-structure`:
  ```
  ├── cmd/api/main.go
  ├── cmd/cli/main.go
  ├── internal/
  │   ├── api/       (handler, middleware, routes, events)
  │   ├── agent/     (engine, loader, registry, types)
  │   ├── tools/     (registry, web_fetch, memory, delegate)
  │   ├── history/   (tree, repo, branch)
  │   ├── llm/       (client, types, stream)
  │   ├── db/
  │   │   ├── connect.go        (SQLite connection + pragmas)
  │   │   ├── embed.go          (embed migrations/ into binary)
  │   │   ├── migrations/       (Goose SQL migration files)
  │   │   │   ├── 001_initial.sql
  │   │   │   └── ...
  │   │   ├── sql/              (raw SQL queries for sqlc)
  │   │   │   ├── sessions.sql
  │   │   │   ├── messages.sql
  │   │   │   ├── agents.sql
  │   │   │   ├── memory.sql
  │   │   │   └── delegation_logs.sql
  │   │   ├── db.go             (sqlc-generated Queries struct)
  │   │   ├── models.go         (sqlc-generated Go structs)
  │   │   ├── querier.go        (sqlc-generated Querier interface)
  │   │   └── *.sql.go          (sqlc-generated query implementations)
  │   └── config/    (config)
  ├── sqlc.yaml                  (sqlc configuration)
  └── agents/
      ├── default.md
      └── examples/
          ├── researcher.md
          ├── analyst.md
          └── summarizer.md
  ```
- [x] Add dependencies: `github.com/ncruces/go-sqlite3`, `github.com/pressly/goose/v3`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`
- [x] Create `gopengai.json.example` with server, LLM, agents_dir, data_dir, default_agent fields
- [x] Create `.gitignore`:
  ```
  # Binary
  /gopengai
  /api
  /cli
  *.exe

  # Data directory (per-project SQLite)
  .gopengai/

  # Go
  vendor/

  # IDE
  .idea/
  .vscode/
  .zed/
  ```
- [x] Verify `go build ./cmd/api/` and `go build ./cmd/cli/` succeed (empty main files)
- [x] Verify `go vet ./...` passes

---

## Phase 1: Configuration & Database Layer
**Dependencies:** Phase 0
**Goal:** Config loading from gopengai.json, SQLite connection, Goose migrations, sqlc-generated CRUD

### 1.1 Configuration (`internal/config/config.go`)
- [x] Define `Config` struct matching `gopengai.json` schema (ServerConfig, LLMConfig, AgentsDir, DataDir, DefaultAgent)
- [x] `Load(path string) (*Config, error)` — read JSON file, apply defaults
- [x] Env var overrides: `GOPENGAI_PORT`, `GOPENGAI_LLM_API_KEY`, etc.
- [ ] CLI flag overrides: `--port`, `--config`

### 1.2 Database Connection (`internal/db/connect.go`)
- [x] `Open(path string) (*sql.DB, error)` — open SQLite via `ncruces/go-sqlite3` (pure Go, no CGo)
- [x] Set pragmas on connection: `foreign_keys=ON`, `journal_mode=WAL`, `page_size=4096`, `cache_size=-8000`, `synchronous=NORMAL`, `busy_timeout=5000`
- [x] Connection pooling: `SetMaxOpenConns(1)` (SQLite single-writer), `SetMaxIdleConns(1)`

### 1.3 Migrations (`internal/db/migrations/`)
- [x] Use **Goose** (`github.com/pressly/goose/v3`) for migration management
- [x] Embed migrations in binary via `go:embed`
- [x] `Migrate(db *sql.DB) error` — runs `goose.Up()` on startup
- [x] **Migration 1: `001_initial.sql`** — all 5 tables, 5 triggers, 4 indexes, foreign keys (fully written)
- [x] **Triggers** (auto-update timestamps, message counts):
  - `update_sessions_updated_at` — on session update
  - `update_messages_updated_at` — on message update
  - `update_memory_updated_at` — on memory update
  - `update_session_message_count_on_insert` — increment on message insert
  - `update_session_message_count_on_delete` — decrement on message delete
- [x] **Indexes**:
  - `idx_messages_session_id` on messages (session_id)
  - `idx_messages_parent_id` on messages (parent_id)
  - `idx_memory_agent_name` on memory (agent_name)
  - `idx_delegation_logs_parent` on delegation_logs (parent_message_id)

### 1.4 sqlc Setup (`sqlc.yaml`)
- [x] Configure sqlc v1.29+ for SQLite engine — `sqlc.yaml` fully written
- [x] Write raw SQL queries in `internal/db/sql/*.sql`:
  - `sessions.sql` — CreateSession, GetSessionByID, ListSessions, UpdateSession, DeleteSession
  - `messages.sql` — CreateMessage, GetMessage, ListMessagesBySession, GetBranchFromRootTo (recursive CTE), GetAllLeaves, UpdateMessage, DeleteMessage, DeleteSessionMessages
  - `agents.sql` — CreateAgent, GetAgent, ListAgents, DeleteAgent
  - `memory.sql` — CreateMemory, GetMemory, ListMemoryByAgent, DeleteMemory
  - `delegation_logs.sql` — CreateDelegationLog, ListDelegationLogsBySession
- [x] Run `sqlc generate` to produce Go code (requires `go.mod` + dependencies first)

### 1.5 Generated Querier Interface (`internal/db/querier.go`)
- [x] sqlc-generated `Querier` interface (generated by `sqlc generate`)

### 1.6 Data Directory Setup
- [x] Default data dir: `.gopengai/` (per-project, gitignored)
- [x] `.gopengai/` already in `.gitignore`

---

## Phase 2: LLM Client Layer
**Dependencies:** Phase 1
**Goal:** OpenAI-compatible HTTP client for LLM calls with tool support

### 2.1 LLM Types (`internal/llm/types.go`)
- [x] Define structs: `ChatCompletionRequest`, `Message`, `ChatCompletionResponse`, `Choice`, `Usage`, `APIError` — basic OpenAI-compatible types with correct JSON tags
- [x] Add `ToolDefinition`, `ToolFunction`, `ToolCall`, `MessageResponse` structs for tool calling support

### 2.2 LLM Client (`internal/llm/client.go`)
- [x] `Client` struct with `BaseURL`, `APIKey`, `Model`, `HTTPClient`
- [x] `NewClient(baseURL, apiKey, model string) *Client`
- [x] `ChatCompletion(ctx, messages) (*ChatCompletionResponse, error)` — HTTP POST with context + error handling
- [x] Support `tool_choice: "auto"` for tool calling
- [x] Structured error type for non-200 responses (currently plain `fmt.Errorf`)
- [x] Accept `config.LLMConfig` instead of 3 separate params in `NewClient`

### 2.3 Streaming Skeleton (`internal/llm/stream.go`)
- [x] SSE parsing infrastructure
- [x] `StreamCompletion(ctx, *CompletionRequest) (<-chan *CompletionResponse, error)`
- [x] Mark as future feature — focus on non-streaming first

---

## Phase 3: Agent Types & Loader
**Dependencies:** Phase 2
**Goal:** Define agent data types, parse `.md` config files with YAML frontmatter, build registry

### 3.1 Agent Types (`internal/agent/types.go`)
- [x] `Agent` struct: `Name`, `SystemPrompt`, `Tools []string`, `Model`, `ParentAgent`, `Permissions map[string]string`, `ConfigPath`
- [x] `Message` struct (in-memory): `Role`, `Content`, `ToolCalls`, `ToolCallID`, `Name`
- [x] `ToolCall` struct: `ID`, `Name`, `Arguments`
- [x] `Response` struct: `Content`, `Usage`, `StopReason`, `Error`
- [x] Helper methods: `HasTool()`, `IsToolAllowed()` on `Agent`

### 3.2 Agent Loader (`internal/agent/loader.go`)
- [x] `LoadAgent(path string) (*Agent, error)` — read `.md` file, parse YAML frontmatter
- [x] YAML frontmatter fields: `name`, `model`, `tools`, `parent_agent`, `permissions`
- [x] Body of `.md` file = system prompt (if not in frontmatter `system_prompt` field)
- [x] Parse `permissions` as `map[string]string` (`tool_name → "allow"/"deny"`)
- [x] `LoadDirectory(dir string) (map[string]*Agent, error)` — scan all `.md` files

### 3.3 Agent Registry (`internal/agent/registry.go`)
- [x] In-memory `Registry` with `map[string]*Agent`
- [x] `Register(agent *Agent)`
- [x] `Get(name string) (*Agent, error)`
- [x] `List() []Agent`
- [x] `Names() []string`
- [x] `Has(name string) bool`
- [x] `Size() int`
- [x] `InitializeFromDir(dir string) (int, error)` — convenience wrapper around Loader

### 3.4 Default Agent Config
- [x] Create `agents/default.md`:
  ```markdown
  ---
  name: default
  tools: []
  permissions: {}
  ---

  You are a helpful AI assistant. Answer questions concisely and accurately.
  ```
- [ ] Create `agents/examples/researcher.md` with web_fetch + memory tools allowed
- [ ] Create `agents/examples/analyst.md` with memory + delegate tools allowed
- [ ] Create `agents/examples/summarizer.md` with no tools

---

## Phase 4: History Tree (Conversation Management)
**Dependencies:** Phase 1, Phase 3
**Goal:** Tree-structured conversation history with branch support (uses sqlc-generated queries)

### 4.1 Repository Wrapper (`internal/history/repo.go`)
- [x] Thin wrapper around sqlc-generated `db.Querier` for history-specific operations
- [x] `InsertMessage(ctx, params) (Message, error)` — delegates to `db.CreateMessage`
- [x] `GetMessagesForSession(ctx, sessionID) ([]Message, error)` — delegates to `db.ListMessagesBySession`
- [x] `GetActiveBranch(ctx, sessionID) ([]Message, error)` — loads active branch via recursive CTE or tree fallback
- [x] `GetActiveBranchByLeafID(ctx, leafID) ([]Message, error)` — delegates to `db.GetBranchFromRootTo` (recursive CTE)
- [x] `GetAllLeaves(ctx, sessionID) ([]Message, error)` — delegates to `db.GetAllLeaves`
- [x] `GetMessageByID(ctx, id) (Message, error)` — delegates to `db.GetMessage`
- [x] `UpdateActiveLeaf(ctx, sessionID, leafID) error` — sets active_leaf_id without read-modify-write race

### 4.2 Tree Operations (`internal/history/tree.go`)
- [x] `BuildTree(messages) []*TreeNode` — construct in-memory tree from flat list (handles mixed roots + orphans)
- [x] `GetLongestLeaf(roots) *TreeNode` — longest root→leaf path (default active branch)
- [x] `GetPathFromRoot(node) []*TreeNode` — traverse root to node via Parent pointer
- [x] `InsertNode(roots, parentID, msg) *TreeNode` — add child to in-memory tree
- [x] `FindNode(roots, id) *TreeNode` — BFS node lookup
- [x] `GetLeafByID(roots, id) *TreeNode` — find leaf by ID
- [x] `IsLeaf(node) bool` — leaf predicate
- [x] `ToAgentMessages(messages) []agent.Message` — convert DB messages to agent messages
- [x] `TreeNode` struct with `Parent` pointer for upward traversal

### 4.3 Branch Management (`internal/history/branch.go`)
- [x] `SelectLeaf(ctx, sessionID, leafID) error` — set active_leaf_id with ownership & leaf validation
- [x] `EditMessage(ctx, params) (newMsgID, error)` — new branch from parent of original (in transaction)
- [x] `ForkSession(ctx, params) (newSessionID, error)` — create new session branching from point (in transaction)
- [x] Input validation: Role allowlist, Content size limit (100KB), FromMessageID ownership check
- [x] All write operations wrapped in `sql.Tx` with atomic commit/rollback

### 4.4 Session Context Builder (`internal/history/context.go`)
- [x] `BuildContext(ctx, sessionID, systemPrompt, maxTokens) ([]llm.Message, error)` — load branch, prepend system prompt, convert to LLM messages
- [x] Truncate if branch history exceeds context window limit (oldest messages dropped first, system prompt preserved)
- [x] Token estimation heuristic (~4 chars/token)

---

## Phase 5: Tool Registry & Implementations
**Dependencies:** Phase 2, Phase 3, Phase 4
**Goal:** Tool interface, registry, 3 tool implementations, permission checking

### 5.1 Tool Interface (`internal/tools/registry.go`)
- [x] Define `Tool` interface:
  ```go
  type Tool interface {
      Name() string
      Description() string
      Parameters() json.RawMessage  // JSON Schema
      Execute(ctx context.Context, args json.RawMessage) (string, error)
  }
  ```
- [x] `Registry` struct: `map[string]Tool`
- [x] `Register(tool Tool)`
- [x] `Get(name string) (Tool, error)`
- [x] `ToToolDefinitions() []llm.ToolDefinition` — convert to LLM API format
- [x] `IsAllowed(toolName string, permissions map[string]string) bool` — check allow/deny

### 5.2 Web Fetch Tool (`internal/tools/web_fetch.go`)
- [x] `WebFetchTool` implementing `Tool` interface
- [x] `Parameters()`: `{ "type": "object", "properties": { "url": { "type": "string" } }, "required": ["url"] }`
- [x] `Execute`: HTTP GET URL, extract text content (strip HTML), return first N chars
- [x] User-Agent header, timeout (10s), max response size (50KB)

### 5.3 Memory Tools (`internal/tools/memory.go`)
- [x] `MemorySave` tool:
  - Parameters: `{ "key": "string", "value": "string", "category": "string" }`
  - Execute: call `db.SaveMemory(agentName, key, value, category)`
- [x] `MemoryRecall` tool:
  - Parameters: `{ "key": "string" }` (empty = list all)
  - Execute: call `db.GetMemory` or `db.ListMemory`
- [x] Both scoped to current agent_name from context

### 5.4 Delegate Tool (`internal/tools/delegate.go`)
- [x] `DelegateTool` implementing `Tool` interface
- [x] Parameters: `{ "agent_name": "string", "task": "string" }`
- [x] Execute: load sub-agent from registry, build new context, call engine recursively
- [x] Log delegation to `delegation_logs` table
- [x] Timeout protection (30s max for sub-agent)
- [x] Cycle detection (visited set of agent names in delegation chain)

---

## Phase 6: Agent Engine (Core Loop)
**Dependencies:** Phase 2, Phase 3, Phase 4, Phase 5
**Goal:** Core agent loop with async processing, event publishing, and permission checking

### 6.1 Engine (`internal/agent/engine.go`)
- [x] `Engine` struct with dependencies: `llm.Client`, `tool.Registry`, `HistoryRepository`, `agent.Registry`, `*sql.DB`, `db.Querier`, `*config.Config`, `EventBus`
- [x] `Process(ctx, sessionID, message, agentName) error` (async — runs in goroutine):
  1. Set session status → "working", publish `session.status` event
  2. Ensure session exists (create if not)
  3. Load agent from registry
  4. Save user message to history (parent = active_leaf)
  5. Build context: system prompt + branch history + new user message
  6. Convert to LLM messages array
  7. Loop (max N iterations from config):
     a. Call LLM with tool definitions
     b. If `stop_reason == "stop"`:
        - Publish `message.part.added`, `message.part.updated` events (streaming)
        - Save assistant message to history
        - Publish `message.complete` event
        - Update active_leaf_id
        - Break
     c. If `tool_calls`:
        - For each tool call:
          - Publish `message.tool.started` event
          - Check permission: `IsAllowed(toolName, agent.Permissions)`
          - If denied: save "tool denied" result, publish `message.tool.error`
          - If allowed: execute tool, publish `message.tool.completed`
          - Save tool call message + tool result message to history
          - Append to context
        - Continue loop
  8. On error: publish `message.error` event
  9. defer: set session status → "idle", publish `session.status` event

### 6.2 Message Persistence
- [x] On user message: `InsertMessage` with parent = current active_leaf
- [x] On assistant message: `InsertMessage` with parent = user message
- [x] On tool calls: `InsertMessage` (role=assistant, tool_calls) → parent = user message
- [x] On tool results: `InsertMessage` (role=tool, tool_call_id) → parent = assistant tool call
- [x] Update `active_leaf_id` on session after full assistant response

### 6.3 Token Counting & Usage Tracking
- [x] Track token_count per message (from LLM response `usage` field)
- [x] Aggregate in completion event

### 6.4 Abort Support
- [x] `Abort(sessionID string) error` — cancel context for running engine goroutine
- [x] Goroutine checks `ctx.Err()` at loop boundaries
- [x] Publish `message.error` with "aborted" status

---

## Phase 7: HTTP API Server + SSE Events
**Dependencies:** Phase 1, Phase 6
**Goal:** REST API with async message handling, SSE event streaming, and OpenAI-compatible endpoints

### 7.1 Event Bus (`internal/api/events.go`)
- [x] `EventBus` struct with `sync.RWMutex`, `global []chan SSEEvent`, `sessions map[string][]chan SSEEvent`
- [x] `SSEEvent` struct: `Type string`, `Properties interface{}`
- [x] `Subscribe(sessionID string) <-chan SSEEvent` — register listener
- [x] `Unsubscribe(sessionID string, ch <-chan SSEEvent)` — remove listener
- [x] `PublishGlobal(eventType, properties)` — send to all global listeners (non-blocking, implements `agent.EventBus`)
- [x] `PublishSession(sessionID, eventType, properties)` — send to session listeners (non-blocking)
- [x] Slow listener protection: drop events if channel buffer full
- [x] Heartbeat goroutine: publish `heartbeat` every 15s
- [x] `Close()` — clean shutdown via done channel (TOCTOU-safe)

### 7.2 SSE Writer (`internal/api/sse.go`)
- [ ] `WriteSSE(w http.ResponseWriter, event SSEEvent)` — format as SSE text
- [ ] Flush support via `http.Flusher`
- [ ] `HandleGlobalSSE(w, r)` — handler for `GET /event`
- [ ] `HandleSessionSSE(w, r)` — handler for `GET /session/:id/events`
- [ ] SSE writer file created (stub)

### 7.3 Routes (`internal/api/routes.go`)
- [x] `RegisterRoutes(mux, handler)` — wire `/health` and `/v1/chat/completions`
- [x] Added sync session CRUD + linear chat routes (Go 1.22+ method routing)
- [ ] Refactor to async pattern: replace sync `POST /session/{id}/message` with 202 + SSE
- [ ] Add remaining routes (agents, memory, SSE, control, OpenAI-compat)

### 7.4 Handlers (`internal/api/handler.go`)

> **⚠️ Current state (MVP):** Session CRUD and chat handlers are built synchronously. They work but use **linear** history (flat `ListMessagesBySession`, no tree traversal) and call the LLM **directly** (no agent engine).
> **Planned state:** Handlers should be thin wrappers that enqueue work to the agent engine and return 202 + stream results via SSE.

**Done (sync MVP — may need refactoring for async):**
- [x] `GET /health` → `{"status": "ok"}`
- [x] `POST /session` → create session `{title?, agent_name?}` → 201
- [x] `GET /session` → list all sessions
- [x] `GET /session/{id}` → get session detail + linear messages (not tree)
- [x] `DELETE /session/{id}` → delete session + messages → 200
- [x] `POST /session/{id}/message` → **sync**: save msg → call LLM directly → return response (no SSE, no agent engine)
- [x] `POST /v1/chat/completions` → forwards to LLM, returns OpenAI format (basic pass-through)

**Still planned (async + event-driven):**
- [ ] `GET /event` → Global SSE stream (subscribe + hold connection)
- [ ] `PATCH /session/:id` → update session `{title?}`
- [ ] `GET /session/status` → status of all sessions
- [ ] **Refactor** `POST /session/:id/message` → save user msg, spawn engine goroutine → **202**, stream via SSE
- [ ] `GET /session/:id/messages` → get **active branch** messages (recursive CTE)
- [ ] `GET /session/:id/events` → per-session SSE stream
- [ ] `GET /session/:id/branches` → list all leaf nodes
- [ ] `POST /session/:id/fork` → fork session at message `{message_id}`
- [ ] `PUT /session/:id/branch` → select active branch `{leaf_id}`
- [ ] `PATCH /messages/:id` → edit message → new branch `{content}`
- [ ] `GET /agents` → list registered agents
- [ ] `GET /agents/:name` → get agent detail
- [ ] `GET /memory?agent=NAME` → list memory facts
- [ ] `GET /memory/:key?agent=NAME` → get specific fact
- [ ] `POST /session/:id/abort` → abort running generation
- [ ] `GET /v1/models` → list agents as models

### 7.5 Middleware (`internal/api/middleware.go`)
- [ ] `LoggingMiddleware` — log method, path, status, duration (structured)
- [ ] `CORSHeaders` — Allow-Origin: *, Allow-Methods, Allow-Headers
- [ ] `RecoveryMiddleware` — panic recovery → 500 with error message
- [ ] `AuthMiddleware` skeleton (future: API key / JWT)

### 7.6 Server Entrypoint (`cmd/api/main.go`)
- [x] Load config from `gopengai.json` (hardcoded path + os.Args fallback)
- [x] Open database + run migrations
- [x] Initialize agent registry from `agents/` directory
- [x] Initialize tool registry + register all tools
- [x] Create EventBus, Agent Engine
- [x] Create API handler (with DB + Config + Engine + EventBus wired) + register routes
- [x] Start HTTP server on configured host:port
- [x] Graceful shutdown (SIGINT/SIGTERM) — drain SSE connections via EventBus.Close, wait for server, close DB

---

## Phase 8: CLI Client
**Dependencies:** Phase 7 (sync MVP built first; async/SSE integration still planned)
**Goal:** Cobra-based CLI client with chat, session, agent, and memory commands

### 8.1 CLI Entrypoint (`cmd/cli/main.go`)
- [x] Cobra root command: `gopengai` with `--server-url` flag (default `http://localhost:8080`)

### 8.2 Chat Command
- [x] `gopengai chat "message" [--session-id ID] [--agent NAME]` — **sync**: send message, wait for JSON response
- [x] Interactive mode: `gopengai chat` — REPL loop (sync polling)
- [ ] Upgrade to SSE streaming: subscribe to session SSE, send message, display streamed tokens
- [ ] Display model name + usage in output

### 8.3 Session Commands
- [x] `gopengai session list` → list all sessions
- [x] `gopengai session show <id>` → show session + linear messages
- [x] `gopengai session create [--title T] [--agent NAME]` → create session
- [x] `gopengai session delete <id>` → delete session
- [ ] `gopengai session branches <id>` → list all leaves (requires Phase 4 tree implementation)
- [ ] `gopengai session fork <id> --message <msg_id>` → fork at message (requires Phase 4)
- [ ] `gopengai session switch <id> --leaf <leaf_id>` → select branch (requires Phase 4)

### 8.4 Agent Commands
- [ ] `gopengai agents` → list available agents
- [ ] `gopengai agents info <name>` → show agent detail

### 8.5 Memory Commands
- [ ] `gopengai memory list [--agent NAME]` → show memory facts
- [ ] `gopengai memory get <key> [--agent NAME]` → get specific fact

---

## Phase 9: Testing & Quality
**Dependencies:** Phase 1-8 (parallel with development)
**Goal:** Unit tests, integration tests, API tests

### 9.1 Unit Tests
- [ ] `internal/db/` — test migrations, CRUD operations (in-memory SQLite)
- [ ] `internal/history/tree.go` — test tree construction, branch selection, edit→new-branch
- [ ] `internal/agent/loader.go` — test YAML frontmatter parsing with permissions
- [ ] `internal/tools/` — test each tool's Execute with mock dependencies
- [ ] `internal/agent/engine.go` — test loop logic with mock LLM client
- [ ] `internal/api/events.go` — test event bus subscribe/publish/unsubscribe
- [ ] `internal/config/` — test config loading, defaults, env overrides

### 9.2 Integration Tests
- [ ] Full chat flow: POST /session/:id/message → SSE events → message.complete
- [ ] Test OpenAI-compatible endpoint format
- [ ] Test branch creation via message edit
- [ ] Test session fork
- [ ] Test tool permission deny
- [ ] Test abort mid-generation

### 9.3 Test Infrastructure
- [ ] Mock HTTP server for LLM responses (net/http/httptest)
- [ ] Temporary SQLite databases per test
- [ ] SSE test helpers (subscribe, collect events, assert)

---

## Phase 10: Documentation & Polish
**Dependencies:** Phase 1-9
**Goal:** Updated README, example agents, gopengai.json.example, Makefile

### 10.1 Documentation
- [ ] Update `README.md` with actual API examples, CLI usage, SSE examples
- [ ] Create `agents/examples/` with pre-built agents (researcher, analyst, summarizer)
- [x] Create `gopengai.json.example` with all configurable fields documented
- [ ] Update all diagrams in `DOCS/diagrams/` if needed

### 10.2 Code Quality
- [x] `go vet ./...` — clean
- [ ] `go fmt ./...` — formatted
- [x] Add `Makefile` with common commands:
  ```makefile
  build:    go build ./cmd/api/ ./cmd/cli/
  run:      go run ./cmd/api/
  test:     go test ./... -v
  lint:     go vet ./...
  fmt:      go fmt ./...
  clean:    rm -f gopengai gopengai.db
  ```

---

## Architecture Risk Summary

| Risk | Impact | Mitigation |
|------|--------|------------|
| SQLite concurrency (multiple requests) | Medium | Use WAL mode, `SetMaxOpenConns(1)`, transactions for writes |
| sqlc code generation drift | Low | Regenerate on migration change, commit generated files |
| Goose migration ordering | Low | Sequential timestamps in filenames, test rollback path |
| SSE memory leak (forgotten listeners) | Medium | Unsubscribe on disconnect, periodic cleanup of dead channels |
| LLM tool calling format differences across providers | High | Abstract LLM client, test with target provider early |
| Infinite agent loop (LLM keeps calling tools) | Medium | Max iterations from config, timeout per iteration |
| Recursive delegation (agent delegates to itself) | Medium | Detect cycles in delegation chain (visited set) |
| Context window overflow from long branches | Medium | Truncate history, keep only active branch, summarize old messages |
| Async goroutine leak on server shutdown | Medium | Context cancellation + WaitGroup for graceful shutdown |
| ncruces/go-sqlite3 Wasm performance | Low | Benchmark early; fallback to mattn/go-sqlite3 if needed |

## Dependency Graph (Build Order)

```
Phase 0 (Bootstrap + Rename + go mod init)
  └── Phase 1 (Config + SQLite + Goose migrations + sqlc setup)
        ├── Phase 2 (LLM Client)
        │     └── Phase 6 (Agent Engine + Event Bus)
        │           └── Phase 7 (HTTP API + SSE)
        │                 └── Phase 8 (CLI)
        └── Phase 3 (Agent Types + Loader)
              └── Phase 5 (Tools + Permissions)
        └── Phase 4 (History Tree — uses sqlc-generated queries)
  Phase 9 (Testing) — runs in parallel
  Phase 10 (Docs) — last
```

## Key DB Tooling (borrowed from OpenCode)

| Tool | Purpose | Why |
|------|---------|-----|
| `ncruces/go-sqlite3` | SQLite driver | Pure Go (Wasm), no CGo dependency |
| `pressly/goose/v3` | Schema migrations | Embedded in binary, auto-applied on startup |
| `sqlc v1.29+` | Query code generation | Type-safe Go from raw SQL, no ORM overhead |

## Priority Stack (if time-constrained)

1. **Must Have:** Phases 0-7 (working API, agent loop, LLM calls, SQLite, SSE, basic tools)
2. **Should Have:** Phase 8 (CLI client), Phase 9 (tests)
3. **Nice to Have:** Advanced delegation, streaming LLM output, memory recall search
