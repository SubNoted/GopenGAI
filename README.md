[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev/)
[![Python Version](https://img.shields.io/badge/Python-3.10+-3776AB?logo=python)](https://www.python.org/)
[![Status](https://img.shields.io/badge/Status-Active-success)]()

**Enterprise AI Gateway** is a high-performance service designed to securely integrate generative AI models into corporate infrastructure. It provides a unified API interface for routing requests between local on-premise models and external AI providers, ensuring data privacy, compliance, and scalability.

# Key Features

- **Unified API Interface** 
  Single endpoint for all AI interactions, abstracting away the complexity of different model providers. Work with **text, images, videos and audio** throughout simple unified way.


- **Smart Routing** 
  Dynamically route requests based on configuration:
  - **Local Models** 
    Secure, on-premise inference of local models
  - **External Providers** 
    Direct integration with cloud APIs (OpenAI, Azure, etc.).

- **Data privacy and control**
  Your data will stay private if you use local models. User can provide his documents in chat or admin can connect database to aicore.

- **Enterprise Security:**
  - API Key & JWT Authentication.
  - PII (Personally Identifiable Information) Masking before sending data to external models.
  - Audit logging for compliance.

- **Hybrid Architecture:**
  - **Go Gateway:** High-concurrency API handling, authentication, and routing.
  - **Python Worker:** Specialized ML inference engine using PyTorch/Transformers.

- **Observability:** Structured JSON logging, metrics export, and distributed tracing ready.

- **Cloud-Native:** Ready for deployment on Kubernetes (Helm charts included) and Docker Compose.

# Architecture

-

### Why Hybrid?
- **Go:** Handles network I/O, authentication, and routing with minimal latency and high concurrency.
- **Python:** Leverages the rich ecosystem of AI libraries (`torch`, `transformers`) for model inference without compromising the gateway's stability.

# Tech Stack

| Component | Technology |
| :--- | :--- |
| **API Gateway** | Go (Gin/Echo), gRPC Client |
| **ML Worker** | Python (FastAPI/gRPC), PyTorch, Transformers |
| **Communication** | gRPC (Protobuf) |
| **Containerization** | Docker, Docker Compose |
| **Orchestration** | Kubernetes (K8s), Helm |
| **Logging** | Zap (Go), Loguru (Python), JSON Format |
| **Testing** | Go `testing`, Python `pytest` |



# Quick Start (TODO change)

### 1. Clone the Repository
```bash
git clone https://github.com/...
cd ai-gateway-service
```

### 2. Configure Environment
Copy the example environment file and adjust settings:
```bash
cp .env.example .env
```
*Edit `.env` to set your API keys, model paths, and routing preferences.*

### 3. Run with Docker Compose
This will start both the Go Gateway and the Python Worker:
```bash
docker-compose up --build
```

### 4. Verify Health
Check the health endpoint:
```bash
curl http://localhost:8080/api/v1/health
```

## ⚙️ Configuration

Key environment variables (`.env`):

| Variable | Description | Default |
| :--- | :--- | :--- |
| `GATEWAY_PORT` | Port for the Go API Gateway | `8080` |
| `WORKER_ADDR` | gRPC address of the Python Worker | `localhost:50051` |
| `ROUTING_MODE` | `local` (Python) or `external` (API) | `local` |
| `AUTH_ENABLED` | Enable JWT/API Key validation | `true` |
| `LOG_LEVEL` | Logging verbosity (info, debug, error) | `info` |
| `EXTERNAL_API_KEY` | Key for external providers (if used) | `""` |

---

# API Usage

### Generate Completion
**Endpoint:** `POST /api/v1/generate`

**Request:**
```json
{
  "prompt": "Summarize this corporate policy...",
  "model": "llama-3-local",
  "max_tokens": 500
}
```

**Response:**
```json
{
  "id": "req_12345",
  "content": "Here is the summary...",
  "usage": { "tokens": 120 },
  "provider": "local"
}
```

---


# Project Structure (TODO)

```text
├── cmd/
│   ├── gateway/          # Go entrypoint
│   └── inference-worker/ # Python entrypoint
├── proto/                # gRPC Protobuf definitions
├── deploy/               # K8s manifests & Helm charts
├── docs/                 # API specs & Architecture docs
└── tests/                # Integration & Unit tests
```

---

# Security & Compliance

- **Data Privacy:** Sensitive data patterns (emails, phones, IDs) are detected and wont leave the internal network before asking you.
- **Audit Trail:** Every request is logged with user ID, timestamp, and token usage for billing and compliance audits.

---

# TODO
(higher is more important)


- [ ] create structure scheme
- [ ] how to use github projects?


## what to learn

- [ ] clone repo to TNT machine
  - [ ] make branch for TNT
  - [ ] TNT makes first commit
  - [ ] merge (or baseon?)

- [ ] learn tech stack in readme, change it

- [ ] how to use code assistant
- [ ] give assistant internet connection


## How to deploy

- [ ] docker


## Someday 

- [ ] testing

---

# Some useful tech information 

## base
 
https://youtu.be/WgV6M1LyfNY

- git
  https://youtu.be/xN1-2p06Urc
  

---

## memory work

- rag 
  https://youtu.be/GkKSDBgz4XQ

- rag and future
  https://youtu.be/qznFV59f3Uk

- ai memory use case 
  https://youtu.be/mLsxlYuLafE

...