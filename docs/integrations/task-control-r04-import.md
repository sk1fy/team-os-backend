# КТ-01.R04: импорт автономных правил и безопасный возврат

Реализованы source requests, admin bridge, preview/apply для v1 и v2. Импорт
заменяет только draft по CAS; публикация и managed включаются отдельными действиями.
Экран импорта и мастер миграции не добавлены. Core хранит инфраструктурное evidence,
company — источники и правила, браузер читает same-origin amoCRM API.

## Авторизация и источники

POST `/api/v1/task-control/connections/{bindingId}/source-requests` требует
текущего owner/admin с taskControl.manage, актуальную установку и scope fences:

```json
{"requestId":"a0000000-0000-4000-8000-000000000077",
 "expectedBindingRevision":1,"expectedModeEpoch":1,
 "kind":"catalog","catalogId":"77"}
```

Kind — `catalog` или `runtime_config`; catalogId обязателен для обоих и должен
совпадать с storage.catalog_id выбранного runtime_config. `references` возвращает
409 `references_server_owned`: общие справочники читаются серверным R03 provider.
GET `.../source-requests/{id}` возвращает pending/completed/failed/expired,
sourceSnapshotId, issues/errorCode и freshUntil. Оба запроса требуют manage.
RequestId долговременно идемпотентен; pending TTL 10 минут, максимум 100 живых
pending заданий на binding. Нет открытого admin bridge — запрос остаётся pending,
после TTL становится expired; пустой успешный источник не подставляется.

Администратор явно запускает bridge существующим действием «Код подключения»
либо `widget.taskControlMigration.startAdminBridge()`. Это работает и при
teamos_enabled=0, пока автономные правила продолжают исполняться. Без этого
действия новая standalone установка не делает запросов TeamOS.

Bridge получает fresh amoCRM disposable JWT, затем Core assertion purpose
`adminBridge` с live CRM admin. GET `/api/v1/widget/task-control/source-requests`
передаёт assertion как `Authorization: Bearer …`. Это отдельное право: обычный
runtime bearer и TeamOS JWT не заменяют proof. Подключение выводится из подписанных
company/account/installation/generation, CRM actor совпадает с администратором
запроса; права текущего инициатора TeamOS проверяются повторно.

Для PUT `/api/v1/widget/task-control/source-requests/{id}/result` нужен новый proof,
у которого requestId равен ID задания, и тот же coreAssertion в header и body:

```json
{"coreAssertion":"<одноразовый signed adminBridge assertion>",
 "complete":true,"sourceChecksum":"<64 hex checksum полного набора правил>",
 "fetchedAt":"2026-10-16T12:00:00Z",
 "payload":{"catalogId":"77","code":"RKRS_WT_RULE_UID",
   "fieldIds":{"rule_uid":100},"rules":[],"raw":{},"discrepancies":[]}}
```

Это сокращённый пример формы; полная рабочая fixture с реальными UID, всеми
полями и raw — [source result](../../contracts/fixtures/task-control-import-source-result.json).
Браузер читает все страницы, проверяет тип/аккаунт каталога, CODE/ID/type/enum
полей, уникальность элементов и UID. Не исправляет отсутствующий UID генерацией
нового и не удаляет некорректные правила из «полного» результата. При прерывании
или некорректном источнике отправляет complete=false/диагностику.

Сервер независимо сверяет raw → правила, количество, UID, флаги, IDs и checksum
**до JSONB**, чтобы повторяющиеся JSON-ключи не исправлялись при записи. Неуспешный
capture получает failed/source_invalid и не получает snapshotId. Тело ≤1 MiB;
превышение — 413. Исходный raw/settings сохраняются в company и локальном backup
браузера; ошибка backup не игнорируется. Bridge ничего не меняет в каталоге.

## Preview и применение

Для `/api/v1/task-control/connections/{bindingId}` и `/api/v2/...` доступны:

- POST `/imports`: requestId, expectedBindingRevision/ModeEpoch,
  expectedDraftRevision и sourceSnapshotId → сохранённый preview на 30 минут.
- GET `/imports/{id}`: scope, sourceKind/catalogId/checksum, rules, original,
  complete/issues; после применения также backup и appliedRequestId.
- POST `/imports/{id}/apply`: requestId, expectedBindingRevision/ModeEpoch,
  expectedDraftRevision, expectedSourceChecksum и **новый** verificationSnapshotId.

Preview не меняет draft. Source snapshot действует 5 минут от серверного ReceivedAt;
FetchedAt браузера информационный. Retry upload не продлевает TTL. Для apply
нужен другой полный snapshot того же источника, полученный после preview. Сверяются
scope, вид источника, исходные данные, карта полей и checksum. Изменившийся raw,
истёкший snapshot или повторное использование исходного snapshot дают source_changed.

Catalog и runtime_config могут различаться. Kind явно выбирает источник;
предупреждения сравнения вычисляются независимо сервером и остаются в preview.
Для их принятия apply должен содержать `acknowledgeDiscrepancies:true`, иначе
409 `import_conflicts_unconfirmed`. Невалидный выбранный источник не становится
допустимым от этого флага. Валидный runtime_config можно импортировать при удалённом
каталоге: catalog_invalid явно сообщает невозможность сравнения и требует подтверждения.
Ошибка 403 или чужой аккаунт не выдаются за удалённый каталог.

Apply атомарно сохраняет предыдущий draft в backup, новый draft и результат
idempotency. Повтор того же semantic request возвращает прежний результат после
актуальной проверки прав даже при истечении источников; другое тело — конфликт.
Параллельный повтор не создаёт вторую revision. Смена epoch/revoke/account запрещает
использование старого источника. В v2 каждый импортированный RuleV1 сохраняется как
`legacyExplicit`: списки пользователей, 142/143, disabled/archived и остальные флаги
не превращаются в allLinked/allOpen. Импорт не создаёт CRM employee mappings.

Полная fixture preview — [пример](../../contracts/fixtures/task-control-import-preview.json).
Оригинал и backup приватны и выдаются только актуальному manager этого binding.
Они хранятся долговременно, отдельно от коротких сроков применимости источника.

## Managed → standalone

R11 mode-preview теперь материализует **полный** набор с disabled/archived, без
actor projection. Метаданные: complete=true, projection=full, expiresAt, scope,
publicationId, sourceRevisions, exportChecksum, runtimeRules и dynamicLoss.
Вычисление идёт по свежим справочникам/оргструктуре и копии intent; публикация не
меняется. Если хотя бы одно правило нельзя представить без потери состава,
export_unrepresentable явно запрещает экспорт. Полностью пустой активный состав
сохраняет отдельное confirmEmptyExport; потеря динамики требует confirmDynamicLoss.

После явного mode commit future UI получает receipt с новым epoch и вызывает:

```js
await widget.taskControlMigration.prepareStandalone(receipt, {
  confirm: true, acceptDynamicLoss: true, confirmEmpty: false
});
```

Одного preview недостаточно: нужен completed receipt targetMode=standalone,
соответствующий новому epoch, и свежая проверка backend. Новый GET
`/api/v1/widget/task-control/connection` с runtime bearer сообщает актуальный
mode/scope в обоих режимах, не выдавая actor-проекцию и не продлевая lease правил.

Виджет сохраняет backup исходных settings/raw, создаёт отдельный каталог, повторно
читает и сверяет полный checksum; исходный каталог не перезаписывается. До и после
операции проверяет mode/scope, исходный capture и изменения settings. Результат
только подготавливает скрытый runtime_config. Для фиксации администратор отключает
существующий флаг TeamOS и нажимает штатное «Сохранить»; native Save выполняет
финальную проверку конкурирующих изменений. При отказе сохранения старые настройки
и исходный каталог сохранены; новый частично созданный каталог не становится runtime.
Назад в managed возвращается сохранённый server intent, а не догадки по спискам CRM IDs.

## Выпуск и проверки

Пакет 1.2.1, Core migration 28 и company migration 38. После установки пакета
оператор подтверждает 1.2.1 для текущей installation/generation; 1.2.0 остаётся
совместимым с v2. Down 28 запрещает потерю evidence 1.2.1; down 38 запрещает удаление
существующих оригиналов/backup. Пустая схема поддерживает down/up.

Проверки: real PostgreSQL/race import/CAS/idempotency/isolation, duplicate raw keys
до JSONB, partial и изменившийся source, отключение прав, v1/v2 semantics; полный
экспорт с флагами; widget pagination/migration/backup/native Save/standalone zero HTTP;
OpenAPI/DTO и общая fixture. Скрипты backend `check-task-control-widget-contract.cjs`,
`check-task-control-v2-widget-contract.cjs`, `check-task-control-import-widget-contract.cjs`
проверяют v1/v2/import на настоящих модулях виджета. Live amoCRM, production rollout
и установка пакета в окружение пользователя не выполнялись.
