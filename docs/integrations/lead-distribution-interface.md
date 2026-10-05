# РС-07: рабочий интерфейс TeamOS и локальная проверка

Распределение использует сохранённые группы, правила, графики, очередь и результаты РС-06. TeamOS обращается к gateway `/api/v1/distribution/*`; принятие команды и ручное действие не означают подтверждённое назначение. Ответственного выбирает company worker, внешний эффект и доказательство результата принадлежат Core.

## Контракт и ограничения первой версии

Источник REST: `contracts/openapi/teamos.yaml`, межсервисный контракт: `contracts/proto/company/v1/company.proto`. Генерация: `PATH="$HOME/go/bin:$PATH" make gen`; фронтенд получает типы из канонического OpenAPI. Старые API симуляции/очистки `distribution_events` остаются отдельными от неизменяемой рабочей очереди/истории.

- `GET /distribution/settings`, `PUT /distribution/settings`: IANA timezone компании; отсутствующая настройка явно требует конфигурации.
- `GET /distribution/rules?limit&offset`, `POST /distribution/rules`, `PUT /distribution/rules/{ruleId}`: текущие правила. Редактирование проверяет `expectedRevision`. Необязательные `pipelineId/statusId` принимаются и проверяются по свежим справочникам Core. Изменить точку можно только до появления первой строки очереди данного правила. Для использованной точки нужно приостановить старую группу и создать новую группу/правило: старые эпизоды, источники и история сохраняются. Первое включение новой точки устанавливает новый watermark, исторические сделки не становятся новыми событиями.
- `PUT /distribution/groups/{groupId}/configuration`: `{expectedRevision,name,memberIds,disabledMemberIds,active,algorithm:"round_robin"}`. Порядок `memberIds` задаёт RR. `revision` возвращается и обычным API групп. Потерявший роль owner/admin во время ожидания блокировки не может сохранить настройки. Изменение состава/порядка во время незавершённой операции отклоняется; приостановка допустима. В первой версии одна группа имеет одно правило, включая приостановленное.
- `GET /distribution/references?bindingId=UUID`, `GET /distribution/connections/{bindingId}/mappings`, `GET /distribution/rules/{ruleId}/availability`: справочники и реальные причины доступности. `user.status=active` не заменяет проверку графика, сопоставления и текущего CRM-пользователя.
- `GET /distribution/queue`: `tab=waiting|assigning|completed|errors|cancelled`, `groupId`, `from`, `to`, `limit` (1–100, default 50), `offset` (0–100000). Период по времени поступления `[from,to)`, обратная сортировка `(createdAt,id)`, `hasMore` и `checkedAt`; это offset pagination, не неизменяемый снимок при новых событиях. Вкладка «Назначаются» содержит `dispatching/uncertain`, «Завершены» — `confirmed/kept`, «Ошибки» — `requires_configuration/failed`.
- `GET /distribution/queue/{queueId}`: детали только после текущей проверки прав на конкретную сделку amoCRM. `updatedAt` является версией для действий. `actions` вычисляет сервер. `resultVersion` — версия известного зеркала Core, `plannedEmployeeId` — план, `currentEmployeeId` — свежее наблюдение, `previousEmployeeId` — ответственный в зафиксированном решении. Неизвестные/несопоставленные значения остаются null.
- `GET /distribution/queue/{queueId}/history?limit&offset`: сохранённые переходы, причина и время. Публичный `payload` по-прежнему пустой объект; frozen commands, CRM-доказательства, секреты и внутренние поля не публикуются.
- `POST /distribution/queue/{queueId}/actions`: `{action:"recalculate"|"check"|"retry"|"cancel",requestId:UUID,expectedUpdatedAt:ISO}`. Только текущие owner/admin с подтверждённым доступом к сделке CRM. При неизвестном HTTP-результате повторяется **тот же** requestId и тело. Company сохраняет неизменяемую идентичность, пользователя и тело в той же транзакции, что wake/cancel/history. Тот же ID с другим телом/очередью/пользователем даёт 409. Актуальная строка и lease проверяются под блокировкой. Сначала сохраняется намерение, затем штатный worker использует уже существующие frozen controls Core.
- `GET /distribution/summary?groupId`: `timezone`, `checkedAt`, `metricsAvailable`, `metricsReason`, nullable `waiting/assigning/errors/confirmedToday/keptToday`. Счётчики относятся только к сделкам, доступным вызывающему сотруднику CRM. Сегодня определяется календарной датой компании и временем подтверждённого завершения; принятие 202 и неопределённый результат не считаются успехом. Чтобы не создавать неконтролируемый поток проверок прав, проверяется максимум 100 релевантных записей (вся незавершённая очередь/ошибки и сегодняшние подтверждения) в общем бюджете 3 секунды. При превышении бюджета/неполных правах/сбое/смене timezone возвращаются null и объяснение, а не ложные нули или раскрывающий скрытые сделки общий total. Это явное ограничение первой версии; расширение требует permission-scoped агрегатов Core.

Список очереди сохраняет непрозрачные локальные UUID и состояния, доступные разделу TeamOS; `leadId=null` и пустой `actions` при неподтверждённых CRM-правах. Детали и CRM-имя/ссылка отдельно защищены живой проверкой. Все ответы `private, no-store`, компанию берут из проверенного контекста пользователя.

Core добавляет необязательные `leadName/leadUrl` в private `LeadObservation`. URL строится только из текущего installation account_domain, проверенного существующим `amocrm.AccountBaseURL`, и конкретного lead ID; TeamOS повторно проверяет HTTPS/host/path и права перед раскрытием. Если свежий источник недоступен, карточка сохраняет подтверждённую историю и явно неизвестные текущие значения. План/старое наблюдение не выдаются за текущего ответственного.

### Безопасность ручного восстановления

Перерасчёт доступен до создания операции. Проверка будит существующую операцию, сохраняет её идентичность и запускает штатную сверку. Повтор терминальной ошибки разрешён только при отсутствии операции либо доверенном `no_attempt + guardReleasable`; возможный/неопределённый внешний эффект запрещает повтор. Отмена до dispatch завершает локальную очередь. После dispatch она сохраняет `cancel_requested`, оставляет `uncertain` и оба claims до доказательства Core; пользовательская отмена не инициирует автоматический повтор эпизода. RR-курсор изменяет только подтверждённое назначение.

## Миграция и совместимость

Company migration `000026_distribution_interface` добавляет revision всех изменений группы, неизменяемый ledger UI-действий, индекс и `UNIQUE(company_id,group_id)` для правил. Перед изменением схема проверяет старые дубликаты; наличие нескольких правил группы останавливает миграцию с понятной ошибкой, ничего не выбирая и не удаляя. Оператор должен заранее проверить `SELECT company_id,group_id,count(*) FROM distribution_rules GROUP BY company_id,group_id HAVING count(*)>1` и согласовать исправление данных с сохранением исторической принадлежности. Старые writers также защищены новым DB constraint. Rollback отказывается удалять уже использованный ledger действий.

РС-06 тест передачи владения точкой использует разные группы: приостановленный старый владелец сохраняет очередь, новое правило другой группы владеет новыми входами. Нельзя обходить first_activation_at ради демонстрационных результатов.

## Проверка локально без реального аккаунта

UI fixtures находятся в отдельном локальном тестовом контуре фронтенда; они явно обозначены как тестовые и не являются рабочими показателями. Локальные проверки server-side работают с изолированным PostgreSQL, fake CRM и настоящими company handlers/queries/transactions. Core проверяет signed API и канонический ответ на своём изолированном PostgreSQL. Старый paired bridge РС-06 проверяет реальное взаимодействие сервисов с контролируемой CRM fixture; это не проверка живого amoCRM.

В корне Team backend:

```sh
PATH="$HOME/go/bin:$PATH" make gen
make test
make check-contract FRONTEND_DIR=/Users/nikpeskov/Projects/team-os-rs07
```

В `services/company` (Docker/Colima запущен):

```sh
DOCKER_HOST="unix://$HOME/.colima/default/docker.sock" \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock GOWORK=off \
go test -tags integration -race ./internal/application \
-run 'TestDistributionInterface|TestDistributionQueueDurable|TestDistributionNight' -count=1 -v
GOWORK=off golangci-lint run --config ../../.golangci.yaml ./...
```

В Core: `make distribution-integration-test` (ранее собранные matching migrator/test images), `go vet ./internal/distribution`, `go test -race -count=1 ./internal/distribution ./api`.

Сценарии включают exact replay после перезапуска, конфликт тела/версии, поддельную устаревшую роль, потерю admin во время SQL-lock wait, company scope, no-attempt retry, запрет unknown retry/сохранение claims при отмене, confirmed-only timezone, скрытые сделки/неизвестные метрики, редактирование использованной точки и DB one-rule/group.

## Варианты следующей проверки

1. **Изолированный тестовый сервер с искусственной CRM.** Отдельные hostnames, базы, NATS и secrets; TeamOS + gateway/company + Core, default capability off. Установить matching версии/миграции и связать сервисы через private HTTPS/HMAC/grants. Поднять отдельную fake CRM, сохранив production OAuth/CRM настройки изолированными. Это даст совместную проверку UI, reverse proxy, CORS, cookies/auth, polling, потери сети, рестарта worker и состояния неизвестного результата. Fake-подмена должна быть ограничена отдельным staging deployment и не включаться флагом в рабочем UI. Этот вариант требует подготовить стенд; сейчас он не развёрнут.
2. **Тот же стенд с настоящим тестовым аккаунтом amoCRM.** Создать интеграцию для тестового tenant, указать HTTPS redirect/webhook URL, провести OAuth, настроить scoped capability/grants, связать TeamOS компанию и подтвердить mapping. Нужны тестовые сотрудники/CRM пользователи, одна воронка с этапами входа/выхода и искусственные сделки. Секреты хранить в secret store/env, не в чате. Проверить вход→очередь→подтверждённую смену ответственного, keep current, вне смены→утро, права разных ролей, CRM-deactivation, ручную смену ответственного, OAuth refresh/reauth, повтор вебхука, network recovery. После этого отдельно закрыть отложенную живую проверку РС-03.1.2.
3. **Локальные сервисы + тестовый amoCRM через HTTPS tunnel.** Меньше подготовки сервера, но callback/webhook адрес зависит от tunnel, ноутбук должен оставаться доступным. Использовать только тестовый tenant и отдельные redirect/grants. Для стабильной совместной приёмки предпочтителен второй вариант.

Текущая работа не деплоит тестовый/рабочий сервер, не использует реальные аккаунты и не подтверждает OAuth/вебхуки/назначение живой CRM. Актуальные runtime инварианты Core: `amocrm-pro/docs/specs/lead-distribution-v1/10-queue-implementation.md` и `amocrm-pro/api/distribution-openapi.yaml`; ранние connection runbooks не заменяют текущий контракт РС-06/07.
