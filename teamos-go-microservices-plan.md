# TeamOS — детальный план микросервисного бэкенда на Go

Дата: 2026-07-06. Обновлено: 2026-07-12 — синхронизация с изменениями фронтенда после 06.07:

- новый модуль **«Распределение сделок»** (`distributionApi`, `src/lib/dealDistribution.ts`, страницы `/distribution`) — §2, §6.2, §8.1.1, §11, §16;
- `orgApi.renameDepartment` → **`updateDepartment`** (`name?`, `headUserId?`, `valuableFinalProduct?`); `createDepartment` принимает `headUserId` и `valuableFinalProduct` (ЦКП отдела);
- новые поля `User`: `birthDate`, `hiredAt`, `vacationAllowance`; правило **«не более одной должности»** (`validatePositionAssignment` в `userGuards.ts`) при сохранении массива `positionIds` в контракте;
- `scheduleApi.saveSchedule` — upsert шаблона графика сотрудника;
- `orgApi.updatePosition` дополнительно принимает `departmentId` (наряду с `movePosition`).

## 0. Расположение проектов

| Что | Путь |
|---|---|
| **Фронтенд (существующий SPA)** | `/Users/nikpeskov/Projects/team-os` |
| **Бэкенд (новый монорепо, отдельная папка)** | `/Users/nikpeskov/Projects/team-os-backend` *(рекомендуемый путь; может лежать где угодно — все ссылки на фронтенд в скриптах и доках бэкенда задаются переменной `FRONTEND_DIR`, по умолчанию `/Users/nikpeskov/Projects/team-os`)* |

Файлы фронтенда, которые являются источником правды для контракта бэкенда:

- `/Users/nikpeskov/Projects/team-os/src/api/index.ts` — сигнатуры всех API-функций (`authApi`, `orgApi`, `kbApi`, `tasksApi`, `academyApi`, `notificationsApi`, `scheduleApi`, `distributionApi`). По правилам проекта при появлении реального бэкенда меняются только реализации, но не сигнатуры.
- `/Users/nikpeskov/Projects/team-os/src/types/index.ts` — типы всех сущностей (контракт данных).
- `/Users/nikpeskov/Projects/team-os/src/api/client.ts` — `mockRequest`, `ApiError`, `notFound()`: формат ошибок, который бэкенд обязан воспроизвести.
- `/Users/nikpeskov/Projects/team-os/src/api/fixtures.ts` — фикстуры → seed-данные дев-окружения бэкенда.
- `/Users/nikpeskov/Projects/team-os/src/api/queryClient.ts` — настройки TanStack Query (`retry: 2`, `networkMode: 'always'` — второе убирается при переходе на реальный API, о чём там прямо написано в комментарии).
- `/Users/nikpeskov/Projects/team-os/src/lib/` — доменные правила (`orgTree.ts`, `inviteRules.ts`, `userGuards.ts`, `schedule.ts`, `taskBoard.ts`, `dealDistribution.ts`) с юнит-тестами — портируются в Go.

---

## 1. Цели и ограничения

1. Заменить мок-API (задержка 300–500 мс, 5 % случайных ошибок, мутируемые массивы в памяти) на настоящий бэкенд: персистентность, реальная авторизация, фоновые процессы, пуш-уведомления.
2. **Не сломать контракт**: те же входы/выходы, что у функций в `src/api/index.ts`; те же типы, что в `src/types/index.ts`; ошибки — русскими сообщениями (аналог `ApiError(message, status)`).
3. **Лёгкая расширяемость, обновляемость и сопровождаемость — главное нефункциональное требование.** Любой сервис можно доработать, пересобрать и развернуть независимо от остальных — в том числе вынести на отдельный сервер, когда на него вырастет нагрузка, — без остановки и пересборки всей системы. Все архитектурные решения в этом плане проверяются на соответствие этому требованию (см. §3).
4. Rich-text хранится и передаётся только как TipTap JSON (`RichTextContent: { type: 'doc', content?: [...] }`), никогда как HTML.
5. Все пользовательские тексты (ошибки, уведомления, письма) — на русском.
6. Мультиарендность на вырост: сейчас одна компания, но `companyId` закладывается во все таблицы и claims сразу — переделывать потом дороже.

---

## 2. Декомпозиция: 5 доменных сервисов + gateway

Дробить по сервису на каждый из 8 API-модулей — избыточно для команды 1–2 человек: пользователи, авторизация и оргструктура слишком связаны (доступы БЗ ссылаются на отделы/должности, задачи — на людей, курсы — на должности, график и распределение сделок — на сотрудников).

| Сервис | Порт (dev) | Модули фронтенда | Владеет данными |
|---|---|---|---|
| `gateway` (BFF) | 8080 | — | ничем: маршрутизация, проверка JWT, CORS, rate limiting, SSE-прокси |
| `company` | 8081 | `authApi`, `orgApi`, `scheduleApi`, `distributionApi` | компания, пользователи, учётки/сессии, приглашения, отделы, должности, графики (`UserSchedule`, `ShiftException`), группы и события распределения сделок |
| `kb` | 8082 | `kbApi` | разделы БЗ, статьи, версии, отметки «Ознакомлен», полнотекстовый поиск |
| `tasks` | 8083 | `tasksApi` | доски, колонки, задачи, комментарии, метки, повторения |
| `academy` | 8084 | `academyApi` | курсы, разделы курсов, уроки, тесты, назначения, прогресс |
| `notifications` | 8085 | `notificationsApi` | уведомления, счётчик непрочитанных, SSE-доставка |

Обоснование границ:

- **`company` объединяет auth + org + schedule.** Пользователь — одна сущность (учётка + профиль + должности + график); разрыв на identity/org на старте порождает распределённые транзакции без выгоды. Внутри сервиса границы всё равно соблюдаются на уровне Go-пакетов (`internal/domain/auth`, `internal/domain/org`, `internal/domain/schedule`, `internal/domain/distribution`) — если сервис разрастётся, первыми выделяются авторизация и распределение сделок (в будущий `crm`), и это будет механическое извлечение.
- **`kb`, `tasks`, `academy`** — классические bounded contexts с собственными агрегатами и слабой связностью (ссылки на чужие сущности только по ID).
- **`notifications`** — чистый консьюмер событий, идеальный кандидат на отдельный сервис.
- **Распределение сделок живёт в `company`** как отдельный доменный пакет (`internal/domain/distribution`): данные модуля — только ссылки на пользователей (`memberIds`) плюс собственные группы/события, а группа по сути — орг-объект «очередь менеджеров». Это первый кирпич будущего CRM: когда появятся сделки/воронки, `distribution` извлекается в сервис `crm` (§3.8) механически — как и auth. `simulateDeal` в моке — демо-кнопка; в реальном бэкенде источником сделок станет вебхук CRM, и эндпоинт сохраняется как ручной триггер для дев/демо.
- **Фаза 2:** `files` (8086) — загрузка вложений/аватаров/логотипов в S3-совместимое хранилище (MinIO локально). Типы `Attachment`, `avatarUrl`, `logoUrl` сейчас держат только URL, поэтому это откладывается без ломки контракта.

---

## 3. Расширяемость, обновление и независимое масштабирование

Это раздел-«конституция»: правила, которые делают возможным сценарий «выделили сервис → доработали → развернули на отдельном сервере из-за нагрузки». Каждое правило — конкретное инженерное решение, а не пожелание.

### 3.1. Каждый сервис — самостоятельный деплой-юнит

- Свой `Dockerfile`, свои миграции, свой набор env-переменных, свой version-тег образа (`teamos/kb:v1.4.2`, semver).
- **CI собирает и публикует образ только изменившегося сервиса** (matrix по `services/*`, триггер по путям). Обновление `kb` не требует пересборки и передеплоя `tasks`.
- Общий код — только через `pkg/*` (versioned внутри монорепо); никаких «shared database», «shared models между сервисами» и import'ов из чужого `internal/`. Компилятор Go физически запрещает импорт чужого `internal/` — этим и пользуемся.

### 3.2. Сервисы stateless — масштабирование горизонтально

- Всё состояние — в Postgres, NATS и (позже) Redis/S3. В памяти процесса нет ничего, что нельзя потерять: вторая реплика сервиса поднимается рядом без координации.
- Единственное «липкое» место — SSE-соединения в `notifications`: hub раздаёт события, полученные из NATS, поэтому реплики равноправны (каждая слушает шину, пользователь может быть подключён к любой). Балансировка — обычный round-robin, sticky sessions не нужны.
- Воркеры (river) координируются через Postgres-локи — несколько реплик сервиса не выполнят одну джобу дважды.

### 3.3. Адресация только через конфигурацию — переезд сервиса без правки кода

- Сервисы находят друг друга исключительно по env-переменным: `GATEWAY_KB_ADDR=kb:8082`, `ACADEMY_KB_GRPC_ADDR=kb:9082`, `COMPANY_NATS_URL=nats://nats:4222`. В коде нет ни одного захардкоженного адреса.
- Перенос сервиса на отдельный сервер = смена значения переменной у его потребителей (или DNS-записи). Кода это не касается.
- Событийные связи ещё свободнее: издатель и подписчик знают только адрес NATS, друг о друге — ничего. Сервис, живущий на другом континенте, подписывается на те же subjects.

### 3.4. База данных на сервис — данные переезжают вместе с сервисом

- В чужую базу не ходит никто (ни JOIN'ов, ни view поверх чужих таблиц) — это правило и делает базу переносимой.
- На старте все БД живут в одном Postgres-кластере (дёшево), но как **отдельные базы** (`teamos_company`, `teamos_kb`, …), а не схемы в одной базе — чтобы перенос был механическим.
- Рецепт переноса БД сервиса на выделенный кластер: логическая репликация Postgres (publication/subscription) → догнать → короткое окно записи (сервис в maintenance на секунды) → переключить `KB_DB_URL` → удалить publication. Без даунтайма чтения.

### 3.5. Рецепт «сервис под нагрузкой — на отдельный сервер» (на примере `kb`)

1. На новом сервере: docker/env для `kb` (`KB_DB_URL`, `KB_NATS_URL` — указывают пока на старую инфраструктуру), поднять реплику `kb`.
2. Добавить адрес новой реплики в upstream gateway (`GATEWAY_KB_ADDR` — список или LB/DNS) и в `ACADEMY_KB_GRPC_ADDR`. Трафик течёт на обе копии.
3. При необходимости перенести и его базу (§3.4) и/или поднять локальный NATS-leaf node (JetStream leafnodes — штатный механизм).
4. Погасить старую копию. Остальные пять сервисов не пересобирались и не перезапускались (gateway — только перечитал конфиг/DNS).

Тот же рецепт масштабирует сервис до N реплик — п. 4 просто не выполняется.

### 3.6. Обратная совместимость контрактов — сервисы обновляются в любом порядке

Независимый деплой возможен, только если сосед со старой версией не ломается. Правила эволюции (проверяются в CI):

- **REST (OpenAPI)**: только аддитивные изменения — новые эндпоинты, новые необязательные поля. Удаление/переименование поля = новая версия пути (`/api/v2/...`), старая живёт минимум два релиза. Diff-линтер спеки (`oasdiff`) в CI падает на breaking change.
- **gRPC (protobuf)**: поля не удаляются и не перенумеровываются (`reserved`), только добавляются optional; `buf breaking` в CI.
- **События**: версия в subject (`teamos.kb.article.updated.v1`); изменение схемы = новый subject `.v2`, издатель публикует оба, пока жив хоть один подписчик v1; каталог `contracts/events/catalog.md` фиксирует, кто на чём сидит.
- **БД-миграции**: только expand → migrate → contract (сначала добавить колонку, потом переключить код, потом удалить старую) — чтобы rolling update двух версий сервиса мог работать одновременно.

### 3.7. Обновление без остановки

- `GET /healthz` + `GET /readyz` у каждого сервиса; rolling update: новая реплика становится ready → старая получает SIGTERM → graceful shutdown (перестать брать новые запросы, дообработать текущие, дозакрыть SSE с событием `retry`, докоммитить NATS-ack) → выход. Тайм-аут drain — 30 с.
- Фронтенд не замечает: `retry: 2` в TanStack Query покрывает секундные окна переключения, SSE-клиент переподключается сам (`EventSource` reconnection).

### 3.8. Добавление нового сервиса/модуля — по шаблону

Появится новый домен (например, CRM — тип `TaskSource` и модуль распределения сделок во фронтенде уже намекают на воронки/сделки; при выделении `crm` домен `distribution` переезжает из `company` первым, §8.1.1):

1. `services/crm/` копированием скелета (cmd, domain, storage, transport, outbox, migrations) — заготовить `tools/new-service` генератор.
2. Своя БД `teamos_crm`, свои миграции, свой образ, регистрация в compose.
3. Маршруты в `contracts/openapi/teamos.yaml` + генерация хендлеров gateway.
4. События — в каталог; подписки на существующие subjects (например, `org.user.*`) — без изменения их издателей.
5. Модуль фронтенда получает свой `crmApi` в `/Users/nikpeskov/Projects/team-os/src/api/index.ts` и флаг `VITE_API_MODE_CRM`.

Ни один существующий сервис при этом не редактируется — только gateway-спека и каталог событий.

### 3.9. Что намеренно НЕ делаем (чтобы поддержка оставалась лёгкой)

- Никакого service mesh, Consul, Kubernetes-операторов на старте — env-адресация плюс compose в разработке и systemd-юниты на проде (§15.1) покрывают потребности до десятков инстансов.
- Никаких распределённых транзакций/2PC — только outbox + идемпотентные консьюмеры.
- Никакого шаринга доменных Go-структур между сервисами — дублирование DTO дешевле связывания.

---

## 4. Технологический стек

| Слой | Выбор | Почему |
|---|---|---|
| Язык | Go 1.24+ | требование задачи; статическая типизация зеркалит TS-контракт |
| HTTP (внешний) | `net/http` + `chi` v5 | стандартно, без магии, middleware-модель |
| RPC (межсервисный) | gRPC (`google.golang.org/grpc` + protobuf) | типизированный контракт, кодогенерация; альтернатива — connect-go, если захочется дебажить curl'ом |
| БД | PostgreSQL 16, **база на сервис** | один кластер на старте, но отдельные базы — перенос вместе с сервисом механический (§3.4) |
| Доступ к БД | `pgx/v5` + `sqlc` | SQL-first, кодогенерация типобезопасных запросов |
| Миграции | `golang-migrate` | версионные SQL-файлы в `migrations/` каждого сервиса; правило expand→contract (§3.6) |
| Шина событий | NATS JetStream | легче Kafka на порядки, персистентные стримы, leafnodes для геораспределения (§3.5) |
| Кэш (фаза 2) | Redis | счётчик непрочитанных, кэш прав |
| Внешний контракт | OpenAPI 3.1 + `oapi-codegen` | единственный источник правды REST; из него же — TS-клиент для фронтенда; `oasdiff` против breaking changes |
| Фоновые задачи | `riverqueue/river` (очередь поверх Postgres) | без отдельного брокера задач; cron-джобы там же; мультиреплики без дублей (§3.2) |
| Наблюдаемость | OpenTelemetry SDK → Prometheus + Grafana + Loki + Tempo; логи `slog` (JSON) | сквозной trace-id от gateway через gRPC и NATS |
| Локальная разработка | **Docker Compose** | один `make up` поднимает всё; Docker — инструмент разработки и тестов, не прода (§15.1) |
| Прод | **нативное развёртывание**: Go-бинарники под systemd, штатно установленные Postgres и NATS | Go собирается в один статический бинарник — контейнер в проде ничего не добавляет; health-checks и graceful shutdown те же (§3.7); Kubernetes — только при реальном росте |
| CI | GitHub Actions | matrix по сервисам, сборка только изменившегося (§3.1); lint, test, contract-тесты, `buf breaking`, `oasdiff` |

---

## 5. Структура репозитория `/Users/nikpeskov/Projects/team-os-backend`

```
team-os-backend/
├── go.work                          # workspace: services/* + pkg
├── Makefile                         # up, down, migrate, seed, test, lint, gen
├── .golangci.yaml
├── contracts/
│   ├── openapi/
│   │   └── teamos.yaml              # внешний REST-контракт (источник правды)
│   ├── proto/
│   │   ├── company/v1/company.proto # межсервисные gRPC-интерфейсы
│   │   ├── kb/v1/kb.proto
│   │   └── ...
│   └── events/
│       ├── catalog.md               # каталог событий: имя, издатель, схема, потребители
│       └── *.proto                  # схемы полезной нагрузки событий
├── services/
│   ├── gateway/
│   │   ├── cmd/gateway/main.go
│   │   └── internal/
│   │       ├── transport/           # хендлеры из oapi-codegen, маппинг на gRPC-клиенты
│   │       ├── authmw/              # проверка JWT, извлечение claims
│   │       └── sse/                 # прокси стрима уведомлений
│   ├── company/
│   │   ├── cmd/company/main.go
│   │   ├── cmd/seed/main.go         # заливка seed-данных (см. §13)
│   │   ├── internal/
│   │   │   ├── domain/
│   │   │   │   ├── auth/            # сессии, пароли, JWT, инвайты
│   │   │   │   ├── org/             # orgtree.go (порт orgTree.ts), userguards.go, inviterules.go
│   │   │   │   ├── schedule/        # порт schedule.ts (week/cycle-шаблоны, исключения)
│   │   │   │   └── distribution/    # порт dealDistribution.ts (группы, выбор исполнителя сделки)
│   │   │   ├── storage/             # sqlc-репозитории
│   │   │   ├── transport/           # gRPC-сервер + внутренние HTTP-ручки (health)
│   │   │   └── outbox/              # transactional outbox → NATS
│   │   └── migrations/
│   ├── kb/          (та же структура: domain/ storage/ transport/ outbox/ consumers/)
│   ├── tasks/       (+ internal/workers/recurrence.go, duedates.go)
│   ├── academy/     (+ internal/consumers/kbarticles.go — репликация live-уроков)
│   └── notifications/ (+ internal/consumers/*.go, internal/sse/hub.go)
├── pkg/                             # общие библиотеки (импортируются сервисами)
│   ├── apierror/                    # ApiError-совместимые ошибки: NotFound("Статья") → 404 «Статья не найдена»
│   ├── auth/                        # парсинг/валидация JWT, типы claims
│   ├── eventbus/                    # обёртка NATS: publish из outbox, идемпотентный consume
│   ├── richtext/                    # тип RichTextContent, извлечение plain-text из TipTap JSON
│   └── httpx/                       # middleware: request-id, логирование, recover, otel
├── tools/
│   ├── sync-contract/               # сверка teamos.yaml с типами фронтенда (см. §6.3)
│   └── new-service/                 # генератор скелета нового сервиса (§3.8)
├── deploy/
│   ├── docker-compose.yaml          # postgres, nats, все сервисы, minio (фаза 2)
│   ├── docker-compose.observability.yaml   # prometheus, grafana, loki, tempo
│   └── k8s/                         # заготовка на будущее
└── docs/
    ├── adr/                         # architecture decision records
    └── runbook.md                   # включая рецепты §3.4–3.5 (перенос сервиса/БД)
```

Правила слоёв внутри сервиса (зеркало того, как `src/lib` во фронтенде не знает про React):

- `internal/domain` — чистые бизнес-правила, без импорта БД и транспорта; только здесь живёт логика, только здесь табличные юнит-тесты.
- `internal/storage` — sqlc-код + маппинг domain ↔ строки БД.
- `internal/transport` — gRPC/HTTP-хендлеры: распаковка запроса → вызов домена → упаковка ответа/ошибки.
- Зависимости направлены внутрь: transport → domain ← storage.

---

## 6. Внешний REST-контракт (gateway)

### 6.1. Общие соглашения

- Префикс `/api/v1`, JSON, `camelCase`-поля — ровно как в `src/types/index.ts`.
- **ID** — UUID-строки (фронтенд уже генерирует `crypto.randomUUID()`; тип `ID = string` не меняется). Серверная генерация ID — на бэкенде, фронтовые временные ID игнорируются.
- Даты-время — ISO-8601 строки (`ISODate`); даты графика — `YYYY-MM-DD`, месяц — `YYYY-MM`, время смен — `HH:MM` (как в `WeekTemplate`/`ShiftException`).
- **Формат ошибки** — зеркало `ApiError` из `client.ts`:

```json
{ "error": { "message": "Нельзя удалить отдел с вложенными отделами или должностями. Сначала переместите их.", "status": 400 } }
```

  Коды: 400 — доменные запреты и валидация, 401 — нет/просрочен токен, 403 — нет прав, 404 — `notFound('Сущность')` («Сущность не найдена»), 409 — конфликт версий (см. §8.2 kb), 5xx — внутренние. Сообщения — по-русски, фронтенд показывает их как есть.
- Health: каждый сервис отдаёт `GET /healthz` (liveness) и `GET /readyz` (готовность: БД + NATS).

### 6.2. Полная карта эндпоинтов (все функции `src/api/index.ts`)

**Auth / Company (`authApi` → сервис `company`)**

| Функция фронтенда | Метод и путь |
|---|---|
| `getCurrentUser()` | `GET /api/v1/auth/me` |
| `updateCurrentUser({firstName?, lastName?, phone?, avatarUrl?})` | `PATCH /api/v1/auth/me` |
| `getCompany()` | `GET /api/v1/company` |
| `updateCompany({name?, logoUrl?})` | `PATCH /api/v1/company` |
| `getInviteByToken(token)` | `GET /api/v1/auth/invites/{token}` |
| *новое: логин* | `POST /api/v1/auth/login` `{email, password}` → set-cookie refresh + `{accessToken, user}` |
| *новое: обновление токена* | `POST /api/v1/auth/refresh` (по httpOnly-cookie, с ротацией) |
| *новое: выход* | `POST /api/v1/auth/logout` |
| *новое: принятие инвайта* | `POST /api/v1/auth/invites/{token}/accept` `{firstName, lastName, password}` |
| *новое: бутстрап владельца* | `POST /api/v1/auth/register` `{companyName, email, password, firstName, lastName}` |

**Оргструктура (`orgApi` → сервис `company`)**

| Функция | Метод и путь |
|---|---|
| `getDepartments()` | `GET /api/v1/org/departments` |
| `createDepartment({name, parentId, headUserId?, valuableFinalProduct?})` | `POST /api/v1/org/departments` |
| `updateDepartment({id, name?, headUserId?, valuableFinalProduct?})` | `PATCH /api/v1/org/departments/{id}` *(бывший `renameDepartment`; `headUserId: null` снимает руководителя, `valuableFinalProduct: null` очищает ЦКП)* |
| `deleteDepartment(id)` | `DELETE /api/v1/org/departments/{id}` |
| `moveDepartment({id, parentId})` | `POST /api/v1/org/departments/{id}/move` |
| `getPositions()` / `getPosition(id)` | `GET /api/v1/org/positions`, `GET /api/v1/org/positions/{id}` |
| `createPosition(...)` / `updatePosition(...)` | `POST /api/v1/org/positions`, `PATCH /api/v1/org/positions/{id}` *(`updatePosition` принимает и `departmentId` — перенос должности возможен как через PATCH, так и через `/move`)* |
| `deletePosition(id)` | `DELETE /api/v1/org/positions/{id}` *(снятие должности с сотрудников — в той же транзакции, как в моке)* |
| `movePosition({id, departmentId})` | `POST /api/v1/org/positions/{id}/move` |
| `getUsers()` / `getUser(id)` | `GET /api/v1/org/users`, `GET /api/v1/org/users/{id}` |
| `createUser(...)` | `POST /api/v1/org/users` |
| `updateUser(...)` | `PATCH /api/v1/org/users/{id}` *(серверные гварды из `userGuards.ts`, включая `validatePositionAssignment` — не более одной должности; новые поля `birthDate`, `hiredAt`, `vacationAllowance`)* |
| `getInvites()` | `GET /api/v1/org/invites` *(сортировка по `createdAt desc` — на сервере)* |
| `inviteUser({email?, role, positionId?, departmentId?})` | `POST /api/v1/org/invites` *(email не задан = приглашение по ссылке)* |
| `resendInvite(id)` | `POST /api/v1/org/invites/{id}/resend` |
| `revokeInvite(id)` | `POST /api/v1/org/invites/{id}/revoke` |

**База знаний (`kbApi` → сервис `kb`)**

| Функция | Метод и путь |
|---|---|
| `getSections()` | `GET /api/v1/kb/sections` |
| `createSection({name, parentId, access?})` | `POST /api/v1/kb/sections` |
| `updateSection({id, name?, access?})` | `PATCH /api/v1/kb/sections/{id}` |
| `deleteSection(id)` | `DELETE /api/v1/kb/sections/{id}` |
| `getArticles(sectionId?)` | `GET /api/v1/kb/articles?sectionId=` |
| `getArticle(id)` | `GET /api/v1/kb/articles/{id}` |
| `createArticle(...)` / `updateArticle(...)` | `POST /api/v1/kb/articles`, `PATCH /api/v1/kb/articles/{id}` |
| `rollbackArticle({articleId, versionId})` | `POST /api/v1/kb/articles/{articleId}/rollback` `{versionId}` |
| `getArticleVersions(articleId)` | `GET /api/v1/kb/articles/{id}/versions` |
| `getAcknowledgements(articleId)` | `GET /api/v1/kb/articles/{id}/acknowledgements` |
| `acknowledgeArticle(articleId)` | `POST /api/v1/kb/articles/{id}/acknowledge` |
| `searchArticles(query)` | `GET /api/v1/kb/articles/search?q=` |

**Таск-трекер (`tasksApi` → сервис `tasks`)**

| Функция | Метод и путь |
|---|---|
| `getBoards()` | `GET /api/v1/tasks/boards` |
| `getColumns(boardId)` | `GET /api/v1/tasks/boards/{boardId}/columns` |
| `createColumn({boardId, name, color?})` | `POST /api/v1/tasks/boards/{boardId}/columns` |
| `updateColumn({id, name?, color?})` | `PATCH /api/v1/tasks/columns/{id}` |
| `getTasks(boardId?)` | `GET /api/v1/tasks?boardId=` |
| `getTask(id)` | `GET /api/v1/tasks/{id}` |
| `createTask({boardId, columnId, title, priority?})` | `POST /api/v1/tasks` |
| `updateTask({id, ...})` | `PATCH /api/v1/tasks/{id}` |
| `moveTask({taskId, columnId, order})` | `POST /api/v1/tasks/{taskId}/move` |
| `getComments(taskId)` / `addComment(...)` | `GET/POST /api/v1/tasks/{taskId}/comments` |
| `getLabels()` | `GET /api/v1/tasks/labels` |

**Академия (`academyApi` → сервис `academy`)**

| Функция | Метод и путь |
|---|---|
| `getCourses()` / `getCourse(id)` | `GET /api/v1/academy/courses`, `GET /api/v1/academy/courses/{id}` |
| `createCourse(...)` / `updateCourse(...)` / `deleteCourse(id)` | `POST /api/v1/academy/courses`, `PATCH/DELETE /api/v1/academy/courses/{id}` |
| `createCourseFromKb({title, mode, sectionIds, articleIds, ...})` | `POST /api/v1/academy/courses/from-kb` |
| `getCourseSections(courseId)` / `createCourseSection(...)` | `GET/POST /api/v1/academy/courses/{courseId}/sections` |
| `updateCourseSection(...)` / `deleteCourseSection(id)` | `PATCH/DELETE /api/v1/academy/sections/{id}` |
| `getLessons(courseId?)` | `GET /api/v1/academy/lessons?courseId=` *(link-уроки уже с актуальным контентом — см. §10.2)* |
| `createLesson(...)` / `updateLesson(...)` / `deleteLesson(id)` | `POST /api/v1/academy/lessons`, `PATCH/DELETE /api/v1/academy/lessons/{id}` |
| `moveLesson({id, sectionId, order})` | `POST /api/v1/academy/lessons/{id}/move` |
| `getQuizzes(lessonId?)` / `upsertQuiz(...)` | `GET /api/v1/academy/quizzes?lessonId=`, `PUT /api/v1/academy/quizzes` |
| `getAssignments()` / `assignCourse(...)` | `GET/POST /api/v1/academy/assignments` |
| `getProgress(courseId?)` | `GET /api/v1/academy/progress?courseId=` |
| `markLessonComplete({courseId, lessonId, userId?})` | `POST /api/v1/academy/progress/lessons/{lessonId}/complete` |

**Уведомления (`notificationsApi` → сервис `notifications`)**

| Функция | Метод и путь |
|---|---|
| `getNotifications()` | `GET /api/v1/notifications` |
| `getUnreadCount()` | `GET /api/v1/notifications/unread-count` |
| `markRead(id)` | `POST /api/v1/notifications/{id}/read` |
| `markAllRead()` | `POST /api/v1/notifications/read-all` |
| *новое: живая доставка* | `GET /api/v1/notifications/stream` (SSE) |

**График работы (`scheduleApi` → сервис `company`)**

| Функция | Метод и путь |
|---|---|
| `getSchedules()` | `GET /api/v1/schedule` |
| `saveSchedule(schedule)` | `PUT /api/v1/schedule/{userId}` — upsert шаблона (`WeekTemplate` \| `CycleTemplate`) сотрудника |
| `getExceptions(month)` | `GET /api/v1/schedule/exceptions?month=YYYY-MM` |
| `saveExceptions(inputs[])` | `PUT /api/v1/schedule/exceptions` — батч, upsert по `(userId, date)`, одна транзакция |

**Распределение сделок (`distributionApi` → сервис `company`)**

| Функция | Метод и путь |
|---|---|
| `getGroups()` | `GET /api/v1/distribution/groups` |
| `createGroup({name, description?, memberIds})` | `POST /api/v1/distribution/groups` *(дефолты как в моке: `active: true`, `algorithm: 'round_robin'`, `dealLimit: 10`, `unclaimedMinutes: 15`; `memberIds` дедуплицируются, минимум один участник)* |
| `updateGroup({id, ...})` | `PATCH /api/v1/distribution/groups/{id}` *(смена `memberIds` вычищает из `disabledMemberIds` выбывших; `dealLimit`/`unclaimedMinutes` — минимум 1)* |
| `deleteGroup(id)` | `DELETE /api/v1/distribution/groups/{id}` *(каскадно удаляет события группы)* |
| `getEvents(groupId)` | `GET /api/v1/distribution/groups/{groupId}/events` *(сортировка `createdAt desc` — на сервере)* |
| `simulateDeal(groupId)` | `POST /api/v1/distribution/groups/{groupId}/simulate` *(дев/демо-триггер; выбор исполнителя — `pickDistributionMember`, §11; 400 «Группа приостановлена» для неактивной)* |
| `resetEvents(groupId)` | `DELETE /api/v1/distribution/groups/{groupId}/events` |

### 6.3. Синхронизация контракта с фронтендом

Так как репозитории лежат в разных папках, дрейф контракта — главный операционный риск. Три рубежа защиты:

1. `contracts/openapi/teamos.yaml` — единственный источник правды; серверные хендлеры gateway генерируются из него (`oapi-codegen`), руками не пишутся.
2. `tools/sync-contract` — Go-утилита в бэкенд-репо: генерирует TS-типы из OpenAPI (`openapi-typescript`) и сравнивает с `/Users/nikpeskov/Projects/team-os/src/types/index.ts` (путь берётся из `FRONTEND_DIR`). Запуск: `make check-contract FRONTEND_DIR=/Users/nikpeskov/Projects/team-os`. В CI фронтенд-репо подключается как read-only checkout.
3. Contract-тесты: в CI ответы gateway валидируются против спеки на живом docker-compose-стенде; `oasdiff` ловит breaking changes между версиями спеки (§3.6).

---

## 7. Авторизация и доступы

### 7.1. Замена фиктивного `CURRENT_USER_ID`

Сейчас `CURRENT_USER_ID` в фикстурах всегда владелец, логина нет. Становится:

- Пароли — argon2id; таблица `credentials` отдельно от `users` (не все пользователи имеют пароль — статус `invited`).
- **JWT access (15 мин, подпись EdDSA) + refresh (30 дней, httpOnly Secure cookie, ротация с детекцией повторного использования)**. Refresh-токены — в таблице `sessions` (хэш), чтобы уметь отзывать.
- Клеймы access-токена:

```json
{
  "sub": "<userId>", "cid": "<companyId>", "role": "owner|admin|employee|partner",
  "pos": ["<positionId>", ...], "dep": ["<departmentId>", ...],
  "exp": 1780000000, "iat": 1779999100
}
```

  `pos`/`dep` в токене — ключевое решение: `kb` и `academy` проверяют доступы **без синхронного похода в `company`** на каждый запрос (прямо служит цели независимого масштабирования из §3: сервисы не образуют цепочку синхронных вызовов). Устаревание при смене должностей ограничено TTL токена (15 мин); критичные операции (деактивация) дополнительно бьют по таблице `sessions`.
- Поток инвайта: `orgApi.inviteUser` создаёт `invite` (+ пользователя в статусе `invited`, как в моке) и событие `org.invite.created` → сервис `notifications`/почта; приглашённый открывает `/auth/invite/{token}` → `GET /auth/invites/{token}` → `POST .../accept` с паролем → статус `active`, инвайт `accepted`.
- Гварды из `/Users/nikpeskov/Projects/team-os/src/lib/userGuards.ts` (защита владельца, запрет самодеактивации и т.д.) — серверная валидация в `company/internal/domain/org`, тесты портируются из `userGuards.test.ts`.

### 7.2. Ролевая модель (middleware gateway + повторная проверка в сервисах)

| Операция | Кому доступна |
|---|---|
| Изменение компании, ролей, приглашения, оргструктура | `owner`, `admin` |
| Создание/редактирование статей, курсов, досок | `owner`, `admin` (уточнить по фактическому UI) |
| Чтение БЗ/курсов | все `active` с учётом `AccessSettings` |
| `partner` | только назначенные курсы (external-назначения) и свои задачи |

Gateway отсекает грубые случаи (нет токена, не та роль), сервисы повторяют доменные проверки — защита от вызовов в обход шлюза.

### 7.3. Алгоритм проверки `AccessSettings` (kb, переиспользуется в academy)

Тип: `{ scope: 'company' | 'custom', departmentIds, positionIds, userIds }`; по контракту «доступ наследуется дочерними разделами, если scope не переопределён».

```
effectiveAccess(section):
  если section.access.scope == 'custom' → section.access
  иначе → подняться к родителю; у корня scope 'company' = доступ всем сотрудникам компании
allowed(user, access):
  access.scope == 'company' → true (для role != partner)
  иначе → userId ∈ userIds  ИЛИ  pos ∩ positionIds ≠ ∅  ИЛИ  dep ∩ departmentIds ≠ ∅
```

`dep` пользователя вычисляется в `company` при выпуске токена как отделы его должностей **плюс все родительские отделы** (materialized path/recursive CTE) — так «доступ отделу» покрывает вложенные подотделы. Решение зафиксировать в ADR.

---

## 8. Схемы данных по сервисам (эскизы DDL)

Общее: `id uuid primary key default gen_random_uuid()`, `company_id uuid not null` везде, `created_at/updated_at timestamptz`, `jsonb` для TipTap. Межсервисные ссылки — просто uuid-колонки **без FK**.

### 8.1. `company`

```sql
create table companies (id uuid pk, name text, logo_url text, owner_id uuid, created_at timestamptz);
create table users (
  id uuid pk, company_id uuid, email citext unique, first_name text, last_name text,
  phone text, avatar_url text,
  role text check (role in ('owner','admin','employee','partner')),
  status text check (status in ('active','invited','deactivated')),
  birth_date date,                                  -- 🎂 в графике, поздравления
  hired_at date,                                    -- стаж, годовщины 🎉
  vacation_allowance smallint,                      -- норма отпуска, дней/год
  created_at timestamptz
);
create table credentials (user_id uuid pk references users, password_hash text, updated_at timestamptz);
create table sessions (id uuid pk, user_id uuid, refresh_hash text, expires_at timestamptz, rotated_from uuid);
create table invites (
  id uuid pk, company_id uuid, email citext, token text unique, role text,
  position_id uuid, department_id uuid, invited_by_id uuid,
  status text check (status in ('pending','accepted','expired')), created_at timestamptz
);
create table departments (
  id uuid pk, company_id uuid, name text,
  parent_id uuid references departments,          -- null = корень
  head_user_id uuid,
  valuable_final_product text,                    -- ЦКП отдела (показывается на оргсхеме)
  "order" int
);
create table positions (
  id uuid pk, company_id uuid, name text, department_id uuid references departments,
  level smallint check (level between 0 and 4), description text,
  article_ids uuid[] default '{}',                -- ссылки в kb, без FK
  required_course_ids uuid[] default '{}'         -- ссылки в academy, без FK
);
create table user_positions (
  user_id uuid, position_id uuid, primary key (user_id, position_id),
  unique (user_id)          -- правило «не более одной должности» (validatePositionAssignment);
);                          -- таблица-связка и массив positionIds в API сохраняются для совместимости
create table user_schedules (user_id uuid pk, template jsonb);        -- WeekTemplate | CycleTemplate
create table shift_exceptions (
  id uuid pk, user_id uuid, date date,
  type text check (type in ('work','off','vacation','sick','trip')),
  start_time time, end_time time, note text,
  unique (user_id, date)                          -- контракт saveExceptions: upsert по (userId, date)
);
```

#### 8.1.1. Распределение сделок (домен `distribution` внутри `company`)

```sql
create table distribution_groups (
  id uuid pk, company_id uuid, name text, description text,
  active bool default true,
  algorithm text check (algorithm in ('round_robin','least_loaded','priority')),
  member_ids uuid[] default '{}',                 -- порядок важен: очередь round_robin и приоритеты
  disabled_member_ids uuid[] default '{}',        -- в группе, но временно вне распределения
  source text, deal_limit int check (deal_limit >= 1),
  unclaimed_minutes int check (unclaimed_minutes >= 1),
  created_at timestamptz
);
create table distribution_events (
  id uuid pk, company_id uuid,
  group_id uuid references distribution_groups on delete cascade,
  deal_number bigint, user_id uuid,
  status text check (status in ('accepted','in_progress','reassigned','declined')),
  created_at timestamptz
);
create index on distribution_events (group_id, created_at desc);
create sequence deal_numbers start 4822;          -- в моке нумерация от 4821; на сервере — sequence вместо max()+1
```

`member_ids` — массив, а не таблица-связка, осознанно: порядок участников — часть доменной модели (очередь), а ссылка на пользователей и так без FK не нужна (единый паттерн с `assignee_ids` в tasks). Выбор исполнителя (`pickDistributionMember`) — под `select ... for update` группы, чтобы конкурентные сделки не назначились одному и тому же участнику в обход round_robin.

### 8.2. `kb`

```sql
create table sections (
  id uuid pk, company_id uuid, name text, parent_id uuid references sections,
  "order" int, access jsonb                        -- AccessSettings
);
create table articles (
  id uuid pk, company_id uuid, section_id uuid references sections,
  title text, content jsonb,                       -- TipTap JSON
  status text check (status in ('draft','published')),
  author_id uuid, version int default 1, requires_acknowledgement bool,
  plain_text text,                                 -- извлекается из TipTap в Go при записи (pkg/richtext)
  search tsvector generated always as (
    setweight(to_tsvector('russian', coalesce(title,'')), 'A') ||
    setweight(to_tsvector('russian', coalesce(plain_text,'')), 'B')) stored,
  created_at timestamptz, updated_at timestamptz
);
create index on articles using gin (search);
create table article_versions (
  id uuid pk, article_id uuid references articles on delete cascade,
  version int, title text, content jsonb, author_id uuid, created_at timestamptz
);
create table acknowledgements (
  article_id uuid, user_id uuid, acknowledged_at timestamptz,
  primary key (article_id, user_id)
);
```

Версионирование — как в моке: `updateArticle` при изменении title/content/status пишет снапшот старой версии в `article_versions` и инкрементирует `version`; `rollbackArticle` делает то же и подменяет контент. Оба — одной транзакцией. Оптимистическая блокировка: `PATCH` может передавать `If-Match: version` → при расхождении 409 «Статья была изменена другим пользователем» (защита от перезаписи в двух вкладках; фронтенд подключит позже).

### 8.3. `tasks`

```sql
create table boards (
  id uuid pk, company_id uuid, name text,
  type text check (type in ('personal','department','project')),
  department_id uuid, owner_id uuid, created_at timestamptz
);
create table columns (id uuid pk, board_id uuid references boards, name text, color text, "order" int);
create table tasks (
  id uuid pk, company_id uuid, board_id uuid, column_id uuid references columns,
  "order" int, title text, description jsonb, author_id uuid,
  assignee_ids uuid[] default '{}', assignee_position_id uuid, watcher_ids uuid[] default '{}',
  due_date timestamptz, priority text check (priority in ('low','medium','high','urgent')),
  label_ids uuid[] default '{}', checklist jsonb default '[]', attachments jsonb default '[]',
  source jsonb,                                     -- TaskSource (CRM-контекст)
  linked_article_ids uuid[] default '{}',
  recurrence jsonb,                                 -- RecurrenceRule
  completed_at timestamptz, created_at timestamptz, updated_at timestamptz
);
create index on tasks (board_id, column_id, "order");
create table comments (id uuid pk, task_id uuid references tasks on delete cascade,
  author_id uuid, content jsonb, created_at timestamptz);
create table labels (id uuid pk, company_id uuid, name text, color text);
```

`moveTask` — одна транзакция с `select ... for update` по задачам обеих колонок; алгоритм перенумерации — порт текущего из `src/api/index.ts` (`moveTask`), тесты — из `taskBoard.test.ts`.

### 8.4. `academy`

```sql
create table courses (
  id uuid pk, company_id uuid, title text, description text, cover_url text,
  status text check (status in ('draft','published')),
  author_id uuid, sequential bool default true, deadline_days int,
  created_at timestamptz, updated_at timestamptz
);
create table course_sections (id uuid pk, course_id uuid references courses on delete cascade,
  title text, "order" int);
create table lessons (
  id uuid pk, course_id uuid, section_id uuid references course_sections on delete cascade,
  title text, "order" int, content jsonb,
  source_article_id uuid,                            -- ссылка в kb, без FK
  source_mode text check (source_mode in ('link','copy')),
  quiz_id uuid
);
create index on lessons (source_article_id) where source_mode = 'link';  -- для репликации
create table quizzes (id uuid pk, lesson_id uuid, questions jsonb,
  passing_score int, max_attempts int);
create table assignments (
  id uuid pk, course_id uuid,
  assignee_type text check (assignee_type in ('user','position','department','external')),
  assignee_id uuid, invite_token text, due_date timestamptz,
  assigned_by_id uuid, created_at timestamptz
);
create table progress (
  user_id uuid, course_id uuid,
  status text check (status in ('not_started','in_progress','completed','overdue')),
  completed_lesson_ids uuid[] default '{}',
  started_at timestamptz, completed_at timestamptz,
  primary key (user_id, course_id)
);
create table quiz_attempts (id uuid pk, quiz_id uuid, user_id uuid,
  score int, passed bool, pending_review bool, created_at timestamptz);
```

Каскад `deleteCourse` (уроки → тесты → прогресс → назначения) — внутри сервиса одной транзакцией (FK `on delete cascade` + явная чистка); чистка `required_course_ids` в должностях — событием `academy.course.deleted` → консьюмер в `company` (см. §10.1).

### 8.5. `notifications`

```sql
create table notifications (
  id uuid pk, company_id uuid, user_id uuid,
  type text check (type in ('task_assigned','task_comment','task_due','article_published',
    'article_ack_required','course_assigned','course_due','mention')),
  title text, body text, link text,                  -- link — внутренний роут SPA
  read bool default false, created_at timestamptz
);
create index on notifications (user_id, read, created_at desc);
create table processed_events (event_id uuid pk, processed_at timestamptz);  -- идемпотентность
```

---

## 9. Межсервисные вызовы (gRPC) — минимальный набор

Синхронные вызовы сведены к минимуму (каждый синхронный вызов — это связь, мешающая независимому развёртыванию, §3); всё остальное — события.

| RPC | Кто → кого | Зачем |
|---|---|---|
| `company.GetUsersByIds` | notifications, tasks, academy → company | имена/аватары для текстов уведомлений; денормализация в событиях предпочтительнее — RPC как fallback |
| `company.ResolvePositionUsers` | tasks, academy → company | `assigneePositionId` задачи / назначение курса на должность → список userId на момент события |
| `company.ResolveDepartmentUsers` | academy → company | назначение курса на отдел (включая подотделы) |
| `kb.GetArticle` / `kb.GetArticlesByIds` | academy → kb | первичное копирование контента при `createLesson(sourceMode)` и `createCourseFromKb` |
| `kb.ArticleExists` | tasks, company → kb | мягкая валидация `linkedArticleIds` / `positions.article_ids` (опционально) |

Всё через protobuf в `contracts/proto/`; клиенты с таймаутами (500 мс), retry и circuit breaker; недоступность зависимого сервиса не валит операцию, если данные некритичны (валидация деградирует до «принять как есть»).

---

## 10. Событийная архитектура (NATS JetStream)

### 10.1. Каталог событий

Subject-схема: `teamos.<service>.<entity>.<action>.v1` (версия в subject — правило эволюции §3.6). Каждое событие: `eventId (uuid)`, `occurredAt`, `companyId`, `actorId`, полезная нагрузка. Выведено из `NotificationType` и текущих каскадов в моке:

| Событие | Издатель | Потребители и реакция |
|---|---|---|
| `teamos.org.user.created/updated/deactivated` | company | notifications (приветствие), academy (пересчёт назначений на должность/отдел) |
| `teamos.org.position.deleted` | company | kb, academy: чистка ссылок (в моке: снятие должности с сотрудников — внутри company синхронно) |
| `teamos.org.invite.created` | company | notifications → email приглашения (фаза 2 — почтовый шлюз) |
| `teamos.kb.article.published` | kb | notifications: `article_published` всем с доступом; `article_ack_required` если `requiresAcknowledgement` |
| `teamos.kb.article.updated` | kb | **academy: обновить `content` link-уроков** (см. §10.2); payload: `articleId`, `version`, `title`, `content` |
| `teamos.kb.article.deleted` | kb | academy: link-уроки → перевести в `copy` с последним контентом (контент уже реплицирован) |
| `teamos.tasks.task.assigned` | tasks | notifications: `task_assigned` исполнителям (кроме автора) |
| `teamos.tasks.comment.added` | tasks | notifications: `task_comment` автору, исполнителям, наблюдателям |
| `teamos.tasks.task.due_soon` | tasks (воркер) | notifications: `task_due` |
| `teamos.academy.course.assigned` | academy | notifications: `course_assigned` (для position/department — резолв в userIds через RPC §9) |
| `teamos.academy.course.due_soon` | academy (воркер) | notifications: `course_due` |
| `teamos.academy.course.deleted` | academy | company: чистка `positions.required_course_ids` (в моке — синхронный каскад) |
| `teamos.*.mention` | tasks, kb | notifications: `mention` (упоминания в TipTap-контенте, тип узла mention) |

### 10.2. Замена `withLiveContent` (уроки-«ссылки»)

В моке урок с `sourceMode: 'link'` на каждом чтении подтягивает контент статьи (`withLiveContent` в `src/api/index.ts`). В микросервисах — **событийная репликация**:

1. При создании link-урока `academy` синхронно берёт контент через `kb.GetArticle` (RPC).
2. Далее `academy` — durable-консьюмер `teamos.kb.article.updated`: `update lessons set content = $1, title-если-не-переименован where source_article_id = $2 and source_mode = 'link'` (частичный индекс из §8.4).
3. Чтение уроков остаётся локальным и быстрым; рассинхрон — секунды, для учебного контента приемлемо. Семантика `link` «контент не редактируется» соблюдается на уровне домена academy (запрет `updateLesson.content` для link-уроков).

### 10.3. Надёжность

- **Transactional outbox** в каждом сервисе-издателе: событие пишется в таблицу `outbox` той же транзакцией, что и данные; отдельная горутина-релейер публикует в NATS и помечает отправленным. Никаких «сохранили, но не опубликовали».
- Консьюмеры **идемпотентны**: `processed_events` (§8.5) или ключ идемпотентности в апдейте.
- Durable consumers + переигрывание стрима JetStream при добавлении нового потребителя.
- DLQ: после N неудачных обработок событие уходит в `teamos.dlq.*`, алерт в Grafana.

### 10.4. Фоновые воркеры (river поверх Postgres, внутри сервисов)

- `tasks/recurrence`: по `RecurrenceRule` (`daily/weekly/monthly`, `interval`, `weekdays`) — генерация следующего экземпляра при завершении текущего (модель «завершил → появилась следующая»; зафиксировать в ADR, т.к. в моке генерации нет).
- `tasks/duedates`: раз в час — задачи с `dueDate` в ближайшие 24 ч и без `completedAt` → `task.due_soon` (однократно на задачу).
- `academy/deadlines`: `assignments.dueDate` либо `courses.deadlineDays` от `assignment.createdAt` → за 3 дня `course.due_soon`; просрочка → `progress.status = 'overdue'`.
- `company/sessions-gc`: чистка протухших refresh-сессий.

### 10.5. Доставка уведомлений в UI

- REST — как в контракте (`getNotifications`, `unread-count`, `markRead`, `markAllRead`).
- **SSE** `GET /api/v1/notifications/stream` через gateway: hub в `notifications` держит соединения по `userId`, при новой записи шлёт `event: notification`. Фронтенд по событию инвалидирует запросы `['notifications']` и `['notifications','unread']` — поллинг больше не нужен, контракт не ломается. SSE вместо WebSocket: односторонний поток, переживает прокси, `EventSource` в браузере из коробки; реплики hub'а равноправны (§3.2).

---

## 11. Портирование доменной логики из `/Users/nikpeskov/Projects/team-os/src/lib/`

| Источник (фронтенд) | Куда в бэкенде | Содержание |
|---|---|---|
| `src/lib/orgTree.ts` (`canMoveDepartment`) | `services/company/internal/domain/org/tree.go` | построение дерева, запрет перемещения в себя/в потомка |
| `src/lib/inviteRules.ts` | `services/company/internal/domain/org/invites.go` | валидация email приглашения (дубликаты, формат) |
| `src/lib/userGuards.ts` | `services/company/internal/domain/org/guards.go` | защита владельца, запрет самодеактивации, правила смены роли/статуса, `validatePositionAssignment` (не более одной должности) |
| `src/lib/schedule.ts` | `services/company/internal/domain/schedule/calendar.go` | развёртка Week/Cycle-шаблонов в календарь месяца, наложение исключений |
| `src/lib/taskBoard.ts` | `services/tasks/internal/domain/board.go` | правила порядка карточек/колонок |
| `src/lib/dealDistribution.ts` | `services/company/internal/domain/distribution/pick.go` | выбор исполнителя сделки: round_robin (по последнему событию), least_loaded (без declined/reassigned), priority (первый активный по порядку) |

Существующие `*.test.ts` рядом с этими файлами — готовые спецификации: кейсы переносятся 1:1 в табличные Go-тесты. Дублирование логики фронт/бэк осознанное: фронтенд использует её для мгновенного UX (disable кнопок), бэкенд — как последнюю линию защиты; расхождение ловится contract-тестами.

---

## 12. Наблюдаемость, безопасность, конфигурация

- **Трассировка**: OpenTelemetry во всех сервисах; trace-id рождается в gateway, пробрасывается в gRPC-метаданных и заголовках NATS-сообщений → в Tempo видна цепочка «HTTP → company → outbox → NATS → notifications → SSE».
- **Метрики**: RED (rate/errors/duration) на хендлер; глубина outbox; lag консьюмеров; размер DLQ. Алерты: 5xx > 1 %, outbox-лаг > 30 с, DLQ > 0. Пер-сервисные дашборды — по ним же принимается решение «какой сервис выносить на отдельный сервер» (§3.5).
- **Логи**: `slog` JSON → Loki; в каждой записи `trace_id`, `company_id`, `user_id`.
- **Безопасность**: CORS на gateway (dev-origin `http://localhost:5173` — порт Vite фронтенда); rate limit на `/auth/*`; валидация размеров тел (TipTap JSON ≤ 1 МБ); секреты — env/sops, в репо не лежат.
- **Конфигурация**: env-переменные с префиксом сервиса (`COMPANY_DB_URL`, `COMPANY_NATS_URL`, `GATEWAY_JWT_PUBLIC_KEY`, …); `.env.example` в корне; никакой логики в конфиге. Полный список переменных каждого сервиса — в его README; это и есть «интерфейс развёртывания» (§3.3).

---

## 13. Seed-данные из фикстур фронтенда

Чтобы дев-окружение бэкенда выглядело как текущий мок:

1. В фронтенд-репо добавляется скрипт `/Users/nikpeskov/Projects/team-os/scripts/export-fixtures.ts` (node/tsx): импортирует `src/api/fixtures.ts` и выгружает каждый массив в JSON: `users.json`, `departments.json`, `articles.json`, …
2. `make seed FRONTEND_DIR=/Users/nikpeskov/Projects/team-os` в бэкенд-репо: запускает экспорт, затем `cmd/seed` каждого сервиса заливает свои сущности (company → kb → tasks → academy → notifications, порядок важен из-за ссылок по ID).
3. ID из фикстур сохраняются как есть (это UUID-совместимые строки — проверить; иначе детерминированный маппинг старый-ID → uuid5).
4. Пароль всем seed-пользователям — дев-константа, владелец соответствует `CURRENT_USER_ID` из фикстур.

---

## 14. Интеграция с фронтендом (без big bang)

Изменения в `/Users/nikpeskov/Projects/team-os` (все — не ломающие):

1. **`src/api/client.ts`**: рядом с `mockRequest` появляется `httpRequest(path, init)` — fetch к `VITE_API_URL` (default `http://localhost:8080/api/v1`), `credentials: 'include'`, разбор `{error: {message, status}}` в `ApiError`, прозрачный refresh по 401 (одна попытка).
2. **Флаг на модуль**: `VITE_API_MODE_AUTH=http|mock`, `VITE_API_MODE_ORG=…` и т.д. Каждая функция модуля в `src/api/index.ts` выбирает реализацию по флагу — **сигнатуры не меняются**, компоненты не трогаются. Модули переключаются на бэкенд по одному, по мере готовности фаз.
3. **`src/api/queryClient.ts`**: когда все модули на http — убрать `networkMode: 'always'` (комментарий в файле прямо это предписывает); `retry: 2` оставить (теперь он покрывает реальные сетевые сбои и окна rolling update, §3.7).
4. **Auth-страницы** (`/auth`): подключаются к реальным `login/refresh/logout/accept-invite`; access-токен — в памяти (Zustand), refresh — httpOnly cookie, при старте приложения — тихий refresh.
5. **Уведомления**: подписка на SSE + инвалидация ключей `['notifications', …]` — поллинг убирается.
6. Мок-режим (`mock`) сохраняется как оффлайн-демо и для юнит-тестов компонентов.

---

## 15. Локальная разработка

`deploy/docker-compose.yaml`: `postgres:16` (одна инстанция, отдельная БД на сервис: `teamos_company`, `teamos_kb`, …), `nats:2.10` с JetStream, все 6 сервисов (build из Dockerfile'ов), опционально MinIO. Профиль `observability` — Prometheus/Grafana/Loki/Tempo.

Makefile-цели:

```
make up / down          # compose со всеми сервисами
make migrate            # golang-migrate по всем сервисам
make seed               # §13 (использует FRONTEND_DIR)
make gen                # oapi-codegen + protoc + sqlc
make test / lint        # go test ./... / golangci-lint
make check-contract     # сверка контракта с фронтендом (§6.3)
make dev SERVICE=kb     # один сервис локально (go run) против compose-инфраструктуры
```

Сценарий совместной разработки: фронтенд запускается как обычно (`npm run dev` в `/Users/nikpeskov/Projects/team-os`, порт 5173), бэкенд — `make up` в `/Users/nikpeskov/Projects/team-os-backend` (gateway на 8080); в `.env.local` фронтенда — `VITE_API_URL=http://localhost:8080/api/v1` и флаги модулей.

### 15.1. Docker — для разработки, на сервере — по-настоящему

Важное уточнение о роли Docker:

- **Во время разработки** — всё в Docker: compose поднимает Postgres, NATS и сервисы одной командой, окружение одинаково у всех разработчиков и в CI (интеграционные, contract- и e2e-тесты гоняются на том же compose-стенде). Ничего, кроме Docker и Go, на машину разработчика ставить не нужно.
- **При развёртывании на боевом сервере** — по-настоящему, без контейнеров: каждый сервис — статический Go-бинарник под systemd-юнитом (`teamos-kb.service` с `Restart=always` и лимитами), Postgres и NATS — штатно установленные пакеты ОС с настроенными бэкапами (wal-g) и обновлениями. Так меньше слоёв (нет docker-демона, сетей и volume-абстракций между сервисом и диском), проще диагностика (`journalctl -u teamos-kb`) и предсказуемее производительность БД.
- Это не создаёт двух разных систем: благодаря env-адресации (§3.3) и одинаковым `/healthz`-пробам бинарник в systemd и контейнер в compose конфигурируются одними и теми же переменными; CI собирает из одного исходника и образ (для дев/тестов), и релизный бинарник (для прода).
- Рецепты переноса сервиса на отдельный сервер (§3.5) работают в обоих вариантах: «поднять реплику» означает либо `docker run` на дев-стенде, либо копирование бинарника + systemd-юнит на проде.
- Если позже захочется вернуть контейнеры в прод (например, при переходе на Kubernetes) — образы уже собираются в CI с первого дня, переезд не потребует переделки.

---

## 16. Дорожная карта (фазы с чек-листами и уровнем сложности)

Шкала сложности: **низкая** — по образцу, без новых решений; **средняя** — есть нетривиальные части, но паттерны уже определены; **высокая** — принимаются решения, влияющие на всю систему, и/или сложная предметная логика.

**Фаза 0 — контракты и скелет. Сложность: средняя. ✅ ВЫПОЛНЕНО** *(работа во многом механическая, но здесь принимаются решения, которые определят всё остальное — границы, формат событий, правила эволюции контрактов)*
- [x] Репозиторий `/Users/nikpeskov/Projects/team-os-backend`, go.work, Makefile, golangci, CI (matrix по сервисам, §3.1).
- [x] `contracts/openapi/teamos.yaml` — все эндпоинты из §6.2 (механическая трансляция `src/api/index.ts`); `oasdiff` в CI.
- [x] `contracts/events/catalog.md` + proto-схемы событий (§10.1); `buf breaking` в CI.
- [x] docker-compose: postgres, nats; `pkg/apierror`, `pkg/httpx`, `pkg/eventbus` (скелеты).
- [x] ADR-001 (границы сервисов), ADR-002 (claims в JWT), ADR-003 (outbox+NATS), ADR-004 (правила эволюции контрактов, §3.6).

**Фаза 1 — `company` + `gateway` + авторизация. Сложность: высокая. ✅ БЭКЕНД ВЫПОЛНЕН** *(остаётся переключение фронтенда на http; самая ответственная фаза: безопасность, сессии, первый прогон всей цепочки domain → outbox → NATS; портирование трёх доменов; всё последующее строится по её образцу)*
- [x] Миграции и sqlc для схемы §8.1 (включая ЦКП отделов, `birthDate`/`hiredAt`/`vacationAllowance`, правило одной должности); домен org (порт orgTree/userGuards/inviteRules + тесты).
- [x] Auth: login/refresh/logout/accept-invite, argon2id, ротация сессий.
- [x] Gateway: oapi-codegen-хендлеры auth/org, JWT-middleware, CORS; health/readyz + graceful shutdown как эталон для всех сервисов (§3.7).
- [x] Outbox + события `org.*`; seed из фикстур (§13).
- [ ] Фронтенд: `httpRequest`, флаги модулей, переключение `auth` и `org` на http. *(не сделано: фронтенд всё ещё на `mockRequest`, `httpRequest`/флагов модулей нет)*

**Фаза 2 — `kb`. Сложность: средняя. ✅ БЭКЕНД ВЫПОЛНЕН** *(версионирование и русский FTS нетривиальны, но сервис строится по образцу фазы 1; проверка доступов — по готовому алгоритму §7.3)*
- [x] Схема §8.2, версии + rollback, ознакомления; `pkg/richtext` (plain-text из TipTap).
- [x] FTS-поиск (`russian`), проверка `AccessSettings` по claims (§7.3).
- [x] События `kb.article.published` / `kb.article.updated` через outbox; gateway проксирует все `kbApi` эндпоинты.
- [ ] Переключение модуля `kb` на http во фронтенде. *(не сделано: фронтенд всё ещё на `mockRequest`)*

**Фаза 3 — `tasks`. Сложность: средняя. ✅ БЭКЕНД ВЫПОЛНЕН** *(транзакционный `moveTask` с конкурентными перестановками и первые фоновые воркеры; доменная модель при этом простая)*
- [x] Схема §8.3 (+ `due_soon_sent_at`); транзакционный `moveTask` (`SELECT FOR UPDATE` + `domain/board.ReorderAfterMove`); комментарии, метки; порт `taskBoard.ts` → `domain/board`.
- [x] Воркеры recurrence + due-dates (`river`); события `tasks.*` через outbox; gateway проксирует все `tasksApi` эндпоинты; seed из фикстур.
- [ ] Переключение модуля `tasks` на http во фронтенде. *(не сделано: фронтенд всё ещё на `mockRequest`)*

**Фаза 4 — `academy`. Сложность: высокая. ✅ БЭКЕНД ВЫПОЛНЕН** *(самый связный сервис: синхронные RPC к `kb`, событийная репликация link-уроков, резолв назначений на должности/отделы через `company`, каскадное удаление курса — здесь микросервисные паттерны нагружаются по-настоящему)*
- [x] Схема §8.4; `createCourseFromKb` (RPC `kb.GetArticlesByIds`); каскад `deleteCourse` и событие `academy.course.deleted.v1` для очистки `required_course_ids` в `company`.
- [x] Консьюмеры `kb.article.updated` / `kb.article.deleted`: идемпотентная репликация link-уроков и перевод удалённой ссылки в `copy` (§10.2).
- [x] Назначения (user/position/department/external + inviteToken), прогресс, воркер дедлайнов; gateway проксирует все `academyApi` эндпоинты; seed из фикстур и unit-тесты нормализации/маппинга Academy.
- [ ] Переключение модуля `academy` на http во фронтенде. *(не сделано по текущей задаче: фронтенд не изменялся и всё ещё использует `mockRequest`)*

**Фаза 5 — `notifications`. Сложность: средняя** *(доменная модель тривиальная, но SSE-hub с горизонтальным масштабированием (§3.2), идемпотентные консьюмеры всех событий и русские тексты уведомлений требуют аккуратности)*
- [ ] Консьюмеры всех событий §10.1, идемпотентность, тексты по-русски.
- [ ] REST + SSE-стрим; фронтенд: подписка на SSE. Переключение модуля.

**Фаза 6 — добивка. Сложность: средняя** *(разнородные задачи: порт календарной математики графика — низкая; файловый сервис, нагрузочные прогоны и прод-развёртывание с бэкапами — средняя)*
- [ ] `scheduleApi` в `company` (порт `schedule.ts`, upsert шаблона `saveSchedule`, батч-upsert исключений). Переключение модуля.
- [ ] `distributionApi` в `company` (схема §8.1.1, порт `dealDistribution.ts` + тесты, транзакционный выбор исполнителя). Переключение модуля (`VITE_API_MODE_DISTRIBUTION`).
- [ ] Убрать `networkMode: 'always'` во фронтенде; e2e-смоук (§17).
- [ ] Сервис `files` + MinIO/S3 (вложения задач, аватары, логотип) — первый тест рецепта §3.8 «новый сервис без правки существующих».
- [ ] Observability-профиль, алерты, нагрузочный прогон (k6: чтение БЗ/задач, конкурентный `moveTask`).
- [ ] Прод-деплой на сервер по-настоящему, без Docker (§15.1): Go-бинарники под systemd, штатные Postgres и NATS, бэкапы (wal-g), runbook с рецептами §3.4–3.5.

После каждой фазы фронтенд полноценно работает в смешанном режиме (часть модулей http, часть mock) — прогресс виден, и каждая фаза откатывается независимо (флагом модуля во фронтенде).

---

## 17. Тестирование

| Уровень | Что и чем |
|---|---|
| Unit (домены) | табличные тесты в `internal/domain/*`; кейсы — порт из `*.test.ts` фронтенда |
| Integration | репозитории и хендлеры против реального Postgres (testcontainers-go); outbox → NATS в контейнере |
| Contract | ответы gateway валидируются против `teamos.yaml` в CI; `make check-contract` против типов фронтенда (§6.3); `oasdiff`/`buf breaking` против breaking changes (§3.6) |
| E2E смоук | docker-compose: сценарий «register → invite → accept → статья (publish + ack) → курс из БЗ (link) → правка статьи → урок обновился → уведомления пришли по SSE» |
| Нагрузочный | k6-профили на чтение БЗ/задач и moveTask (конкурентные перестановки) |

---

## 18. Риски и решения

| Риск | Митигация |
|---|---|
| Оверинжиниринг для 1–2 разработчиков | 5 сервисов, а не 7+; один Postgres-кластер; Docker только в разработке, прод — systemd-бинарники без оркестраторов (§15.1); никакого mesh/discovery (§3.9). Микросервисы здесь оправданы заявленным требованием №3 (§1) — независимое обновление и вынос сервисов под нагрузку; модульный монолит это требование выполняет хуже (масштабируется только целиком) |
| Распределённые каскады (deleteCourse ↔ positions, deletePosition ↔ статьи/уроки) | события + outbox + идемпотентные консьюмеры; UI терпим к секундам рассинхрона |
| Проверка доступов БЗ требует оргданных | claims `pos`/`dep` в JWT + TTL 15 мин (§7.1, §7.3) — без синхронных вызовов на каждый запрос |
| Дрейф контракта между двумя репозиториями в разных папках | OpenAPI как источник правды + `make check-contract FRONTEND_DIR=/Users/nikpeskov/Projects/team-os` + contract-тесты в CI (§6.3) |
| Независимый деплой ломает соседа со старой версией | правила эволюции контрактов (§3.6): `oasdiff`, `buf breaking`, версии в subjects событий, expand→contract-миграции |
| Рассинхрон link-уроков при сбое консьюмера | durable consumer + переигрывание JetStream + ночная сверка `lessons.content` с `kb` (reconciliation-джоба) |
| Потеря событий при падении между записью в БД и публикацией | transactional outbox (§10.3) — исключено по построению |
| Русские сообщения ошибок размазаны по сервисам | единый `pkg/apierror`: `NotFound("Статья")` → «Статья не найдена», 404; конструкторы для всех доменных запретов; тексты — константы рядом с доменом |
| Конкурентные правки статьи (две вкладки) | optimistic locking по `version` + 409 (§8.2); фронтенд подключает `If-Match` отдельной задачей |
| `mention`-уведомления требуют парсинга TipTap | `pkg/richtext` извлекает узлы `mention` при записи комментария/статьи — без парсинга на чтении |
