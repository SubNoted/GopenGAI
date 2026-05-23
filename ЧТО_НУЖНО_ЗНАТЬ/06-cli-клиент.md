# CLI Клиент (cmd/cli)

## Общий подход

CLI-клиент использует Cobra (`github.com/spf13/cobra`) — стандартную библиотеку для построения CLI в Go. Клиент общается с API-сервером через HTTP.

```
CLI → HTTP POST/GET/DELETE → API Server → SQLite + LLM
```

## Структура команд

```
gopengai [--server URL]
├── chat [message] [--session ID] [--agent NAME]
├── session
│   ├── list
│   ├── show <id>
│   ├── create [--title T] [--agent NAME]
│   └── delete <id>
```

## Глобальные параметры

| Флаг | Сокращение | По умолчанию | Описание |
|------|-----------|-------------|----------|
| `--server` | `-s` | `http://localhost:8080` | URL API-сервера |

## Команда `chat`

### Режим 1: Однократное сообщение

```bash
gopengai chat "Что такое Go?"

# Если указать session-id, продолжит существующий диалог:
gopengai chat -S abc123 "Продолжим про Go"

# С указанием агента для новой сессии:
gopengai chat -a researcher "Исследуй тему Go"
```

Алгоритм:
1. Если `--session` не указан → создаём новую сессию (POST /session) с заголовком = первым 40 символам сообщения
2. Отправляем сообщение (POST /session/{id}/message)
3. Выводим ответ ассистента (только `cr.Content`)
4. Если ошибка — выводим в stderr

### Режим 2: Интерактивный REPL

```bash
gopengai chat
```

Сценарий:
1. Создаётся сессия с заголовком "REPL chat"
2. Выводится приглашение `You:`
3. Пользователь вводит сообщения
4. После каждого ответа снова `You:`
5. `/quit` или `/exit` — выход
6. `/session` — показать ID текущей сессии

### Важные детали

**Обрезание строк:** `messageTrunc` использует руны, а не байты:

```go
func messageTrunc(s string, max int) string {
    if utf8.RuneCountInString(s) <= max {
        return s
    }
    runes := []rune(s)
    return string(runes[:max]) + "..."
}
```

Это критически важно для корректной работы с кириллицей, эмодзи и другими многобайтовыми символами.

**URL сервера:** `strings.TrimSuffix(serverURL, "/")` — обрезает только один завершающий слэш.

## Команда `session`

### session list

```bash
gopengai session list
```

Выводит таблицу: ID (36 символов), TITLE (обрезанный до 18), STATUS, MSG COUNT.

### session show <id>

```bash
gopengai session show abc123
```

Выводит: ID, Title, Agent, Status, Message Count, и все сообщения (роль + контент, обрезанный до 120 символов).

### session create

```bash
gopengai session create --title "Мой проект" --agent researcher
```

### session delete <id>

```bash
gopengai session delete abc123
```

## HTTP-хелперы

```go
func apiGET(path string) (*http.Response, error)
func apiPOST(path string, body any) (*http.Response, error)
func apiDELETE(path string) (*http.Response, error)
```

Все хелперы конкатенируют `serverURL + path`. POST-запросы сериализуют body в JSON.

## Типы для декодирования ответов

```go
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

type MessageView struct {
    ID        string `json:"id"`
    Role      string `json:"role"`
    Content   string `json:"content"`
    AgentName string `json:"agent_name,omitempty"`
    CreatedAt int64  `json:"created_at"`
}

type ChatResponse struct {
    SessionID string `json:"session_id"`
    MessageID string `json:"message_id"`
    Role      string `json:"role"`
    Content   string `json:"content"`
    Model     string `json:"model"`
    Error     string `json:"error,omitempty"`
}
```

## Запланированные команды (не реализованы)

```bash
# SSE-стриминг ответов (вместо синхронного ожидания)
gopengai chat "Привет" --stream

# Управление ветками
gopengai session branches <id>          # список веток
gopengai session fork <id> --msg <msg>  # форк от сообщения
gopengai session switch <id> --leaf <l> # переключение ветки

# Агенты
gopengai agents                     # список агентов
gopengai agents info <name>         # детали агента

# Память
gopengai memory list [--agent NAME]
gopengai memory get <key> [--agent NAME]
```