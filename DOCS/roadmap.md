# AI Core — Implementation Roadmap

> **Team:** 2 people (beginners)
> **Deadline:** ~June 2026
> **Strategy:** Python agent first, Go gateway as stretch goal

## Phase 1: Agent Core (Week 1-2)

Build the foundation — a working chat endpoint that talks to Polza AI.

- [ ] Scaffold FastAPI project (`cmd/worker/`)
- [ ] POST /chat endpoint (accepts prompt + user_id)
- [ ] Polza AI client (HTTP calls to API)
- [ ] SQLite user DB (users table, settings)
- [ ] Chat history table (save messages)
- [ ] Basic agent loop: prompt → Polza AI → save → return
- [ ] **Checkpoint: curl /chat and get a real AI response**

## Phase 2: RAG Tool (Week 2-3)

Add document intelligence — upload docs, ask questions about them.

- [ ] POST /upload endpoint (accept file uploads)
- [ ] Document parser: .txt extraction
- [ ] Document parser: .pdf extraction (use PyMuPDF or pdfplumber)
- [ ] ChromaDB setup (in-process, no separate server needed)
- [ ] Embedding pipeline: split docs → embed → store in ChromaDB
- [ ] RAG search tool: query vector store, return top-k chunks
- [ ] Integrate RAG into agent loop (agent searches docs before answering)
- [ ] **Checkpoint: upload a doc, ask about it, get accurate answer**

## Phase 3: Agent Intelligence (Week 3-4)

Make the agent smarter — tool dispatch, context management, admin config.

- [ ] Tool dispatcher: agent decides which tools to call based on input
- [ ] Conversation context: include relevant chat history in prompts
- [ ] Admin API: configure user settings (model choice, connected DB)
- [ ] Admin API: connect external databases for RAG ingestion
- [ ] Confidence/source attribution in responses
- [ ] **Checkpoint: agent reasons about which tools to use**

## Phase 4: Go Gateway (Stretch — Week 4-5)

Add the enterprise gateway layer on top. Only if Phase 1-3 are solid.

- [ ] Go project scaffold (`cmd/gateway/`)
- [ ] REST API proxy to Python agent
- [ ] JWT / API Key authentication
- [ ] gRPC proto definition + Python gRPC server
- [ ] Go gRPC client
- [ ] PII masking module (detect emails, phones, IDs)
- [ ] **Checkpoint: authenticated requests through Go → Python → Polza AI**

## Phase 5: Deploy & Demo (Week 5)

Polish, containerize, prepare for presentation.

- [ ] Docker Compose: Python agent + SQLite + ChromaDB
- [ ] (If Phase 4 done) Docker Compose: add Go gateway
- [ ] Audit logging (structured JSON logs)
- [ ] API documentation (OpenAPI / Swagger — FastAPI generates this)
- [ ] Integration tests
- [ ] Demo script (repeatable demo scenario)
- [ ] Presentation slides

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Go too hard / no time | Phase 4 is optional. Phase 1-3 = passing grade |
| ChromaDB issues | ChromaDB is in-process (pip install, no server). Fallback: keyword search |
| Polza AI down during demo | Cache responses, have mock mode |
| Scope creep | Each phase has a checkpoint — always have something to show |
