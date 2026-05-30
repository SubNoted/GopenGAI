# GoPengAI — Implementation TODO

> **Last synced:** 2026-05-30 (Phases 1, 3, 8 finished: CLI flags, example agents, all subcommands)
> **Go version:** 1.26.1 (from `go.mod`)
> **Tech Stack:** Go 1.26+, SQLite3 (ncruces/go-sqlite3), sqlc, Goose, Cobra CLI, net/http, SSE
> **Approach:** Pure Go — no CGo, no Python. All phases for semester 4 delivery. Local dev deployment.
> **API Design:** Adapted OpenCode hybrid — async message POST (202) + SSE streaming + tree-based history
> **DB Design:** Adapted OpenCode SQLite model — 5 tables extended for agents, memory, delegation

## Overall Progress: ~91%

| Phase | Claim | Actual | Gap |
|-------|-------|--------|-----|
| Phase 0 (Bootstrap) | 100% | 100% | — |
| Phase 1 (Config+DB) | 100% | **100%** | — |
| Phase 2 (LLM Client) | 100% | 100% | — |
| Phase 3 (Agent Types) | 100% | **100%** | — |
| Phase 4 (History Tree) | 100% | 100% | — |
| Phase 5 (Tools) | 100% | 100% | — |
| Phase 6 (Agent Engine) | 100% | 100% | — |
| Phase 7 (HTTP API) | 100% | 100% | — |
| Phase 8 (CLI) | 100% | **100%** | — |
| Phase 9 (Testing) | 0% | **0%** | Zero test files exist |
| Phase 10 (Docs) | 50% | **50%** | README outdated, diagrams need review, `go fmt` not run |

## Progress Bars

```
Phase 0 (Bootstrap)    ██████████ 100%  ✓
Phase 1 (Config+DB)    ██████████ 100%  ✓
Phase 2 (LLM Client)   ██████████ 100%  ✓
Phase 3 (Agent Types)  ██████████ 100%  ✓
Phase 4 (History Tree) ██████████ 100%  ✓
Phase 5 (Tools)        ██████████ 100%  ✓
Phase 6 (Agent Engine) ██████████ 100%  ✓
Phase 7 (HTTP API)     ██████████ 100%  ✓
Phase 8 (CLI)          ██████████ 100%  ✓
Phase 9 (Testing)      ░░░░░░░░░░   0%  (NOTHING)
Phase 10 (Docs)        █████░░░░░  50%  (README outdated, diagrams need review)
```

## ✅ Complete Phases (No Action Needed)

### Phase 0 — Project Bootstrap ✅
All done: `go mod init`, directory structure, `.gitignore`, `gopengai.json.example`, build verification.

### Phase 1 — Config & Database ✅
All done: Config loading from JSON with env var overrides, `--config`/`--port` CLI flags, SQLite connection with ncruces/go-sqlite3 (pure Go), Goose migrations embedded and auto-applied, sqlc-generated CRUD for all 5 tables.

### Phase 2 — LLM Client ✅
All done: OpenAI-compatible HTTP client, ChatCompletion, streaming skeleton, tool calling support, structured types.

### Phase 3 — Agent Types ✅
All done: Agent struct with YAML frontmatter parsing, loader, registry, default agent (`agents/default.md`), 3 example agents (`researcher`, `analyst`, `summarizer`).

### Phase 4 — History Tree ✅
All done: Repository wrapper, tree operations (build/extract/find/insert), branch management (select leaf, edit→new-branch, fork session with transactions), session context builder with truncation.

### Phase 5 — Tools ✅
All done: Tool interface, registry with permission checking, WebFetch tool, MemorySave/MemoryRecall tools, Delegate tool with cycle detection and timeout.

### Phase 6 — Agent Engine ✅
All done: Core agent loop with async processing, tool-calling loop, event publishing, message persistence, token counting, abort support with context cancellation.

### Phase 7 — HTTP API ✅
All done: EventBus with subscribe/publish/unsubscribe/close/heartbeat, SSE writer and handlers (global + session), all 22+ routes wired (session CRUD, async message with 202, branches, fork, abort, agents, memory, models, OpenAI compat), all 4 middleware functions (Auth, Logging, CORS, Recovery), graceful shutdown with WaitGroup + context + signal handling, server timeouts.

### Phase 8 — CLI ✅
All done: Full command tree (`chat`, `session`, `agents`, `memory`), sync mode (202 accepted), SSE streaming mode (`--stream` flag), REPL loop, session list/show/create/delete/branches/fork/switch, agents list/info, memory list/get, all HTTP helpers with auth support.

---

## 🔴 Remaining Work

### Phase 9 — Testing

**Dependencies:** All other phases complete

**Goal:** Unit tests + integration tests for all components.

#### 9.1 Unit Tests — Config & DB
**Files:**
- Create: `internal/config/config_test.go`
- Create: `internal/db/connect_test.go`

- [ ] `config_test.go` — test JSON loading, defaults, env var overrides
- [ ] `connect_test.go` — test SQLite open + migrate with in-memory DB

#### 9.2 Unit Tests — History
**Files:**
- Create: `internal/history/tree_test.go`
- Create: `internal/history/branch_test.go`
- Create: `internal/history/context_test.go`

- [ ] `tree_test.go` — test BuildTree, GetLongestLeaf, GetPathFromRoot, FindNode, IsLeaf
- [ ] `branch_test.go` — test SelectLeaf, EditMessage→new-branch, ForkSession with transaction rollback
- [ ] `context_test.go` — test BuildContext, truncation logic

#### 9.3 Unit Tests — Agent
**Files:**
- Create: `internal/agent/loader_test.go`
- Create: `internal/agent/engine_test.go`

- [ ] `loader_test.go` — test YAML frontmatter parsing with permissions and system prompt extraction
- [ ] `engine_test.go` — test Process loop with mock LLM client (httptest), tool calling cycle, abort

#### 9.4 Unit Tests — Tools
**Files:**
- Create: `internal/tools/web_fetch_test.go`
- Create: `internal/tools/memory_test.go`
- Create: `internal/tools/delegate_test.go`

- [ ] `web_fetch_test.go` — HTTP mock server, HTML stripping, max size enforcement
- [ ] `memory_test.go` — save/recall with mock DB querier, agent scoping
- [ ] `delegate_test.go` — cycle detection, timeout, delegation logging

#### 9.5 Unit Tests — LLM & API
**Files:**
- Create: `internal/llm/client_test.go`
- Create: `internal/api/events_test.go`
- Create: `internal/api/handler_test.go`
- Create: `internal/api/middleware_test.go`

- [ ] `client_test.go` — mock server, success/error responses, tool calling format
- [ ] `events_test.go` — EventBus subscribe/publish/unsubscribe/close, slow listener drop
- [ ] `handler_test.go` — httptest-based session CRUD, chat (202 + SSE events), branches, fork, abort, agents, memory
- [ ] `middleware_test.go` — auth (valid/invalid key, empty key skips), CORS headers, logging capture

#### 9.6 Integration Tests
**Files:**
- Create: `tests/integration/chat_flow_test.go`

- [ ] Full chat flow: POST message → SSE events → message.complete
- [ ] Branch creation via message edit
- [ ] Session fork
- [ ] Tool permission deny
- [ ] Abort mid-generation

#### 9.7 Test Infrastructure
- [ ] Mock HTTP server for LLM responses (`net/http/httptest`)
- [ ] Temporary SQLite databases per test suite
- [ ] SSE test helpers (subscribe topic, collect events, assert event types)

### Phase 10 — Documentation & Polish

**Dependencies:** All other phases complete

**Goal:** Updated README, example agents, code formatting.

#### 10.1 Documentation
**Files:**
- Modify: `README.md`

- [ ] Update `README.md` with actual API examples, CLI usage, SSE examples

#### 10.2 Code Quality
- [ ] Run `go fmt ./...` and commit formatting changes
- [ ] Verify diagrams in `DOCS/diagrams/` match current architecture (async + SSE)
- [ ] Update `gopengai.json.example` if any new config fields were added
- [ ] Add `api`, `cli`, `gopengai` binaries to `.gitignore`

---

## Architecture Risk Summary

| Risk | Impact | Mitigation |
|------|--------|------------|
| SQLite concurrency (multiple requests) | Medium | WAL mode, `SetMaxOpenConns(1)`, transactions for writes |
| sqlc code generation drift | Low | Regenerate on migration change, commit generated files |
| SSE memory leak (forgotten listeners) | Medium | Unsubscribe on disconnect, periodic cleanup of dead channels |
| LLM tool calling format differences across providers | High | Abstract LLM client, test with target provider early |
| Infinite agent loop (LLM keeps calling tools) | Medium | Max iterations from config, timeout per iteration |
| Recursive delegation (agent delegates to itself) | Medium | Cycle detection in delegation chain (visited set) |
| Context window overflow from long branches | Medium | Truncate history, keep only active branch |
| Async goroutine leak on server shutdown | Medium | Context cancellation + WaitGroup for graceful shutdown |

## Priority Stack (if time-constrained)

1. **Must Have:** All of Phase 10 (docs + polish) for submission
2. **Should Have:** Phase 9 core unit tests (config, history, agent loader, engine)
3. **Nice to Have:** Full integration tests, all example agents

---

## Key DB Tooling

| Tool | Purpose | Why |
|------|---------|-----|
| `ncruces/go-sqlite3` | SQLite driver | Pure Go (Wasm), no CGo dependency |
| `pressly/goose/v3` | Schema migrations | Embedded in binary, auto-applied on startup |
| `sqlc v1.29+` | Query code generation | Type-safe Go from raw SQL, no ORM overhead |