# GoPengAI — Implementation TODO

> **Last synced:** 2026-05-30 (Phase 9 complete: 16 test files, 4 bug fixes)
> **Go version:** 1.26.1 (from `go.mod`)
> **Tech Stack:** Go 1.26+, SQLite3 (ncruces/go-sqlite3), sqlc, Goose, Cobra CLI, net/http, SSE
> **Approach:** Pure Go — no CGo, no Python. All phases for semester 4 delivery. Local dev deployment.
> **API Design:** Adapted OpenCode hybrid — async message POST (202) + SSE streaming + tree-based history
> **DB Design:** Adapted OpenCode SQLite model — 5 tables extended for agents, memory, delegation

## Overall Progress: ~95%

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
| Phase 9 (Testing) | 0% | **100%** | 16 test files, all packages covered, `go vet` clean |
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
Phase 9 (Testing)      ██████████ 100%  ✓ (16 test files, all pass)
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

### Phase 9 — Testing ✅

**Dependencies:** All other phases complete

**Completed:** 16 test files written across all 8 testable packages:

| Package | Test File | Focus |
|---------|-----------|-------|
| `agent` | `loader_test.go` | YAML frontmatter parsing, edge cases, LoadDirectory |
| `agent` | `engine_test.go` | Process loop, abort, adapters, tool calling, ID generation |
| `api` | `handler_test.go` | All 19 handler endpoints + SSE + route registration |
| `api` | `events_test.go` | EventBus pub/sub/unsubscribe/close/heartbeat |
| `api` | `middleware_test.go` | Auth, CORS, logging, recovery, chain ordering |
| `config` | `config_test.go` | JSON loading, defaults, env overrides, invalid configs |
| `db` | `connect_test.go` | SQLite open + migrate with in-memory DB |
| `history` | `tree_test.go` | BuildTree, GetLongestLeaf, GetPathFromRoot, FindNode, IsLeaf |
| `history` | `branch_test.go` | SelectLeaf, EditMessage, ForkSession |
| `history` | `context_test.go` | BuildContext, truncation, token estimation |
| `history` | `repo_test.go` | GetSession, InsertMessage, GetActiveBranch, GetAllLeaves |
| `llm` | `client_test.go` | ChatCompletion, error handling, tool calls |
| `tools` | `registry_test.go` | Register, Get, List, IsAllowed, ToToolDefinitions |
| `tools` | `web_fetch_test.go` | HTTP mock server, HTML stripping, max size, schema validation |
| `tools` | `memory_test.go` | Save/recall with real SQLite DB, agent scoping, overwrite |
| `tools` | `delegate_test.go` | Cycle detection, timeout, delegation logging (no LLM call) |

**Bug fixes discovered during testing (3 production bugs fixed):**
1. `tools/memory.go` — overwrite support (delete-then-insert, + migration 002)
2. `history/branch.go` — two-pass ID mapping in `ForkSession`  
3. `history/repo.go` — reverse order in `GetActiveBranchByLeafID`
4. `history/tree.go` — cycle protection in `assignDepths`

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