# Message Flow — Async Agent Processing

> How a message flows through the system from POST to completion.
> GoPengAI uses async processing: POST returns 202, response streams via SSE.

## Flow Diagram

```mermaid
flowchart TD
    Start([POST /session/:id/message]) --> Parse[Parse request body]
    Parse --> Validate{Valid?}

    Validate -->|No| Err400[400 Bad Request]
    Validate -->|Yes| LoadSession{Session exists?}

    LoadSession -->|No| Err404[404 Not Found]
    LoadSession -->|Yes| SaveUser[Save user message to tree<br/>parent = active_leaf]

    SaveUser --> Accepted[Return 202 Accepted<br/>{message_id, status: accepted}]

    Accepted --> BG[Start background goroutine]

    BG --> SetWorking[Set session status: working<br/>Publish session.status via SSE]
    SetWorking --> LoadAgent{Agent specified?}

    LoadAgent -->|Yes| LoadByName[Load agent from registry]
    LoadAgent -->|No| LoadDefault[Load session default agent]

    LoadByName --> BuildContext
    LoadDefault --> BuildContext

    BuildContext["Build context:<br/>1. System prompt<br/>2. Branch history (root → leaf)<br/>3. New user message"]

    BuildContext --> CallLLM[POST /v1/chat/completions<br/>to LLM provider]

    CallLLM --> ParseResp{Response type?}

    ParseResp -->|Stop: text| SaveAssistant[Save assistant message<br/>Publish message.part.added<br/>Publish message.part.updated]
    SaveAssistant --> Done[Publish message.complete<br/>Set session status: idle]

    ParseResp -->|Tool calls| ExecTools[Publish message.tool.started<br/>for each tool]

    ExecTools --> ToolPerm{Tool allowed?}

    ToolPerm -->|Deny| ToolDenied[Save tool result: denied<br/>Publish message.tool.error]
    ToolDenied --> CallLLM

    ToolPerm -->|Allow| RunTool[Execute tool]
    RunTool --> SaveToolResult[Save tool call + result messages<br/>Publish message.tool.completed]
    SaveToolResult --> LoopCheck{Max iterations<br/>reached?}

    LoopCheck -->|No| CallLLM
    LoopCheck -->|Yes| ForcedStop[Force stop<br/>Publish message.error]

    Done --> Return([Background goroutine exits])
    ForcedStop --> Return

    style Start fill:#4CAF50,color:#fff
    style Accepted fill:#FF9800,color:#fff
    style Done fill:#4CAF50,color:#fff
    style Err400 fill:#f44336,color:#fff
    style Err404 fill:#f44336,color:#fff
```

---

## Sequence: Full Message Lifecycle

```mermaid
sequenceDiagram
    actor User
    participant Client as Client
    participant API as HTTP API
    participant Bus as Event Bus
    participant Engine as Agent Engine
    participant DB as SQLite
    participant LLM as LLM Provider
    participant Tool as Tool (web_fetch)

    User->>Client: "Explain Go generics"
    Client->>Bus: GET /session/:id/events (subscribe)
    Client->>API: POST /session/:id/message {content}

    API->>DB: Save user message (parent = active_leaf)
    API-->>Client: 202 Accepted {message_id}

    API->>Engine: Process(sessionID, content) [goroutine]
    Engine->>Bus: session.status → working

    Engine->>DB: Load session + branch history
    Engine->>Engine: Load agent, build context

    Engine->>LLM: POST /v1/chat/completions
    LLM-->>Engine: {tool_calls: [{name: "web_fetch", args: {url}}]}

    Engine->>Bus: message.tool.started {web_fetch}
    Engine->>Tool: Execute({url})
    Tool-->>Engine: "page content..."
    Engine->>Bus: message.tool.completed {web_fetch}
    Engine->>DB: Save tool call + result messages

    Engine->>LLM: POST /v1/chat/completions (with tool result)
    LLM-->>Engine: {content: "Based on the web page..."}

    Engine->>Bus: message.part.added {assistant}
    Engine->>Bus: message.part.updated {content streaming}
    Engine->>DB: Save assistant message

    Engine->>Bus: message.complete {full response, usage}
    Engine->>DB: Update session active_leaf_id
    Engine->>Bus: session.status → idle

    Client-->>User: Display response
```

---

## Session Status State Machine

```mermaid
stateDiagram-v2
    [*] --> Idle : Session created
    Idle --> Working : POST /session/:id/message
    Working --> Idle : Agent completes (message.complete)
    Working --> Idle : Error (message.error)
    Working --> Idle : Aborted (POST /session/:id/abort)

    Idle --> Idle : GET, PATCH, etc. (non-mutating)
    Idle --> [*] : DELETE session
```

---

## Background Goroutine Lifecycle

```mermaid
flowchart LR
    Accept[202 Accepted] --> Goroutine[engine.Process goroutine]
    Goroutine --> Cleanup[defer: set status idle, close SSE listeners]
    Goroutine --> Panic[defer: recover → publish error event]

    Goroutine -->|Success| Complete[message.complete]
    Goroutine -->|Error| ErrEvent[message.error]
    Goroutine -->|Aborted| AbortEvent[message.error + aborted flag]
    Goroutine -->|Max iter| ForcedStop[message.error + max_iterations]
```

---

## Tool Permission Check

```mermaid
flowchart TD
    ToolCall[LLM returns tool_calls] --> FindTool{Tool in registry?}
    FindTool -->|No| ToolError["Result: 'tool not found'"]
    ToolError --> SaveResult[Save as tool result message]
    SaveResult --> CallLLM[Continue agent loop]

    FindTool -->|Yes| CheckPerm{Tool permission<br/>in agent config?}

    CheckPerm -->|Allow| Exec[Execute tool]
    Exec --> Success[Save result, continue loop]

    CheckPerm -->|Deny| Denied["Result: 'tool execution denied by policy'"]
    Denied --> SaveResult

    style ToolError fill:#f44336,color:#fff
    style Denied fill:#FF9800,color:#fff
    style Success fill:#4CAF50,color:#fff
```

---

## Abort Flow

```mermaid
sequenceDiagram
    participant C as Client
    participant API as HTTP API
    participant E as Engine (running)
    participant Bus as Event Bus

    C->>API: POST /session/:id/abort
    API->>E: Cancel context (context.WithCancel)
    API-->>C: 200 {aborted: true}

    E->>E: Context cancelled at next check point
    E->>Bus: message.error {error: "aborted by user"}
    E->>Bus: session.status → idle
```

---

## Design Invariants

1. **User message is always saved before 202 is returned** — ensures no message loss
2. **Session status is always reset to idle** — even on panic (via defer)
3. **Max iteration limit** — prevents infinite tool call loops (default: 10)
4. **Context cancellation** — abort kills LLM calls in progress
5. **Tool results always saved** — even errors are persisted for debugging
6. **Active leaf updated after full response** — not after each intermediate message
