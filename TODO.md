# AI Core — TODO

> See `DOCS/roadmap.md` for full phased plan.
> See `DOCS/architecture.md` for system design.

## Current: Phase 1 — Agent Core

- [ ] Scaffold FastAPI project structure
- [ ] POST /chat endpoint
- [ ] Polza AI client
- [ ] SQLite user DB + chat history
- [ ] Basic agent loop (prompt → LLM → save → return)
- [ ] **Checkpoint: working chat via curl**

## Next: Phase 2 — RAG Tool

- [ ] POST /upload endpoint
- [ ] Document parsing (txt, pdf)
- [ ] ChromaDB + embeddings pipeline
- [ ] RAG search tool in agent loop
- [ ] **Checkpoint: ask questions about uploaded docs**

## Later

- [ ] Phase 3: Agent Intelligence (tool dispatch, admin API)
- [ ] Phase 4: Go Gateway (stretch)
- [ ] Phase 5: Deploy & Demo

## Meta Tasks

- [ ] Set up GitHub Projects board
- [ ] Clone repo to TNT machine
- [ ] Docker setup

## Learning

- [ ] FastAPI basics: https://fastapi.tiangolo.com/
- [ ] ChromaDB: https://docs.trychroma.com/
- [ ] Polza AI API docs
- [ ] gRPC with Python: https://grpc.io/docs/languages/python/
- [ ] Go basics: https://go.dev/learn/
