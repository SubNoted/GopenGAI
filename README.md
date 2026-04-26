[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)
[![Python Version](https://img.shields.io/badge/Python-3.10+-3776AB?logo=python)](https://www.python.org/)
[![Status](https://img.shields.io/badge/Status-Active-success)]()

**AI Core** is an AI Agent Service that intelligently handles user requests by combining multiple tools — RAG document search, database queries, document parsing, and LLM generation. It exposes a REST API for integration into corporate workflows, ensuring data privacy and structured responses.

# Key Features

- **AI Agent with Tool Use**
  The agent reasons about which tools to call (RAG search, DB query, document parsing) before generating an answer. It builds context-rich prompts automatically.

- **RAG (Retrieval-Augmented Generation)**
  Upload documents (text, PDF), and the agent will search them to answer questions with source attribution.

- **Private Database Access**
  Connect internal databases. The agent can query structured data to answer questions without exposing data to external services.

- **Polza AI Integration**
  Requests are forwarded to Polza AI for generation. Local model support planned for future.

- **Conversation History**
  Full chat history per user, persisted in database. Agent uses relevant history for context-aware responses.

- **Enterprise Security (planned)**
  - API Key & JWT Authentication (Go gateway layer)
  - PII Masking before sending data to external models
  - Audit logging for compliance

- **Cloud-Native**
  Docker Compose for deployment. Kubernetes support planned.

---

# Architecture

See `DOCS/architecture.md` for diagrams and detailed design.

```text
User → REST API → Agent Engine → [RAG Search | DB Query | Doc Parser] → Polza AI → Response
```

---

# Quick Start

### 1. Clone the Repository
```bash
git clone https://github.com/...
cd aicore
```

### 2. Configure Environment
```bash
cp .env.example .env
```
*Edit `.env` to set your Polza AI API key.*

### 3. Run with Docker Compose
```bash
docker-compose up --build
```

### 4. Verify Health
```bash
curl http://localhost:8080/api/v1/health
```

---

# API Usage

### Chat with AI
**Endpoint:** `POST /api/v1/chat`

**Request:**
```json
{
  "user_id": "user_123",
  "message": "Summarize this corporate policy...",
  "documents": ["optional_base64_or_url"]
}
```

**Response:**
```json
{
  "id": "req_12345",
  "content": "Here is the summary...",
  "sources": ["policy_v2.pdf, page 3"],
  "usage": { "tokens": 120 },
  "provider": "polza"
}
```

### Upload Document
**Endpoint:** `POST /api/v1/upload`

---

# Project Structure

```text
├── cmd/
│   ├── worker/           # Python agent engine (main logic)
│   │   ├── main.py       # FastAPI entrypoint
│   │   ├── agent.py      # Agent loop & tool dispatcher
│   │   ├── rag.py        # RAG search + ChromaDB
│   │   ├── parsers.py    # Document parsing (txt, pdf)
│   │   └── polza.py      # Polza AI client
│   └── gateway/          # Go API gateway (stretch)
│       └── main.go
├── proto/                # gRPC definitions (if Go gateway used)
├── deploy/               # Docker Compose, K8s manifests
├── DOCS/                 # Architecture & roadmap
│   ├── architecture.md
│   ├── roadmap.md
│   └── mvp/
└── tests/
```

---

# Roadmap

See `DOCS/roadmap.md` for the full phased plan.

| Phase | What | Status |
|-------|------|--------|
| Phase 1 | Agent Core (chat + Polza AI) | **Current** |
| Phase 2 | RAG Tool (documents + vector search) | Next |
| Phase 3 | Agent Intelligence (tool dispatch, admin) | Planned |
| Phase 4 | Go Gateway (auth, PII masking) | Stretch |
| Phase 5 | Deploy & Demo | Planned |

---

# Security & Compliance

- **Data Privacy:** PII detection and masking before data leaves the network
- **Audit Trail:** Every request logged with user ID, timestamp, token usage
- **User Isolation:** Per-user settings, history, and document access

---

# Useful References

## Base
- https://youtu.be/WgV6M1LyfNY
- Git: https://youtu.be/xN1-2p06Urc

## RAG & Memory
- RAG: https://youtu.be/GkKSDBgz4XQ
- RAG future: https://youtu.be/qznFV59f3Uk
- AI memory use case: https://youtu.be/mLsxlYuLafE
