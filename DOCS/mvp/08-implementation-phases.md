# AI Core — MVP: Фазы реализации (Gantt Chart)

> План реализации MVP на ~4 недели.

```mermaid
gantt
    title AI Core — MVP Implementation (4 недели)
    dateFormat  YYYY-MM-DD
    axisFormat  %d %b

    section Phase 1: Foundation
    Scaffold FastAPI project           :a1, 2026-05-01, 1d
    POST /health endpoint              :a2, after a1, 1d
    POST /chat endpoint (stub)         :a3, after a2, 1d
    Polza AI HTTP client               :a4, after a3, 2d
    SQLite schema + models             :a5, after a3, 1d
    Chat history CRUD                  :a6, after a5, 1d
    Basic agent loop (no RAG)          :a7, after a4, 2d
    Checkpoint: curl /chat works       :milestone, after a7, 0d

    section Phase 2: RAG Pipeline
    POST /upload endpoint              :b1, after a7, 1d
    Document parser: .txt              :b2, after b1, 1d
    Document parser: .pdf              :b3, after b2, 2d
    ChromaDB setup + embeddings        :b4, after b2, 2d
    RAG search tool                    :b5, after b4, 2d
    Integrate RAG into agent loop      :b6, after b5, 2d
    Checkpoint: upload doc + ask       :milestone, after b6, 0d

    section Phase 3: Polish
    Structured JSON logging            :c1, after b6, 1d
    Error handling + retry             :c2, after c1, 1d
    Usage statistics tracking          :c3, after c1, 1d
    Docker Compose setup               :c4, after c3, 2d
    Integration tests                  :c5, after c4, 2d
    OpenAPI / Swagger docs             :c6, after c4, 1d
    Checkpoint: full demo works        :milestone, after c5, 0d

    section Phase 4: Delivery
    Demo script                        :d1, after c5, 1d
    Documentation cleanup              :d2, after d1, 1d
    Presentation prep                  :d3, after d2, 2d
    DONE                               :milestone, after d3, 0d
```

## Описание фаз

### Phase 1: Foundation (Неделя 1)
**Цель:** Рабочий чат через curl.

| Задача | Детали |
|--------|--------|
| Scaffold FastAPI | Структура проекта, `main.py`, `requirements.txt` |
| /health | Проверка доступности сервиса |
| /chat (stub) | Принимает запрос, возвращает заглушку |
| Polza AI client | HTTP POST к API, обработка ответа |
| SQLite schema | Таблицы: `users`, `messages`, `usage_stats` |
| Chat history | CRUD для сообщений |
| Agent loop | prompt → Polza AI → save → return |

### Phase 2: RAG Pipeline (Неделя 2-3)
**Цель:** Загрузка документов + вопросы по ним.

| Задача | Детали |
|--------|--------|
| /upload | Принимает файл, валидирует |
| Parser .txt | Чтение текстовых файлов |
| Parser .pdf | PyMuPDF / pdfplumber |
| ChromaDB | In-process setup, коллекция для эмбеддингов |
| RAG search | Векторный поиск → top-k чанков |
| Integrate | Agent ищет по документам перед LLM |

### Phase 3: Polish (Неделя 3-4)
**Цель:** Готовность к демонстрации.

| Задача | Детали |
|--------|--------|
| Logging | JSON structured: user_id, timestamp, tokens |
| Error handling | Retry LLM calls, валидация входов |
| Usage stats | Запись token usage в SQLite |
| Docker Compose | Python app + volumes для SQLite/ChromaDB |
| Tests | Интеграционные тесты /chat и /upload |
| Swagger | FastAPI auto-generates OpenAPI |

### Phase 4: Delivery (Неделя 4)
**Цель:** Презентация проекта.

| Задача | Детали |
|--------|--------|
| Demo script | Повторяемый сценарий для показа |
| Docs | Финальная чистка документации |
| Presentation | Слайды для защиты курсовой |
