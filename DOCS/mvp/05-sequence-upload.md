# AI Core — MVP: Последовательность /upload запроса

> Жизненный цикл загрузки документа: парсинг, эмбеддинг, сохранение.

```mermaid
sequenceDiagram
    autonumber
    participant U as Пользователь
    participant API as FastAPI API
    participant A as Agent Engine
    participant P as Document Parser
    participant RAG as RAG Pipeline
    participant VDB as ChromaDB
    participant DB as SQLite

    U->>API: POST /upload<br>{user_id, file (.txt/.pdf)}
    API->>API: Валидация формата файла<br>(тип, размер)

    API->>A: Передать файл
    A->>P: Парсинг документа

    alt Файл = .txt
        P->>P: Чтение текста<br>из .txt файла
    else Файл = .pdf
        P->>P: Извлечение текста<br>из .pdf (PyMuPDF)
    end

    P->>P: Разбиение на чанки<br>(chunk_size + overlap)
    P-->>A: chunks[] с метаданными<br>(источник, страница)

    A->>RAG: Эмбеддинг чанков
    RAG->>RAG: Генерация векторов<br>(sentence-transformers)
    RAG->>VDB: Сохранение эмбеддингов<br>+ метаданных
    VDB-->>RAG: OK

    RAG-->>A: Подтверждение

    A->>DB: Сохранить мета-данные<br>документа (user_id, filename,<br>chunk_count, timestamp)
    DB-->>A: OK

    A-->>API: {status, document_id,<br>chunk_count}
    API-->>U: 200 OK<br>{id, chunks_count, message}
```

## Описание шагов

| Шаг | Действие | Детали |
|-----|----------|--------|
| 1 | Пользователь загружает файл | `POST /upload` с `user_id` и файлом (.txt / .pdf) |
| 2 | Валидация | Проверка типа файла, размера |
| 3 | Передача в парсер | Файл передаётся в Document Parser |
| 4-5 | Парсинг | Извлечение текста (txt = чтение, pdf = PyMuPDF) |
| 6 | Разбиение на чанки | Chunk size ~500 токенов, overlap ~50 |
| 7 | Возврат чанков | Массив чанков с метаданными (файл, страница) |
| 8-10 | Эмбеддинг | sentence-transformers → векторы → сохранение в ChromaDB |
| 11-12 | Метаданные в SQLite | filename, chunk_count, timestamp, user_id |
| 13-14 | Ответ пользователю | `{id, chunks_count, message: "Документ загружен"}` |

## Важно

- После загрузки документа, следующие `/chat` запросы от этого пользователя
  **автоматически** будут искать по загруженным документам (RAG).
- Документы привязаны к `user_id` — каждый пользователь видит только свои.
