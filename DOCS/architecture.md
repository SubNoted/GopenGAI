# AI Core — Architecture

## What is AI Core?

AI Core is an **AI Agent Service** — not a simple gateway.
Users interact via REST API. Behind the scenes, an agent engine decides
which tools to use (RAG search, database query, document parsing, LLM call)
to build the best possible answer.

## Container Diagram

```mermaid
C4Container
    title AI Core — Container Diagram

    Person(user, "End User", "Sends chat requests & documents")
    Person(admin, "Admin", "Configures users, models, connected databases")

    Container(api, "API Layer", "Python FastAPI", "REST: /chat, /upload, /admin")
    Container(agent, "Agent Engine", "Python", "Reasoning loop: picks tools, builds prompts, calls LLM")
    ContainerDb(sqldb, "Users & History DB", "SQLite", "Users, settings, chat history, usage stats")
    ContainerDb(vectordb, "Vector Store", "ChromaDB", "Document embeddings for RAG")

    System_Ext(polza, "Polza AI", "External LLM provider")

    Rel(user, api, "POST /chat, POST /upload", "HTTPS/JSON")
    Rel(admin, api, "POST /admin/config", "HTTPS/JSON")
    Rel(api, agent, "Forward request")
    Rel(agent, vectordb, "RAG: search relevant docs")
    Rel(agent, sqldb, "Query user data, save history")
    Rel(agent, polza, "Generate completion")
```

## Request Flow (Agent Loop)

```mermaid
flowchart TD
    Start([User sends message + maybe a document]) --> Parse[Parse input\nextract text from docs]
    Parse --> Recall[Load conversation history\nfrom User DB]
    Recall --> Decide{Agent decides:\nwhat tools do I need?}

    Decide -->|Need doc context| RAG[Search Vector Store\nfor similar chunks]
    Decide -->|Need user data| DB[Query User DB\nfor relevant records]
    Decide -->|New document to process| DocParse[Parse & embed\ndocument into Vector Store]
    Decide -->|Have enough context| Build

    RAG --> Build
    DB --> Build
    DocParse --> Build

    Build[Build augmented prompt\n= system prompt + context + history + user message]
    Build --> LLM[Send to Polza AI]
    LLM --> Save[Save response to\nchat history DB]
    Save --> Return([Return answer to user])
```

## Agent Tools

The agent can invoke these tools:

| Tool | Description | When Used |
|------|-------------|-----------|
| **RAG Search** | Searches vector store for relevant document chunks | User asks about uploaded docs |
| **DB Query** | Queries connected databases for user data | User asks about structured data |
| **Doc Parser** | Extracts text from uploaded files (txt, pdf) | User uploads a new document |
| **LLM Call** | Sends augmented prompt to Polza AI | Agent has enough context to answer |

## Future: Go Gateway Layer

In a later phase, a Go-based gateway can be added in front of the Python agent:

- JWT/API Key authentication
- PII masking before requests reach the agent
- Audit logging
- Rate limiting
- Request routing

This is an additive layer — the Python agent works independently.

## Future: Agent Intelligence

- Chain-of-thought: agent recursively refines answers
- Self-check mode: agent evaluates confidence in its response
- Web search tool: search internet while keeping private data local
- Multimodal: parse images, video, audio
