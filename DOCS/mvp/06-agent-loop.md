# AI Core — MVP: Логика агента (Flowchart)

> Как Agent Engine обрабатывает каждый запрос: от входа до ответа.

```mermaid
flowchart TD
    Start([Входящий запрос]) --> CheckType{Тип запроса?}

    %% === UPLOAD ===
    CheckType -->|upload| ValidateFile{Файл валиден?<br>(тип + размер)}
    ValidateFile -->|Нет| ErrFile[Ошибка: неверный формат]
    ValidateFile -->|Да| ParseFile[Парсинг документа<br>.txt / .pdf]
    ParseFile --> SplitChunks[Разбиение на чанки<br>chunk_size + overlap]
    SplitChunks --> EmbedChunks[Генерация эмбеддингов<br>sentence-transformers]
    EmbedChunks --> StoreVDB[Сохранение в ChromaDB<br>+ метаданные]
    StoreVDB --> SaveMeta[Сохранение метаданных<br>в SQLite]
    SaveMeta --> UploadOK([Ответ: документ загружен<br>chunks_count])

    %% === CHAT ===
    CheckType -->|chat| LoadHistory[Загрузка истории чатов<br>из SQLite]
    LoadHistory --> HasDocs{У пользователя<br>есть документы?}

    HasDocs -->|Да| RAGSearch[Векторный поиск<br>по ChromaDB]
    RAGSearch --> GotChunks{Найдены<br>релевантные чанки?}
    GotChunks -->|Да| BuildContext[Формирование контекста<br>из чанков + источники]
    GotChunks -->|Нет| NoContext[Контекст = пусто<br>только история]
    HasDocs -->|Нет| NoContext

    BuildContext --> BuildPrompt
    NoContext --> BuildPrompt[Сборка augmented prompt<br>= system + context +<br>history + user_message]

    BuildPrompt --> CallLLM[HTTP POST к Polza AI]
    CallLLM --> LLMOK{Ответ получен?}

    LLMOK -->|Да| ExtractSources[Извлечение источников]
    LLMOK -->|Нет| RetryCheck{Повторить?<br>(до 2 раз)}
    RetryCheck -->|Да| CallLLM
    RetryCheck -->|Нет| ErrLLM[Ошибка: LLM недоступен]

    ExtractSources --> SaveChat[Сохранение сообщений<br>+ usage stats в SQLite]
    SaveChat --> ChatOK([Ответ: content,<br>sources, usage])

    %% === HEALTH ===
    CheckType -->|health| HealthCheck[Проверка компонентов:<br>SQLite, ChromaDB, Polza AI]
    HealthCheck --> HealthOK([Ответ: статус сервисов])

    %% === ERROR HANDLING ===
    ErrFile --> LogError[Логирование ошибки<br>JSON structured log]
    ErrLLM --> LogError
    LogError --> ErrResponse([Ответ: ошибка<br>с описанием])
```

## Принятие решений агентом

В MVP агент **не выбирает** инструменты динамически (это Phase 3).
Вместо этого логика простая и детерминированная:

| Условие | Действие |
|---------|----------|
| Загружен новый файл | → Парсинг → эмбеддинг → ChromaDB |
| У пользователя есть документы | → RAG-поиск перед вызовом LLM |
| У пользователя нет документов | → Только история + user_message |
| LLM не ответил | → Retry (до 2 раз) → ошибка |

В будущем (Phase 3) агент будет **решать** какие инструменты вызывать
на основе содержания запроса (tool dispatch).
