# КТ-01.R07: подтверждения runtime и техническая диагностика

Источники контракта: `contracts/openapi/teamos.yaml`, `contracts/proto/company/v1/company.proto`; Core — `api/admin-openapi.yaml` в amocrm-pro. Экраны TeamOS и админки в этап не входят.

## API и авторизация

- `POST /api/v1/task-control/runtime/telemetry` — события `received`, `applied`, `heartbeat`, `error`, общий протокол для runtime v1/v2.
- `POST /api/v1/task-control/runtime/ack` — совместимый ACK явной публикации v1.
- `POST /api/v2/task-control/runtime/ack` — существующий ACK actor-проекции v2. Сохранённый формат запроса теперь проверяется по актуальной выдаче и текущей ревизии.
- `GET /api/v1|v2/task-control/connections/{bindingId}/publications/{publicationId}/application` — состояние наблюдавшихся экземпляров. В v1 обработчик заменяет прежний 501.

POST требуют отдельный opaque runtime bearer, полученный через свежую Core assertion и `/api/v1/widget/task-control/sessions`. JWT TeamOS и adminBridge assertion не заменяют runtime-сессию. Gateway обращается в company со служебной авторизацией; company повторно проверяет session, actor, компанию, generation, bindingRevision, modeEpoch и актуальность установки под блокировкой аккаунта.

GET требуют текущее членство и `taskControl.read`. Чужое подключение/публикация не раскрываются. Сохранённые сведения читаются при недоступном Core; неподтверждённая установка делает наблюдения устаревшими. Все ответы — `Cache-Control: private, no-store`.

## События

Обязательные поля telemetry: `scope`, `actorId` (десятичная строка amoCRM), `runtimeInstanceId`, `runtimeVersion`, `event`, `eventId`, `occurredAt`, `correlationId`. Для успешных наблюдений обязательны `publicationId`, `publicationVersion`, `runtimeRevision`, `payloadChecksum`, а для v2 также `snapshotId`.

`received` означает, что клиент принял и проверил выданную проекцию. `applied` отправляется после установки правил в локальный движок. `heartbeat` допустим только после подтверждённого применения той же публикации и runtime revision этим экземпляром. Он не заменяет первоначальный applied и не меняет время применения. У каждой вкладки собственный UUID instance, закреплённый сервером за actor.

`error` принимает только коды `config_invalid`, `config_fetch_failed`, `lease_expired`, `scope_mismatch`, `runtime_incompatible`, `apply_failed`, `references_unavailable`, `network_unavailable`. Ошибку загрузки после получения session можно передать без полей публикации. Свободный текст, URL, правила, содержимое сделок, JWT и assertions запрещены строгим DTO. Correlation UUID связывает полученную конфигурацию и последующие наблюдения; ошибки до выдачи используют UUID попытки загрузки.

События runtime идут через ограниченную фоновую очередь. Зависший диагностический POST не задерживает установку правил; порядок received → applied сохраняется. Heartbeat отправляется не чаще раза в 60 секунд. Закрытая/остановленная вкладка не продолжает heartbeat. Телеметрия никогда не продлевает session или lease и не запускает дополнительную авторизацию.

Фактический wire-пример, сформированный кодом виджета: `contracts/fixtures/task-control-telemetry.json`. Проверки `scripts/check-task-control-v2-widget-contract.cjs` и `TestTaskControlTelemetryAcceptsActualWidgetWireFixture` сверяют его с реальным клиентом, Go DTO и выданной actor-проекцией.

## Актуальность и ограничения

Company сохраняет метаданные фактически выданной публикации/проекции. Нельзя подтвердить чужой checksum, actor, snapshot, старую публикацию, старую ревизию пересчитанного состава либо истёкшую выдачу. Lease — максимум пять минут и для v2 ограничена свежестью справочников. Даже для одной intent-публикации разные runtime revision учитываются раздельно. Изменённый повтор `eventId` конфликтует; идентичный повтор идемпотентен в пределах действующей выдачи.

Размер POST ограничен 8 КиБ, повторяющиеся JSON-ключи отвергаются. Лимит — 20 событий на экземпляр в минуту, 1000 на подключение в минуту и 256 активных экземпляров за пять минут. Превышение возвращает 429 с `telemetry_rate_limited` или `telemetry_instance_limit`; неверная/устаревшая ревизия — 409 `runtime_revision_stale`. Неверный runtime bearer — 401/403; превышение размера — 413; несогласованные поля — 400.

`occurredAt` допускает до пяти секунд вперёд и до пяти минут назад. Для выдачи дополнительно проверяется, что событие не предшествует ей более чем на пять секунд. Свежесть и lastSeenAt считаются по серверному времени получения, а не по часам браузера.

## Чтение состояния

Старые поля v1 `scope`, `publicationId`, `publishedVersion`, `runtimeState`, `lastRuntimeAck` сохранены. В v2 сохранены `publicationId`, `runtimeState`, `issues`, `currentSnapshotId`, `currentRuntimeRevision`, `lastRuntimeAck`. Дополнительно возвращаются:

- `instances`: до 256 записей с runtime version/revision, состоянием active/stale, раздельными временем получения/применения/heartbeat, lease, причиной и correlation UUID;
- `activeInstances`, `staleInstances`, `appliedInstances` — счётчики только возвращённой выборки;
- `observedAt`, `evidenceTtlSeconds: 300`, `retentionDays: 30`, `truncated`;
- `coverage: observedInstancesOnly`, `allInstancesApplied: false`.

Последний ACK означает применение хотя бы одним наблюдавшимся действующим экземпляром. Это не подтверждение применения всеми вкладками, сотрудниками или по всем сделкам. Отсутствие событий сохраняет неопределённость; закрытая вкладка становится stale после истечения lease/TTL. История других ревизий не подтверждает текущую. Лимит выборки событий — 2000; `truncated` явно сигнализирует ограничение. При отсутствии ACK v1 опускает `lastRuntimeAck`, v2 возвращает null.

Причины stale включают `scope_changed`, `heartbeat_expired`, `lease_expired`, `sources_stale`, `installation_unverified`, `connection_inactive`, `runtime_revision_stale`. Ошибка получения конфигурации видна разрешённым техническим кодом и correlation UUID. Heartbeat и его отсутствие не меняют состояние установки и не доказывают uninstall.

## Core и будущая админка

`GET /admin/v1/installations/{id}/task-control/diagnostics` доступен только на существующем admin listener через `ADMIN_API_TOKEN` и `X-Admin-Actor`. Это отдельная административная граница, не браузерная runtime-сессия.

Ответ содержит технический срез реестра, текущие generation/revision, enabled grant/product, ожидающую reconciliation, доказательство версии runtime текущего поколения, код состояния и UUID запроса (`request_id`/`X-Request-ID`). `fresh_until` ограничен пятью минутами, ответ `no-store`. В проекцию не попадают правила, JWT, assertions, session bearer, service keys, произвольные metadata и персональные данные сотрудников. Неизвестные поля/поддеревья не добавляются к разрешённому DTO.

Пример: `docs/examples/task-control-diagnostics.json` в Core. Диагностика не заменяет reconciliation R06 и не объявляет uninstall на основании молчания браузера.

## Хранение и поставка

Миграция company 40 добавляет технические выдачи и события. Фоновая очистка работает раз в минуту пакетами до 1000 записей; события удаляются после 30 дней, метаданные выдач — через 30 дней после validUntil. Срок очистки пакетный; старые записи не учитываются как свежие. Down отказывается удалять сохранённые события. История бизнес-публикаций и аудит R05 остаются в своём хранилище.

Runtime 1.2.2 добавляет протокол наблюдений; Core миграция 29 и CLI подтверждения версии поддерживают 1.2.2, сохраняя поддержку 1.2.0/1.2.1. Сначала поставляются совместимые company/gateway и Core, затем пакет виджета и подтверждение версии оператором. UI, production-миграции и живая приёмка amoCRM в локальный прогон не входят.
