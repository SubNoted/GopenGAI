# History Tree — Branching Conversation Model

> How conversations branch when users edit messages or start new threads.

## Tree Structure

```mermaid
flowchart TD
    subgraph "Session: abc-123"
        M1["🔵 msg-1 | role: user | 'Hello, explain Go generics'"]
        M2["🟢 msg-2 | role: assistant | 'Go generics allow...'"]
        M3["🔵 msg-3 | role: user | 'Can you simplify that?'"]
        M4["🟢 msg-4 | role: assistant | 'Sure! Think of generics...'"]
        M5["🔵 msg-5 | role: user | 'Now explain channels'"]
        M6["🟢 msg-6 | role: assistant | 'Channels are typed conduits...'"]

        M7["🔵 msg-7 | role: user | EDIT: 'Explain Go concurrency'"]
        M8["🟢 msg-8 | role: assistant | 'Go concurrency uses goroutines...'"]

        M9["🔧 msg-9 | role: tool | web_fetch result"]
        M10["🔵 msg-10 | role: user | 'Summarize the Go blog'"]
        M11["🟢 msg-11 | role: assistant | 'The Go blog discusses...'"]
    end

    M1 --> M2
    M2 --> M3
    M3 --> M4
    M4 --> M5
    M5 --> M6

    M2 --> M7
    M7 --> M8

    M4 --> M9
    M9 --> M10
    M10 --> M11

    style M1 fill:#2196F3,color:#fff
    style M2 fill:#4CAF50,color:#fff
    style M7 fill:#FF9800,color:#fff
    style M9 fill:#9E9E9E,color:#fff
```

## Branch Selection Rules

| Scenario | Active Branch |
|----------|--------------|
| Default | Longest root→leaf path |
| After edit | New branch from edit point |
| Explicit branch select | User picks leaf via API |

## Key Properties

- **Immutable messages**: editing creates a NEW message node, old one stays
- **Multiple leaves**: a session can have many leaf nodes
- **One active path**: `GET /sessions/:id` returns one root→leaf path
- **Branch listing**: `GET /sessions/:id/branches` returns all leaves
- **Tool messages**: stored in tree as `role: tool` nodes, same structure
- **Delegation traces**: sub-agent calls appear as nested tool calls
