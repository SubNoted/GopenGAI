# AI Core — MVP: Системный контекст (C4 Level 1)

> Показывает AI Core в контексте внешних пользователей и систем.

```mermaid
C4Context
    title AI Core — System Context Diagram (MVP)

    Person(user, "Пользователь", "Отправляет запросы через REST API,\nзагружает документы")
    Person(admin, "Администратор", "Проверяет здоровье системы,\nсмотрит логи")

    System(aicore, "AI Core", "AI Agent Service: принимает запросы,\nищет по документам, вызывает LLM,\nвозвращает ответы с источниками")

    System_Ext(polza, "Polza AI", "Внешний LLM провайдер.\nГенерирует текстовые ответы\nпо augmented prompt.")

    Rel(user, aicore, "POST /chat, POST /upload\nHTTPS/JSON")
    Rel(admin, aicore, "GET /health\nМониторинг логов")
    Rel(aicore, polza, "HTTP POST\n(generate completion)")
```

## Описание

| Актор | Роль |
|-------|------|
| **Пользователь** | Основной потребитель. Отправляет вопросы, загружает документы, получает AI-ответы с указанием источников |
| **Администратор** | Следит за состоянием системы через health-check и логи |
| **AI Core** | Центральная система — AI агент с RAG, парсингом документов и интеграцией с Polza AI |
| **Polza AI** | Внешний сервис LLM. AI Core отправляет augmented prompt, получает текстовый ответ |

## Ключевые потоки

1. **Чат:** Пользователь → `POST /chat` → AI Core → Polza AI → ответ с источниками
2. **Загрузка:** Пользователь → `POST /upload` → AI Core (парсинг + эмбеддинг) → подтверждение
3. **Мониторинг:** Администратор → `GET /health` + просмотр логов
