# AI Core — MVP (Минимально жизнеспособный продукт)

> **Цель:** Рабочий AI Agent Service с REST API, способный отвечать на вопросы
> пользователей с учётом загруженных документов и истории диалогов.
>
> **Дедлайн:** ~4 недели (июнь 2026)
> **Команда:** 2 человека (начинающие)

---

## Что входит в MVP

### 1. REST API (FastAPI)
- `POST /chat` — отправка сообщения, получение AI-ответа
- `POST /upload` — загрузка документов (.txt, .pdf) для RAG
- `GET /health` — проверка работоспособности

### 2. Agent Engine
- Базовый цикл: вход → парсинг → контекст → Polza AI → сохранение → ответ
- Автоматический поиск по загруженным документам (RAG)
- Использование истории диалога для контекстных ответов

### 3. Polza AI Integration
- HTTP-клиент для внешнего LLM провайдера
- Передача augmented prompt (system + context + history + user message)

### 4. Данные (SQLite + ChromaDB)
- **SQLite:** пользователи, история чатов, статистика использования
- **ChromaDB (in-process):** векторные эмбеддинги документов для RAG

### 5. Парсер документов
- Извлечение текста из .txt и .pdf
- Разбиение на чанки → эмбеддинги → сохранение в ChromaDB

### 6. Логирование
- Структурированные JSON-логи каждого запроса
- User ID, timestamp, token usage, результат

---

## Что НЕ входит в MVP (отложено)

| Компонент | Почему отложено |
|-----------|----------------|
| Доступ к удалённым БД | Требует настройки подключений, отдельный модуль |
| API настроек пользователя | Нужна полноценная admin-панель |
| Генерация отчётов | Из ответа LLM — усложняет MVP |
| Go Gateway (auth, PII) | Stretched goal, Phase 4 |
| Цепочка мыслей / самопроверка | Post-MVP агентная логика |
| Мультимодальность (фото, видео) | Post-MVP |

---

## Архитектура MVP

```
Пользователь → FastAPI → Agent Engine → [RAG Search | Doc Parser] → Polza AI → Ответ
                                          ↕                    ↕
                                       ChromaDB            SQLite
```

### Диаграммы (в папке DOCS/mvp/)

| Файл | Тип | Описание |
|------|-----|----------|
| `02-system-context.md` | C4 System Context | Система в контексте окружения |
| `03-container.md` | C4 Container | Внутренние сервисы и хранилища |
| `04-sequence-chat.md` | Sequence Diagram | Жизненный цикл запроса /chat |
| `05-sequence-upload.md` | Sequence Diagram | Загрузка документа и RAG-пайплайн |
| `06-agent-loop.md` | Flowchart | Логика принятия решений агентом |
| `07-database-erd.md` | ERD | Схема базы данных SQLite |
| `08-implementation-phases.md` | Gantt Chart | Фазы реализации MVP |

---

## Технологический стек

| Компонент | Технология |
|-----------|------------|
| API Framework | Python FastAPI |
| LLM Provider | Polza AI (HTTP API) |
| Relational DB | SQLite |
| Vector Store | ChromaDB (in-process) |
| PDF Parsing | PyMuPDF / pdfplumber |
| Embeddings | sentence-transformers |
| Логирование | Python logging + JSON |
| Контейнеризация | Docker Compose |
