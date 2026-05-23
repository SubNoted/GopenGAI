# HTTP API (internal/api + cmd/api/main.go)

## Общая архитектура

API-сервер — это стандартный HTTP-сервер Go 1.21+ с использованием нового роутера Go 1.22+ (с поддержкой методов в маршрутах и path-параметров).

```
Запрос → net/http ServeMux → Handler → { DB | LLM } → JSON-ответ
```

## Маршруты (routes.go)

```go
func RegisterRoutes(mux *http.ServeMux, h *Handler) {
    // Health
    mux.HandleFunc("GET /health", h.HandleHealth)

    // Session CRUD
    mux.HandleFunc("POST /session", h.HandleCreateSession)
    mux.HandleFunc("GET /session", h.HandleListSessions)
    mux.HandleFunc("GET /session/{id}", h.HandleGetSession)
    mux.HandleFunc("DELETE /session/{id}", h.HandleDeleteSession)

    // Chat
    mux.HandleFunc("POST /session/{id}/message", h.HandleChatMessage)

    // OpenAI-compatible pass-through
    mux.HandleFunc("POST /v1/chat/completions", h.HandleChatCompletion)
}
```

> **Go 1.22+ routing:** `"GET /session/{id}"` — метод + путь с параметром `{id}`, доступным через `r.PathValue("id")`.

## Handler (handler.go)

### Структура Handler

```go
type Handler struct {
    LLM    *llm.Client
    DB     *db.Queries
    SQLDB  *sql.DB
    Config *config.Config
}
```

### Эндпоинты

#### GET /health
```
Ответ: {"status":"ok"}
```

#### POST /session
Создаёт новую сессию. Если `agent_name` не указан — используется `default_agent` из конфига.
Если `title` не указан — генерируется "Chat YYYY-MM-DD HH:MM".

```
Запрос:  {"title": "Мой чат", "agent_name": "researcher"}
Ответ:   201 {SessionView}
```

#### GET /session
```
Ответ: 200 [{SessionView}, ...] (сортировка по updated_at DESC)
```

#### GET /session/{id}
Возвращает сессию + все сообщения в линейном порядке (по created_at ASC).

```
Ответ: 200 {SessionView с messages}
```

#### DELETE /session/{id}
```
Сначала удаляет сообщения, потом сессию.
Ответ: 200 {"status":"deleted"}
```

#### POST /session/{id}/message — **Ключевой эндпоинт**

Это самый сложный эндпоинт. Вот полный алгоритм:

```
1. Парсим тело запроса: {"content": "..."}
2. Открываем SQL-транзакцию
3. Читаем сессию (GetSessionByID)
4. Определяем parent_id (active_leaf_id, если есть)
5. Создаём сообщение пользователя с parent_id
6. Обновляем статус сессии → "working"
7. Устанавливаем active_leaf_id → ID сообщения пользователя
8. Коммитим транзакцию
9. (вне транзакции) Загружаем всю историю сообщений сессии
10. Конвертируем DB-сообщения в LLM-формат
11. Вызываем LLM (ChatCompletion)
   - Если ошибка → сохраняем error-сообщение, отвечаем 502
12. Сохраняем ответ ассистента (новая транзакция)
   - Создаём сообщение с ролью "assistant"
   - Обновляем active_leaf_id → ID ответа ассистента
   - Статус → "idle"
13. Отвечаем клиенту: 200 {ChatResponse}
```

### Почему две транзакции?

```
Транзакция 1:
  SELECT session WHERE id = ?
  INSERT messages (user message)
  UPDATE sessions SET status='working', active_leaf_id=?

      ↓ LLM вызов (может длиться 5-30 секунд) ↓

Транзакция 2:
  INSERT messages (assistant response)
  UPDATE sessions SET status='idle', active_leaf_id=?
```

Если бы LLM вызов был внутри транзакции, SQLite был бы заблокирован на всё время ответа LLM, и другие запросы не могли бы читать/писать БД.

### Генерация ID

```go
func newID() string {
    b := make([]byte, 16)
    if _, err := rand.Read(b); err != nil {
        // Fallback: timestamp + nanosec
        now := time.Now()
        return fmt.Sprintf("fallback-%x-%x", now.UnixMilli(), now.Nanosecond())
    }
    return fmt.Sprintf("%x-%x-%x-%x-%x",
        b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
```

> **Важно:** `crypto/rand` почти никогда не выдаёт ошибку (только в контейнерах без энтропии). Fallback — защита от паники.

### Безопасность ошибок

```go
func (h *Handler) internalError(w http.ResponseWriter, err error) {
    log.Printf("internal error: %v", err)         // реальная ошибка — в лог
    http.Error(w, "internal server error", http.StatusInternalServerError)  // клиенту — generic
}
```

Все внутренние ошибки (SQL, файловая система) проходят через `internalError`. Исключения:
- 400 Bad Request — неверный ввод пользователя
- 404 Not Found — сессия не найдена

### request/response типы

```go
type ChatRequest struct {
    Model      string               `json:"model"`
    Messages   []llm.Message        `json:"messages"`
    Tools      []llm.ToolDefinition `json:"tools,omitempty"`
    ToolChoice json.RawMessage      `json:"tool_choice,omitempty"`
}

type CreateSessionRequest struct {
    Title     string `json:"title,omitempty"`
    AgentName string `json:"agent_name,omitempty"`
}

type ChatResponse struct {
    SessionID string     `json:"session_id"`
    MessageID string     `json:"message_id"`
    Role      string     `json:"role"`
    Content   string     `json:"content"`
    Model     string     `json:"model"`
    Usage     *llm.Usage `json:"usage,omitempty"`
    Error     string     `json:"error,omitempty"`
}

type SessionView struct {
    ID           string        `json:"id"`
    AgentName    string        `json:"agent_name"`
    Title        string        `json:"title"`
    Status       string        `json:"status"`
    MessageCount int64         `json:"message_count"`
    CreatedAt    int64         `json:"created_at"`
    UpdatedAt    int64         `json:"updated_at"`
    Messages     []MessageView `json:"messages,omitempty"`
}
```

### Вспомогательные функции

```go
// Конвертация DB-сообщений в LLM-формат (только user + assistant)
func dbMessagesToLLM(msgs []db.Message) []llm.Message

// Конвертация DB-сообщений в API-view
func toMessageViews(msgs []db.Message) []MessageView

// Конвертация сессии в API-view
func toSessionView(s db.Session, msgs []MessageView) SessionView
```

## Точка входа сервера (cmd/api/main.go)

### Полный алгоритм старта

```go
func main() {
    // 1. Определяем путь к конфигу
    //    По умолчанию "gopengai.json", можно передать аргументом
    cfgPath := "gopengai.json"
    if len(os.Args) > 1 {
        cfgPath = os.Args[1]
    }

    // 2. Загружаем конфиг
    cfg, err := config.Load(cfgPath)

    // 3. Открываем SQLite БД
    database, err := db.Open(cfg.DataDir + "/gopengai.db")

    // 4. Запускаем миграции
    db.Migrate(database)

    // 5. Создаём sqlc-запросы
    queries := db.New(database)

    // 6. Создаём LLM-клиент
    client := llm.NewClientFromConfig(cfg.LLM)

    // 7. Собираем Handler
    handler := &api.Handler{
        LLM:    client,
        DB:     queries,
        SQLDB:  database,
        Config: cfg,
    }

    // 8. Регистрируем маршруты
    mux := http.NewServeMux()
    api.RegisterRoutes(mux, handler)

    // 9. Запускаем HTTP-сервер
    addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
    http.ListenAndServe(addr, mux)
}
```

## Запланированные эндпоинты (не реализованы)

| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/event` | Глобальный SSE-поток событий |
| PATCH | `/session/{id}` | Обновление сессии |
| GET | `/session/{id}/messages` | Сообщения активной ветки (через рекурсивный CTE) |
| GET | `/session/{id}/events` | SSE-поток для конкретной сессии |
| GET | `/session/{id}/branches` | Список листьев дерева (ветки) |
| POST | `/session/{id}/fork` | Форк сессии от указанного сообщения |
| PUT | `/session/{id}/branch` | Выбор активной ветки |
| PATCH | `/messages/{id}` | Редактирование сообщения → новая ветка |
| GET | `/agents` | Список зарегистрированных агентов |
| GET | `/agents/{name}` | Детали агента |
| GET | `/memory` | Список фактов памяти |
| POST | `/session/{id}/abort` | Прерывание генерации |
| GET | `/v1/models` | Список агентов как моделей (OpenAI-совместимый) |

> **Важно:** После реализации Agent Engine эндпоинт `POST /session/{id}/message` будет переделан на асинхронный паттерн: **202 Accepted** + SSE-стрим результата, вместо синхронного 200.