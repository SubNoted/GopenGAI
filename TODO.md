# GoPengAI — Implementation TODO

> **Last synced:** 2026-05-23 (Phase 0-Phase 1 files exist but untracked; no code committed to git yet)
> **Based on:** 10 architecture diagrams (01-container through 10-gopengai-container)
> **Tech Stack:** Go 1.21+, SQLite3 (ncruces/go-sqlite3), sqlc, Goose, Cobra CLI, net/http, SSE
> **Approach:** Pure Go — no CGo, no Python. All phases for semester 4 delivery. Local dev deployment.
> **API Design:** Adapted OpenCode hybrid — async message POST (202) + SSE streaming + tree-based history
> **DB Design:** Adapted OpenCode SQLite model — 3 base tables extended for agents, memory, delegation
> **Order:** Sequential phases. Each phase builds on the previous.

---

## Phase 0: Project Bootstrap
**Dependencies:** None
**Goal:** Rename to gopengai, initialize Go module, create directory structure, verify build

- [ ] `go mod init gopengai`
- [ ] Create directory structure per diagram `06-package-structure`:
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
- [ ] Add dependencies: `github.com/ncruces/go-sqlite3`, `github.com/pressly/goose/v3`, `github.com/spf13/cobra`, `gopkg.in/yaml.v3`
- [ ] Create `gopengai.json.example` with server, LLM, agents_dir, data_dir, default_agent fields
- [ ] Create `.gitignore`:
  ```
  # Binary
  /gopengai
  /api
  /cli

  # Data directory (per-project SQLite)
  .gopengai/

  # Go
  vendor/

  # IDE
  .idea/
  .vscode/
  ```
- [ ] Verify `go build ./cmd/api/` and `go build ./cmd/cli/` succeed (empty main files)
- [ ] Verify `go vet ./...` passes

---

## Phase 1: Configuration & Database Layer
**Dependencies:** Phase 0
**Goal:** Config loading from gopengai.json, SQLite connection, Goose migrations, sqlc-generated CRUD

### 1.1 Configuration (`internal/config/config.go`)
- [ ] Define `Config` struct matching `gopengai.json` schema:
  ```go
  type Config struct {
      Server   ServerConfig   `json:"server"`
      LLM      LLMConfig      `json:"llm"`
      AgentsDir string        `json:"agents_dir"`
      DataDir   string        `json:"data_dir"`     // default: ".gopengai"
      DBPath    string        `json:"db_path"`       // default: "<data_dir>/gopengai.db"
      DefaultAgent string     `json:"default_agent"`
  }
  type ServerConfig struct {
      Host string `json:"host"`
      Port int    `json:"port"`
  }
  type LLMConfig struct {
      Provider      string `json:"provider"`
      BaseURL       string `json:"base_url"`
      APIKey        string `json:"api_key"`
      Model         string `json:"model"`
      MaxIterations int    `json:"max_iterations"`
  }
  ```
- [ ] `Load(path string) (*Config, error)` — read JSON file, apply defaults
- [ ] Env var overrides: `GOPENGAI_PORT`, `GOPENGAI_LLM_API_KEY`, etc.
- [ ] CLI flag overrides: `--port`, `--config`

### 1.2 Database Connection (`internal/db/connect.go`)
- [ ] `Open(path string) (*sql.DB, error)` — open SQLite via `ncruces/go-sqlite3` (pure Go, no CGo)
- [ ] Set pragmas on connection: `foreign_keys=ON`, `journal_mode=WAL`, `page_size=4096`, `cache_size=-8000`, `synchronous=NORMAL`
- [ ] Connection pooling: `SetMaxOpenConns(1)` (SQLite single-writer), `SetMaxIdleConns(1)`

### 1.3 Migrations (`internal/db/migrations/`)
- [ ] Use **Goose** (`github.com/pressly/goose/v3`) for migration management
- [ ] Embed migrations in binary via `go:embed`
- [ ] `Migrate(db *sql.DB) error` — runs `goose.Up()` on startup
- [ ] **Migration 1: `initial.sql`** — Create tables (adapted from OpenCode + our extensions):
  ```sql
  -- Sessions (OpenCode-compatible base)
  CREATE TABLE IF NOT EXISTS sessions (
      id TEXT PRIMARY KEY,
      parent_session_id TEXT,          -- nullable, for session chaining (auto-compact)
      agent_name TEXT NOT NULL DEFAULT 'default',
      title TEXT NOT NULL,
      active_leaf_id TEXT,             -- current active branch leaf (tree extension)
      status TEXT NOT NULL DEFAULT 'idle',  -- idle | working | aborted
      message_count INTEGER NOT NULL DEFAULT 0 CHECK (message_count >= 0),
      prompt_tokens INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
      completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
      cost REAL NOT NULL DEFAULT 0.0 CHECK (cost >= 0.0),
      updated_at INTEGER NOT NULL,     -- Unix ms
      created_at INTEGER NOT NULL,     -- Unix ms
      summary_message_id TEXT          -- for auto-compact summary link
  );

  -- Messages (OpenCode-compatible base + tree/tool extensions)
  CREATE TABLE IF NOT EXISTS messages (
      id TEXT PRIMARY KEY,
      session_id TEXT NOT NULL,
      parent_id TEXT,                  -- tree structure: NULL for root, else → messages.id
      role TEXT NOT NULL,              -- user | assistant | tool | system
      parts TEXT NOT NULL DEFAULT '[]', -- JSON array of message parts (text, tool_use, images)
      content TEXT,                    -- plain text content (for tree queries)
      agent_name TEXT,                 -- which agent produced this
      tool_name TEXT,                  -- if role=tool: which tool
      tool_call_id TEXT,               -- links tool response to call
      tool_args TEXT,                  -- JSON: tool arguments
      model TEXT,                      -- nullable, model used
      token_count INTEGER DEFAULT 0,
      created_at INTEGER NOT NULL,
      updated_at INTEGER NOT NULL,
      finished_at INTEGER,             -- nullable, when assistant finished
      FOREIGN KEY (session_id) REFERENCES sessions (id) ON DELETE CASCADE
  );

  -- Agents (loaded from .md files)
  CREATE TABLE IF NOT EXISTS agents (
      name TEXT PRIMARY KEY,           -- filename without .md
      system_prompt TEXT NOT NULL,
      tools TEXT NOT NULL DEFAULT '[]', -- JSON array of allowed tool names
      model TEXT,                       -- model override (nullable)
      parent_agent TEXT,               -- parent agent name (nullable)
      permissions TEXT NOT NULL DEFAULT '{}', -- JSON: tool_name → "allow"/"deny"
      config_path TEXT,                -- absolute path to .md file
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

  -- Delegation Logs (agent → sub-agent tracking)
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
  ```
- [ ] **Triggers** (auto-update timestamps, message counts):
  - `update_sessions_updated_at` — on session update
  - `update_messages_updated_at` — on message update
  - `update_memory_updated_at` — on memory update
  - `update_session_message_count_on_insert` — increment on message insert
  - `update_session_message_count_on_delete` — decrement on message delete
- [ ] **Indexes**:
  - `idx_messages_session_id` on messages (session_id)
  - `idx_messages_parent_id` on messages (parent_id)
  - `idx_memory_agent_name` on memory (agent_name)
  - `idx_delegation_logs_parent` on delegation_logs (parent_message_id)

### 1.4 sqlc Setup (`sqlc.yaml`)
- [ ] Configure sqlc v1.29+ for SQLite engine:
  ```yaml
  version: "2"
  sql:
    - engine: "sqlite"
      schema: "internal/db/migrations"
      queries: "internal/db/sql"
      gen:
        go:
          package: "db"
          out: "internal/db"
          emit_json_tags: true
          emit_prepared_queries: true
          emit_interface: true
  ```
- [ ] Write raw SQL queries in `internal/db/sql/*.sql`:
  - `sessions.sql` — CreateSession, GetSessionByID, ListSessions, UpdateSession, DeleteSession
  - `messages.sql` — CreateMessage, GetMessage, ListMessagesBySession, GetBranchFromRootTo (recursive CTE), GetAllLeaves, UpdateMessage, DeleteMessage, DeleteSessionMessages
  - `agents.sql` — CreateAgent, GetAgent, ListAgents, DeleteAgent
  - `memory.sql` — CreateMemory, GetMemory, ListMemoryByAgent, DeleteMemory
  - `delegation_logs.sql` — CreateDelegationLog, ListDelegationLogsBySession
- [ ] Run `sqlc generate` to produce Go code

### 1.5 Generated Querier Interface (`internal/db/querier.go`)
- [ ] Expected interface (sqlc-generated):
  ```go
  type Querier interface {
      // Sessions
      CreateSession(ctx, arg CreateSessionParams) (Session, error)
      GetSessionByID(ctx, id string) (Session, error)
      ListSessions(ctx) ([]Session, error)
      UpdateSession(ctx, arg UpdateSessionParams) (Session, error)
      DeleteSession(ctx, id string) error

      // Messages
      CreateMessage(ctx, arg CreateMessageParams) (Message, error)
      GetMessage(ctx, id string) (Message, error)
      ListMessagesBySession(ctx, sessionID string) ([]Message, error)
      GetBranchFromRootTo(ctx, messageID string) ([]Message, error)
      GetAllLeaves(ctx, sessionID string) ([]Message, error)
      UpdateMessage(ctx, arg UpdateMessageParams) error
      DeleteMessage(ctx, id string) error
      DeleteSessionMessages(ctx, sessionID string) error

      // Agents
      CreateAgent(ctx, arg CreateAgentParams) (Agent, error)
      GetAgent(ctx, name string) (Agent, error)
      ListAgents(ctx) ([]Agent, error)
      DeleteAgent(ctx, name string) error

      // Memory
      CreateMemory(ctx, arg CreateMemoryParams) (Memory, error)
      GetMemory(ctx, arg GetMemoryParams) (Memory, error)
      ListMemoryByAgent(ctx, agentName string) ([]Memory, error)
      DeleteMemory(ctx, id string) error

      // Delegation
      CreateDelegationLog(ctx, arg CreateDelegationLogParams) (DelegationLog, error)
      ListDelegationLogsBySession(ctx, sessionID string) ([]DelegationLog, error)
  }
  ```

### 1.6 Data Directory Setup
- [ ] Default data dir: `.gopengai/` (per-project, gitignored)
- [ ] Structure: `.gopengai/gopengai.db` — single SQLite file
- [ ] Add `.gopengai/` to `.gitignore`

---

## Phase 2: LLM Client Layer
**Dependencies:** Phase 1
**Goal:** OpenAI-compatible HTTP client for LLM calls with tool support

### 2.1 LLM Types (`internal/llm/types.go`)
- [ ] Define structs matching OpenAI `/v1/chat/completions` format:
  - `CompletionRequest`, `Message`, `ToolDefinition`, `ToolFunction`
  - `CompletionResponse`, `Choice`, `MessageResponse`, `ToolCall`, `Usage`
- [ ] All fields with correct JSON tags

### 2.2 LLM Client (`internal/llm/client.go`)
- [ ] `Client` struct with config (`BaseURL`, `APIKey`, `Model`, `MaxIterations`)
- [ ] `NewClient(cfg config.LLMConfig) *Client`
- [ ] `ChatCompletion(ctx, *CompletionRequest) (*CompletionResponse, error)` — HTTP POST
- [ ] Support `tool_choice: "auto"` for tool calling
- [ ] Error handling for non-200 responses with structured error type
- [ ] Context cancellation support (for abort)

### 2.3 Streaming Skeleton (`internal/llm/stream.go`)
- [ ] SSE parsing infrastructure
- [ ] `StreamCompletion(ctx, *CompletionRequest) (<-chan *CompletionResponse, error)`
- [ ] Mark as future feature — focus on non-streaming first

---

## Phase 3: Agent Types & Loader
**Dependencies:** Phase 2
**Goal:** Define agent data types, parse `.md` config files with YAML frontmatter, build registry

### 3.1 Agent Types (`internal/agent/types.go`)
- [ ] `Agent` struct: `Name`, `SystemPrompt`, `Tools []string`, `Model`, `ParentAgent`, `Permissions map[string]string`, `ConfigPath`
- [ ] `Message` struct (in-memory): `Role`, `Content`, `ToolCalls`, `ToolCallID`
- [ ] `ToolCall` struct: `ID`, `Name`, `Arguments`
- [ ] `Response` struct: `Content`, `Sources`, `Usage`, `StopReason`

### 3.2 Agent Loader (`internal/agent/loader.go`)
- [ ] `LoadAgent(path string) (*Agent, error)` — read `.md` file, parse YAML frontmatter
- [ ] YAML frontmatter fields: `name`, `model`, `tools`, `parent_agent`, `permissions`
- [ ] Body of `.md` file = system prompt (if not in frontmatter `system_prompt` field)
- [ ] Parse `permissions` as `map[string]string` (`tool_name → "allow"/"deny"`)
- [ ] `LoadDirectory(dir string) (map[string]*Agent, error)` — scan all `.md` files

### 3.3 Agent Registry (`internal/agent/registry.go`)
- [ ] In-memory `Registry` with `map[string]*Agent`
- [ ] `Register(agent *Agent)`
- [ ] `Get(name string) (*Agent, error)`
- [ ] `List() []Agent`
- [ ] `InitializeFromDir(dir string) error` — convenience wrapper around Loader

### 3.4 Default Agent Config
- [ ] Create `agents/default.md`:
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
- [ ] Thin wrapper around sqlc-generated `db.Querier` for history-specific operations
- [ ] `InsertMessage(ctx, db, msg) error` — delegates to `db.CreateMessage`
- [ ] `GetMessagesForSession(ctx, db, sessionID) ([]Message, error)` — delegates to `db.ListMessagesBySession`
- [ ] `GetBranchFromRootTo(ctx, db, messageID) ([]Message, error)` — delegates to `db.GetBranchFromRootTo` (recursive CTE)
- [ ] `GetAllLeaves(ctx, db, sessionID) ([]Message, error)` — delegates to `db.GetAllLeaves`

### 4.2 Tree Operations (`internal/history/tree.go`)
- [ ] `BuildTree(messages) *TreeNode` — construct in-memory tree from flat list
- [ ] `FindActiveLeaf(tree) *TreeNode` — longest root→leaf path
- [ ] `GetPathFromRoot(tree, node) []Message` — traverse root to node
- [ ] `InsertNode(tree, parentID, newMessage) *TreeNode` — add child

### 4.3 Branch Management (`internal/history/branch.go`)
- [ ] `SelectBranch(db, sessionID, leafMessageID) error` — set active_leaf_id
- [ ] `EditMessage(db, originalMsgID, newContent) (newMsgID, error)` — new branch from parent of original
- [ ] `ForkSession(db, sessionID, messageID) (newSessionID, error)` — create new session branching from point

### 4.4 Session Context Builder
- [ ] `BuildContext(db, sessionID) ([]llm.Message, error)` — load branch, convert to LLM messages
- [ ] Include system prompt from agent
- [ ] Truncate if branch history exceeds context window limit

---

## Phase 5: Tool Registry & Implementations
**Dependencies:** Phase 2, Phase 3, Phase 4
**Goal:** Tool interface, registry, 3 tool implementations, permission checking

### 5.1 Tool Interface (`internal/tools/registry.go`)
- [ ] Define `Tool` interface:
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
- [ ] `IsAllowed(toolName string, permissions map[string]string) bool` — check allow/deny

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
- [ ] Cycle detection (visited set of agent names in delegation chain)

---

## Phase 6: Agent Engine (Core Loop)
**Dependencies:** Phase 2, Phase 3, Phase 4, Phase 5
**Goal:** Core agent loop with async processing, event publishing, and permission checking

### 6.1 Engine (`internal/agent/engine.go`)
- [ ] `Engine` struct with dependencies: `llm.Client`, `ToolRegistry`, `history.Manager`, `agent.Registry`, `*sql.DB`, `*EventBus`
- [ ] `Process(ctx, sessionID, message, agentName) error` (async — runs in goroutine):
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
- [ ] On user message: `InsertMessage` with parent = current active_leaf
- [ ] On assistant message: `InsertMessage` with parent = user message
- [ ] On tool calls: `InsertMessage` (role=assistant, tool_calls) → parent = user message
- [ ] On tool results: `InsertMessage` (role=tool, tool_call_id) → parent = assistant tool call
- [ ] Update `active_leaf_id` on session after full assistant response

### 6.3 Token Counting & Usage Tracking
- [ ] Track token_count per message (from LLM response `usage` field)
- [ ] Aggregate in completion event

### 6.4 Abort Support
- [ ] `Abort(sessionID string) error` — cancel context for running engine goroutine
- [ ] Goroutine checks `ctx.Err()` at loop boundaries
- [ ] Publish `message.error` with "aborted" status

---

## Phase 7: HTTP API Server + SSE Events
**Dependencies:** Phase 1, Phase 6
**Goal:** REST API with async message handling, SSE event streaming, and OpenAI-compatible endpoints

### 7.1 Event Bus (`internal/api/events.go`)
- [ ] `EventBus` struct with `sync.RWMutex`, `global []chan SSEEvent`, `sessions map[string][]chan SSEEvent`
- [ ] `SSEEvent` struct: `Type string`, `Properties interface{}`
- [ ] `Subscribe(sessionID string) <-chan SSEEvent` — register listener
- [ ] `Unsubscribe(sessionID string, ch <-chan SSEEvent)` — remove listener
- [ ] `PublishGlobal(event SSEEvent)` — send to all global listeners (non-blocking)
- [ ] `PublishSession(sessionID string, event SSEEvent)` — send to session listeners (non-blocking)
- [ ] Slow listener protection: drop events if channel buffer full
- [ ] Heartbeat goroutine: publish `heartbeat` every 15s

### 7.2 SSE Writer (`internal/api/sse.go`)
- [ ] `WriteSSE(w http.ResponseWriter, event SSEEvent)` — format as SSE text
- [ ] Flush support via `http.Flusher`
- [ ] `HandleGlobalSSE(w, r)` — handler for `GET /event`
- [ ] `HandleSessionSSE(w, r)` — handler for `GET /session/:id/events`

### 7.3 Routes (`internal/api/routes.go`)
- [ ] `RegisterRoutes(mux, handler)` — wire all routes

### 7.4 Handlers (`internal/api/handler.go`)

**Global:**
- [ ] `GET /health` → `{"status": "ok", "version": "...", "uptime": "..."}`
- [ ] `GET /event` → Global SSE stream (subscribe + hold connection)

**Session CRUD:**
- [ ] `GET /session` → list all sessions
- [ ] `POST /session` → create session `{title?, agent_name?}` → 201
- [ ] `GET /session/:id` → get session detail + active branch
- [ ] `PATCH /session/:id` → update session `{title?}`
- [ ] `DELETE /session/:id` → delete session + messages → 200

**Session Status:**
- [ ] `GET /session/status` → status of all sessions

**Messages (Async):**
- [ ] `POST /session/:id/message` → save user msg, spawn engine goroutine → 202
- [ ] `GET /session/:id/messages` → get active branch messages
- [ ] `GET /session/:id/events` → per-session SSE stream

**Branches:**
- [ ] `GET /session/:id/branches` → list all leaf nodes
- [ ] `POST /session/:id/fork` → fork session at message `{message_id}`
- [ ] `PUT /session/:id/branch` → select active branch `{leaf_id}`
- [ ] `PATCH /messages/:id` → edit message → new branch `{content}`

**Agents:**
- [ ] `GET /agents` → list registered agents
- [ ] `GET /agents/:name` → get agent detail

**Memory:**
- [ ] `GET /memory?agent=NAME` → list memory facts
- [ ] `GET /memory/:key?agent=NAME` → get specific fact

**Control:**
- [ ] `POST /session/:id/abort` → abort running generation

**OpenAI-Compatible:**
- [ ] `POST /v1/chat/completions` → map to internal engine, return OpenAI format
- [ ] `GET /v1/models` → list agents as models

### 7.5 Middleware (`internal/api/middleware.go`)
- [ ] `LoggingMiddleware` — log method, path, status, duration (structured)
- [ ] `CORSHeaders` — Allow-Origin: *, Allow-Methods, Allow-Headers
- [ ] `RecoveryMiddleware` — panic recovery → 500 with error message
- [ ] `AuthMiddleware` skeleton (future: API key / JWT)

### 7.6 Server Entrypoint (`cmd/api/main.go`)
- [ ] Load config from `gopengai.json` (or `--config` flag)
- [ ] Open database + run migrations
- [ ] Initialize agent registry from `agents/` directory
- [ ] Initialize tool registry + register all tools
- [ ] Create EventBus, LLM client, Engine
- [ ] Create API handler + register routes
- [ ] Start HTTP server on configured host:port
- [ ] Graceful shutdown (SIGINT/SIGTERM) — drain SSE connections, wait for engine goroutines

---

## Phase 8: CLI Client
**Dependencies:** Phase 7
**Goal:** Cobra-based CLI client with chat, session, agent, and memory commands

### 8.1 CLI Entrypoint (`cmd/cli/main.go`)
- [ ] Cobra root command: `gopengai` with `--server-url` flag (default `http://localhost:8080`)
- [ ] Global `--json` flag for machine-readable output

### 8.2 Chat Command
- [ ] `gopengai chat "message" [--session-id ID] [--agent NAME]`
- [ ] Subscribe to session SSE, send message, display streaming response
- [ ] Auto-create session if no `--session-id`
- [ ] Interactive mode: `gopengai chat` — REPL loop

### 8.3 Session Commands
- [ ] `gopengai sessions` → list all sessions
- [ ] `gopengai sessions get <id>` → show session + active branch
- [ ] `gopengai sessions create [--title T] [--agent NAME]` → create session
- [ ] `gopengai sessions delete <id>` → delete session
- [ ] `gopengai sessions branches <id>` → list all leaves
- [ ] `gopengai sessions fork <id> --message <msg_id>` → fork at message
- [ ] `gopengai sessions switch <id> --leaf <leaf_id>` → select branch

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
- [ ] Create `gopengai.json.example` with all configurable fields documented
- [ ] Update all diagrams in `DOCS/diagrams/` if needed

### 10.2 Code Quality
- [ ] `go vet ./...` — clean
- [ ] `go fmt ./...` — formatted
- [ ] Add `Makefile` with common commands:
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
