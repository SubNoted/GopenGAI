# AI Core — MVP: Контейнерная диаграмма (C4 Level 2)

> Внутренняя архитектура AI Core: сервисы, хранилища, потоки данных.

```mermaid
C4Container
    title AI Core — Container Diagram (MVP)

    Person(user, "Пользователь", "Отправляет запросы и документы")

    Container(api, "REST API\n(FastAPI)", "Python FastAPI", "Обрабатывает HTTP-запросы:\n/chat, /upload, /health")

    Container(agent, "Agent Engine", "Python", "Логика агента: парсинг,\nформирование контекста,\nвызов инструментов")

    Container(parser, "Document Parser", "Python", "Извлечение текста из\n.txt и .pdf файлов,\nразбиение на чанки")

    Container(rag, "RAG Pipeline", "Python + sentence-transformers", "Генерация эмбеддингов,\nвекторный поиск по ChromaDB,\nвозврат top-k чанков")

    ContainerDb(sqldb, "SQLite DB", "SQLite", "Пользователи, история чатов,\nстатистика использования")

    ContainerDb(chromadb, "Vector Store", "ChromaDB (in-process)", "Эмбеддинги документов\nдля семантического поиска")

    System_Ext(polza, "Polza AI", "Внешний LLM провайдер")

    Rel(user, api, "HTTPS/JSON")
    Rel(api, agent, "Передача запроса\nвнутри процесса")
    Rel(agent, parser, "Парсинг загруженных\nдокументов")
    Rel(agent, rag, "Поиск по документам\n(top-k chunks)")
    Rel(agent, sqldb, "Сохранение/чтение\nистории и статистики")
    Rel(parser, chromadb, "Сохранение\nэмбеддингов чанков")
    Rel(rag, chromadb, "Векторный поиск\nпо эмбеддингам")
    Rel(agent, polza, "HTTP: augmented prompt\n→ completion")

    UpdateLayoutConfig($c4ShapeInRow="3", $c4BoundaryInRow="1")
```

## Описание контейнеров

| Контейнер | Технология | Ответственность |
|-----------|------------|-----------------|
| **REST API** | Python FastAPI | Входная точка. Валидация запросов, маршрутизация, возврат ответов |
| **Agent Engine** | Python | Оркестрация: определяет какие инструменты вызвать, формирует prompt, вызывает Polza AI |
| **Document Parser** | Python (PyMuPDF/pdfplumber) | Извлекает текст из .txt/.pdf, разбивает на чанки, передаёт в RAG Pipeline |
| **RAG Pipeline** | sentence-transformers + ChromaDB | Генерирует эмбеддинги, выполняет семантический поиск, возвращает релевантные чанки |
| **SQLite DB** | SQLite (file) | Постоянное хранение: таблицы users, messages, usage_stats |
| **ChromaDB** | ChromaDB (in-process) | Векторное хранилище: эмбеддинги документов для RAG-поиска |
| **Polza AI** | HTTP API | Внешний LLM: получает augmented prompt, возвращает текстовый ответ |
