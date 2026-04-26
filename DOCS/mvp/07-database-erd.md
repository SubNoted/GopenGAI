# AI Core — MVP: Схема базы данных (ERD)

> Структура SQLite базы данных для MVP.

```mermaid
erDiagram
    USER ||--o{ MESSAGE : sends
    USER ||--o{ DOCUMENT : uploads
    USER ||--o{ USAGE_STAT : has
    DOCUMENT ||--o{ DOC_CHUNK : contains

    USER {
        string id PK "UUID, уникальный идентификатор"
        string name "Имя пользователя"
        datetime created_at "Дата создания"
        datetime last_active "Последняя активность"
    }

    MESSAGE {
        int id PK "Автоинкремент"
        string user_id FK "→ USER.id"
        string role "user / assistant / system"
        text content "Текст сообщения"
        string document_id FK "→ DOCUMENT.id (опционально)"
        datetime created_at "Время отправки"
    }

    DOCUMENT {
        string id PK "UUID, уникальный идентификатор"
        string user_id FK "→ USER.id"
        string filename "Оригинальное имя файла"
        string file_type "txt / pdf"
        int chunk_count "Количество чанков"
        int file_size_bytes "Размер файла"
        datetime uploaded_at "Дата загрузки"
    }

    DOC_CHUNK {
        int id PK "Автоинкремент"
        string document_id FK "→ DOCUMENT.id"
        int chunk_index "Порядковый номер чанка"
        text content "Текст чанка"
        int page_number "Номер страницы (для PDF)"
        string embedding_id "ID в ChromaDB"
    }

    USAGE_STAT {
        int id PK "Автоинкремент"
        string user_id FK "→ USER.id"
        string request_id "ID запроса"
        int prompt_tokens "Токены в prompt"
        int completion_tokens "Токены в ответе"
        int total_tokens "Суммарно"
        float latency_ms "Время ответа (мс)"
        datetime created_at "Время запроса"
    }
```

## Описание таблиц

| Таблица | Назначение | Ключевые поля |
|---------|------------|---------------|
| **USER** | Пользователи системы | `id` (UUID), `name`, timestamps |
| **MESSAGE** | История всех сообщений | `user_id`, `role`, `content`, `created_at` |
| **DOCUMENT** | Загруженные документы | `user_id`, `filename`, `file_type`, `chunk_count` |
| **DOC_CHUNK** | Разбиения документов на чанки | `document_id`, `chunk_index`, `content`, `page_number` |
| **USAGE_STAT** | Статистика использования API | `user_id`, token counts, `latency_ms` |

## Связи

```
USER ──(1:N)──> MESSAGE        Пользователь имеет много сообщений
USER ──(1:N)──> DOCUMENT       Пользователь загружает много документов
USER ──(1:N)──> USAGE_STAT     Статистика по каждому запросу
DOCUMENT ──(1:N)──> DOC_CHUNK  Документ разбит на чанки
```

## Примечания

- **DOC_CHUNK хранит только текст и метаданные.** Векторные эмбеддинги
  хранятся в ChromaDB, а ссылка на них — в поле `embedding_id`.
- **MESSAGE.role** принимает значения: `user` (запрос), `assistant` (ответ),
  `system` (системные сообщения агента).
- **USAGE_STAT** нужна для мониторинга и потенциальной тарификации.
