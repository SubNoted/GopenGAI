# LLM Клиент (internal/llm)

## Общий подход

Клиент реализует **OpenAI-совместимый** HTTP-протокол для вызова любых LLM (OpenAI, Anthropic, Ollama, локальные модели) через единый API.

## Типы данных (types.go)

### Запрос к LLM

```go
type ChatCompletionRequest struct {
    Model      string           `json:"model"`
    Messages   []Message        `json:"messages"`
    Tools      []ToolDefinition `json:"tools,omitempty"`
    ToolChoice json.RawMessage  `json:"tool_choice,omitempty"`
}
```

### Сообщение

```go
type Message struct {
    Role       string     `json:"role"`        // system | user | assistant | tool
    Content    string     `json:"content"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    Name       string     `json:"name,omitempty"`
}
```

### Описание инструмента (Tool)

```go
type ToolDefinition struct {
    Type     string       `json:"type"`     // "function"
    Function ToolFunction `json:"function"`
}

type ToolFunction struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}
```

### Ответ от LLM

```go
type ChatCompletionResponse struct {
    ID      string    `json:"id"`
    Choices []Choice  `json:"choices"`
    Usage   Usage     `json:"usage"`
    Error   *APIError `json:"error,omitempty"`
}

type Choice struct {
    Index        int             `json:"index"`
    Message      MessageResponse `json:"message"`
    FinishReason string          `json:"finish_reason"` // stop | tool_calls | length
}
```

### Tool Call от LLM

```go
type ToolCall struct {
    ID       string       `json:"id"`       // e.g., "call_abc123"
    Type     string       `json:"type"`     // "function"
    Function FunctionCall `json:"function"`
}

type FunctionCall struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON строка, нужно распарсить
}
```

### Информация о токенах

```go
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

### Типы для стриминга

```go
type StreamCompletionResponse struct {
    ID      string         `json:"id"`
    Choices []StreamChoice `json:"choices"`
    Usage   *Usage         `json:"usage,omitempty"`
}

type StreamChoice struct {
    Index        int         `json:"index"`
    Delta        StreamDelta `json:"delta"`
    FinishReason string      `json:"finish_reason,omitempty"`
}

type StreamDelta struct {
    Role      string     `json:"role,omitempty"`
    Content   string     `json:"content,omitempty"`
    ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
```

## HTTP-клиент (client.go)

### Структура Client

```go
type Client struct {
    BaseURL    string
    APIKey     string
    Model      string
    HTTPClient *http.Client
}
```

### Создание клиента

```go
// Из отдельных параметров
client := llm.NewClient("https://api.openai.com/v1", "sk-...", "gpt-4o")

// Из конфига
client := llm.NewClientFromConfig(cfg.LLM)
```

### Основной метод — ChatCompletion

```go
func (c *Client) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)
```

Что происходит внутри:

1. Если `req.Model` не задан — подставляется `c.Model`
2. Сериализуем запрос в JSON
3. POST на `{BaseURL}/chat/completions`
4. Заголовки: `Content-Type: application/json`, `Authorization: Bearer {APIKey}`
5. Читаем ответ
6. Если статус не 200 — парсим `APIError` из тела ответа и возвращаем `LLMError`
7. Если статус 200 — десериализуем в `ChatCompletionResponse`

### Структурированная ошибка

```go
type LLMError struct {
    StatusCode int
    Message    string
    APIError   *APIError  // Если удалось распарсить ответ LLM
}

func (e *LLMError) Error() string {
    // Пример: "llm api error (status 401): Invalid API key (type=auth_error code=invalid_api_key)"
}
```

## Стриминг (stream.go)

### Парсинг SSE-потока

```go
// Разбирает SSE-поток на события
func ParseSSEStream(ctx context.Context, r io.Reader, out chan<- SSEEvent) error
```

SSE формат:
```
event: message
data: {"choices":[{"delta":{"content":"Hello"}}]}

event: done
data: [DONE]
```

### Стриминговый вызов LLM

```go
func (c *Client) StreamCompletion(ctx context.Context, req *ChatCompletionRequest) (<-chan SSEEvent, <-chan error)
```

Возвращает два канала:
- **events** — поток SSE-событий от LLM
- **errs** — ошибка (одна, после закрытия events)

### Вспомогательные функции

```go
// Парсит тело SSE-события как StreamCompletionResponse
func ParseStreamData(data string) (*StreamCompletionResponse, error)

// Проверяет, завершён ли стрим (есть finish_reason)
func IsStreamDone(chunk *StreamCompletionResponse) bool
```

## Как это всё связано

```
Клиент (Agent Engine / Handler)
       │
       ▼
  llm.NewClientFromConfig(cfg.LLM)
       │
       ▼
  llm.Client.ChatCompletion(ctx, req)
       │
       ├──→ HTTP POST → LLM Provider (OpenAI, Anthropic, etc.)
       │       │
       │       ▼
       │   JSON-ответ (или поток SSE)
       │
       ▼
  ChatCompletionResponse { Choices, Usage }
```

> **Важно:** В текущей реализации (MVP) хендлер вызывает LLM напрямую, минуя Agent Engine. В будущем Agent Engine будет обёрткой, которая вызывает LLM, обрабатывает tool calls и т.д.
