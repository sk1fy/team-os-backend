# AGENTS.md

Руководство для ИИ-агентов, работающих в монорепозитории **бэкенда TeamOS**. Прочитай перед внесением изменений.

## Что это

Бэкенд TeamOS на Go-микросервисах, заменяющий мок-API фронтенда на настоящий бэкенд
(персистентность, реальная авторизация, фоновые процессы, пуш-уведомления). Полный дизайн — в
[teamos-go-microservices-plan.md](teamos-go-microservices-plan.md); считай его источником правды по
архитектуре, фазам и контрактам.

- **Фронтенд (источник правды для контракта):** `/Users/nikpeskov/Projects/team-os`
  (путь настраивается переменной `FRONTEND_DIR`).
- **Этот репозиторий:** `/Users/nikpeskov/Projects/team-os-backend`.

## Незыблемые правила

1. **Не ломать контракт.** REST-контракт зеркалит `src/api/index.ts` и `src/types/index.ts` во
   фронтенде: те же входы/выходы, поля `camelCase`, ID — UUID-строки.
   `contracts/openapi/teamos.yaml` — единственный источник правды для REST; хендлеры gateway
   **генерируются** из него (`oapi-codegen`) — не редактируй `services/gateway/internal/api/teamos.gen.go` руками.
2. **Все пользовательские тексты — на русском** — сообщения об ошибках, уведомления, письма. Формат
   ошибки зеркалит `ApiError` фронтенда: `{ "error": { "message": "...", "status": 400 } }`
   (см. `pkg/apierror`).
3. **Rich-text — только TipTap JSON** (`{ type: 'doc', content?: [...] }`), никогда не HTML.
4. **`companyId` — везде** — в таблицах и в claims JWT. Мультиарендность заложена сразу, хотя
   сейчас компания одна.
5. **Эволюция контракта — только аддитивная** (§3.6 плана): новые эндпоинты / необязательные поля.
   Ломающие изменения требуют новой версии пути. В CI работают `oasdiff` и `buf breaking`.

## Архитектура (см. план §2–§3)

Пять доменных сервисов + gateway (BFF). Каждый сервис владеет своей базой и является самостоятельным
деплой-юнитом.

| Сервис | Порт | Владеет |
|---|---|---|
| `gateway` | 8080 | маршрутизация, проверка JWT, CORS, rate limiting, SSE-прокси (данными не владеет) |
| `company` | 8081 | авторизация, пользователи, оргструктура, графики, распределение сделок |
| `kb` | 8082 | база знаний (ещё не реализован) |
| `tasks` | 8083 | таск-трекер (ещё не реализован) |
| `academy` | 8084 | курсы (ещё не реализован) |
| `notifications` | 8085 | уведомления + SSE (ещё не реализован) |

**Слои внутри сервиса** (зависимости направлены внутрь: `transport → domain ← storage`):
- `internal/domain` — чистые бизнес-правила, без импорта БД/транспорта. Здесь живут табличные юнит-тесты.
- `internal/storage` — sqlc-код + маппинг domain↔строки БД.
- `internal/transport` — gRPC/HTTP-хендлеры: распаковка → вызов домена → упаковка ответа/ошибки.
- `internal/application` — оркестрация, связывающая domain + storage + outbox.
- `internal/outbox` — транзакционный outbox → NATS.

**Правила межсервисной связности:** никаких импортов из чужого `internal/`, никакой общей БД, никаких
JOIN'ов через границы сервисов. Общий код — только через `pkg/*`. Межсервисные ссылки — голые
UUID-колонки **без FK**. Сервисы находят друг друга только через env-переменные — никаких
захардкоженных адресов.

## Раскладка репозитория

```
contracts/         # источник правды по интерфейсам
  openapi/         # teamos.yaml — внешний REST-контракт
  proto/           # protobuf межсервисных сервисов
  events/          # protobuf-схемы событий NATS
  gen/go/          # сгенерированный protobuf на Go (отдельный модуль)
pkg/               # общие библиотеки (отдельный модуль): apierror, auth, eventbus, httpx
services/          # по одному Go-модулю на сервис (company, gateway, ...)
deploy/            # docker-compose.yaml, инициализация postgres
docs/adr/          # architecture decision records (ADR-001..004)
```

Go-модули используют префикс `github.com/sk1fy/team-os-backend/...`. Workspace `go.work` связывает их
вместе (Go 1.25+).

## Частые команды (через Makefile)

- `make up` / `make down` — запуск/остановка дев-стека (Postgres, NATS, сервисы) через docker-compose.
  `make up` автоматически создаёт `.env` с dev-only ключами Ed25519, если его нет.
- `make migrate` — применить миграции.
- `make seed` — загрузить экспортированные фикстуры фронтенда из `SEED_DIR` (см. план §13).
- `make gen` — перегенерировать protobuf (`buf`), хендлеры OpenAPI (`oapi-codegen`) и sqlc-код.
  **Запускай после правки любого контракта или `.sql`-запроса — не редактируй сгенерированные файлы руками.**
- `make test` / `make test-race` — юнит-тесты во всех модулях (`GOWORK=off` для каждого модуля).
- `make lint` — `golangci-lint` по каждому модулю с корневым `.golangci.yaml`.
- `make fmt` — `go fmt` по каждому модулю.
- `make check-contract` — линт OpenAPI + buf, сверка с типами фронтенда, когда есть инструмент синхронизации.
- `make dev SERVICE=company` — запуск одного сервиса локально.

Тесты выполняются помодульно с `GOWORK=off`; делай так же, запуская `go test` вручную в папке сервиса.

## Соглашения

- **Доступ к БД:** `pgx/v5` + `sqlc`. Пиши SQL в `internal/storage/queries/*.sql`, затем `make gen`.
- **Миграции:** `golang-migrate`, версионный SQL в `migrations/` каждого сервиса. Правило
  expand → migrate → contract, чтобы rolling update двух версий мог работать одновременно (план §3.6).
- **События:** subjects версионируются `teamos.<service>.<entity>.<action>.v1`. Закрепляй схему
  каждого события в `contracts/events/*.proto`. Публикуй через outbox, потребляй идемпотентно.
- **Авторизация:** JWT access (15 мин, EdDSA) + refresh (httpOnly-cookie, с ротацией). Claims несут
  `pos`/`dep`, чтобы `kb`/`academy` проверяли доступы без похода в `company` (план §7). Пароли: argon2id.
- **Health:** каждый сервис отдаёт `GET /healthz` и `GET /readyz`; graceful shutdown по SIGTERM.
- **Логи:** `slog` в JSON; проброс trace OpenTelemetry от gateway через gRPC и NATS.

## При портировании доменной логики

Доменные правила берутся из `src/lib/` фронтенда (`orgTree.ts`, `userGuards.ts`, `inviteRules.ts`,
`schedule.ts`, `taskBoard.ts`, `dealDistribution.ts`). Портируй **и логику, и её юнит-тесты** в
соответствующий пакет `internal/domain/*` (план §11).
