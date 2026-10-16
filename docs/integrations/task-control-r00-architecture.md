# R00: общие справочники и динамические правила Контроля задач

2026-10-16 · статус: на review · [R00](https://app.clickup.com/t/869fj7r39).
Основание: [ADR-0038, редакция 4](../../../amocrm-pro/docs/adr/0038-task-control-hybrid-integration.md).
Нормативный проект для последующих реализаций, без изменения работающего MVP.
Существующие OpenAPI v1 остаются описанием текущих wire-форм. Новые пути/DTO ниже
должны попасть в OpenAPI и генерацию вместе с реализацией R03/R11. Reference API и
provider реализованы в R03: [фактический контракт и проверки](task-control-r03-references.md).
Intent/resolver/runtime v2 остаются задачей R11.
При расхождении с историческим описанием v1 этот документ определяет только v2.

## 1. Проверенная база и границы переиспользования

Проверка исходников: Core `64355af`, backend `43134a7`, TeamOS `8a0f483`,
widgets `6e6d665`. Это проверка кода, не live-приёмка.

| Уже есть | Используем | Что требуется |
|---|---|---|
| Core `internal/gateway/gateway.go`: Users/GetDirectory, кеш 30 s, выдача ≤100, кеш ≤1000 пользователей | amoCRM client, кеширование и policy patterns | Не использовать выдачу панели как полный справочник |
| Core `internal/gateway/enrichment.go`: Pipelines | Reader/маппинг/лимитер | Полнота, provenance, общий revision |
| Core `internal/integration/amocrm/distribution.go`: DistributionUsers/Pipelines | Проверенное постраничное чтение; пользователи до 4×250, превышение — ошибка | Вынести нейтральные readers, сохранив адаптеры Distribution; добавить groups/customer statuses |
| Core `internal/distribution/http.go`: referenceData | Проверки до/после чтения, accountDomain, свежесть 5 min | Не вызывать Distribution endpoint/grant из Контроля задач |
| Core `internal/taskcontrol/status.go`: signed HTTP, durable nonce, scoped grant | Транспорт и проверка установки | Отдельная capability references, generation fence |
| Backend `amo_users.go`, `org.go`, `corebridge` | Отделы, employees, CRM mapping, server bridge | Общая проекция выбора; события/ревизии mapping и org |
| Backend `task_control_rules.go`, `domain/taskcontrol/rules.go` | CAS draft, immutable publication, idempotency, legacy checksum | Сейчас explicit IDs и `structural-v1`; реальной проверки справочников нет |
| Widget `lib/settings_controller.js`, `lib/teamos.js` | Standalone loader; managed auth, polling, memory cache | Customers error сейчас превращается в пустой массив; v1 отвергает смену checksum при прежней version — для динамики нужен v2 |

Core владеет OAuth, чтением CRM, CRM reference cache и установкой. Company владеет
оргструктурой, mapping, правами TeamOS, draft/intent/publication/snapshot/ACK.
Виджет исполняет материализованные правила; Core не получает их содержимое.
Никаких общих БД, нового сервиса или обязательной установки Distribution.
Демо-счётчики активных сделок из макетов не входят в обязательный reference DTO:
чтение всех сделок ради выбора воронки не требуется.

## 2. Маршрут и матрица прав

Managed: TeamOS UI → gateway → company → Core product adapter → общий CRM provider.
Company worker использует тот же adapter без браузера. Новый внутренний путь Core:
`GET /internal/task-control/accounts/{accountId}/references` без query/body.
HMAC — существующий `X-Task-Control-*` transport и `Signature` из status.go:
method и точный URI входят в подпись, окно ±60 s, durable nonce, TLS, без redirects.
Company/account/product берутся из проверенного grant, installation/integration/generation
разрешаются сервером, а не из UI. Ответ содержит весь scope. Проверка grant и generation
повторяется после CRM-чтения и перед ответом, включая cache hit.

Добавить capability `task-control.references.read` в grant. Существующее право
installation.read не расширяется автоматически; миграция выдаёт references только
явно разрешённым bindings. Общий provider не содержит правил авторизации Distribution;
каждый adapter проверяет свою product policy до вызова provider.

| Операция | Субъект/условия |
|---|---|
| Список соединений, справочники, история, диагностика | TeamOS `taskControl.read`, активный company scope; минимальные данные выбора |
| Draft/validate/publish/restore/import/refresh | Текущий owner/admin + `taskControl.manage`; актуальная установка и binding |
| Link/mode/revoke | То же + свежий Core CRM-admin proof, purpose/requestId bound |
| Core references | Service HMAC + references capability + exact company/account/integration/product + active installation/generation |
| Фоновый пересчёт | Company service identity; активная опубликованная связь, references grant; не права конкретного автора публикации |
| Runtime config/ACK | Короткая runtime session, exact actor/binding/epoch/generation; без списка сотрудников/отделов |
| Standalone settings | Существующие CRM admin checks и same-origin; нет зависимости от TeamOS |

Имена/CRM groups в справочнике не доказывают роль. Runtime не может читать references.
Не выдавать email, resource rights, OAuth или service secrets. Отдельная read-policy
разрешает просмотр пользователю без manage, но не публикует от его имени.

## 3. Reference DTO, полнота и жизненный цикл

`ReferenceSnapshotV1`:

```text
schemaVersion: 1
scope: {companyId, accountId, integrationId, installationId, generation, product}
revision: sha256:<64 lowercase hex>
provenance: {source: "coreAmocrm", fetchedAt, fetchStartedAt}
freshUntil: RFC3339 UTC
complete: true
users: [{id: CrmId, name, isActive: boolean, groupId: CrmId|null}]
groups: [{id: CrmId, name}]
pipelines: [{id: CrmId, name, sort: integer,
  statuses: [{id: CrmId, name, sort: integer, color: string|null,
              kind: "open"|"won"|"lost"}]}]
customers: {availability: "supported"|"unsupported"|"unknown",
  statuses: [{id: CrmId, name, sort: integer, color: string|null}],
  checkedAt: RFC3339 UTC, errorCode: string|null}
```

CrmId — canonical positive decimal string, safe JS integer, без ведущих нулей;
TeamOS ID — UUID. Названия не являются идентичностью. Закрытые этапы определяются
семантикой CRM (142/143 для сделок), не именем/цветом. Неизвестная классификация
делает справочник непригодным к исполнению, а не открытым этапом по умолчанию.
CRM groups читаются из полного источника групп аккаунта; нельзя объявлять перечень
групп полным, выведя его только из пользователей (пустые группы потеряются).
Endpoint и capability customers проверяет adapter; 401/403/timeout не равны unsupported.
Unsupported подтверждается ответом о возможности аккаунта; unknown не становится [].
При unknown customers разрешены lead-only правила; customer-правила не публикуются
и не пересчитываются до получения соответствующей свежей части источника.

Все страницы users/groups/pipelines/statuses дочитываются до подтверждённого конца,
проверяются дубли и связи status→pipeline, поля активности и идентичность аккаунта.
Ограничения первого релиза: ≤1000 users, ≤1000 groups, ≤1000 pipelines,
≤1000 statuses на pipeline, общий reference response ≤4 MiB. Превышение/цикл
пагинации/неполный ответ — `references_incomplete` или `references_too_large`, не усечение.
Чтение ограничено 30 s через существующий account limiter; singleflight на scope.
CRM не даёт атомарного снимка между страницами: complete означает полное чтение,
а не транзакцию CRM; несовместимые ссылки требуют повторного полного чтения.

Revision — SHA-256 канонического DTO scope + данные + customers availability,
без времён/ошибок транспорта. RFC 8785 JCS после сортировки entity arrays по
числовому CRM ID, statuses внутри pipeline; nullable поля сохраняются. Идентичные
данные при новом чтении сохраняют revision, но получают новые fetchedAt/freshUntil.
TTL отсчитывается от fetchStartedAt: 5 min, не от cache hit или времени получения в company.
Если чтение закончилось уже после freshUntil, оно не годится как fresh.

Core cache key включает company/account/integration/installation/generation/product/
reference-schema/grant revision. На первом релизе кеш не разделяется между products,
хотя код provider общий. Кеш never grants access. Revoke/uninstall/reinstall/OAuth
identity change инвалидируют запись; старое асинхронное чтение не может её воскресить.
Company хранит последний полный снимок и отдельно последнее состояние ошибки.
GET может вернуть stale snapshot для отображения с `state: stale|unavailable`;
публикация/validate используют только fresh. Пустой fresh и недоступный источник различаются.

Целевой UI API v2: `GET /api/v2/task-control/connections/{bindingId}/references`:
`{scope, state, crm: ReferenceSnapshotV1|null, organizationRevision, mappingRevision,
employees: [{employeeId, name, positionName, departmentIds, crmUserId|null,
eligible, ineligibleReason|null}], departments: [{id,name,parentId|null}], issues: []}`.
Все employee/mapping чтения ограничены company + конкретным account; связь с другим
аккаунтом не подходит. Организация читается согласованно с её revision; CRM группами
нельзя перезаписать org при каждом reference GET. Существующая синхронизация amo_users
остаётся отдельной операцией. Пустой departmentIds — «Без отдела», не фиктивный UUID.
Отсутствующая/неоднозначная mapping, inactive employee или inactive CRM user → ineligible.

`POST .../references/refresh` (v2, manage) ставит coalesced job и возвращает 202 +
refreshId; `GET .../references/refresh/{refreshId}` возвращает pending/completed/failed.
GET никогда не запускает бесконечную блокирующую загрузку; первый неизвестный
снимок возвращает unavailable и ставит ограниченное обновление.

## 4. Намерение правила v2

RuleV2 сохраняет ruleUid/name/enabled/archived/showModal/denyExit/checkResponsible.
Вместо v1 users/pipelines/customerStatuses — discriminated selection:

```json
{
  "schemaVersion": 2,
  "ruleUid": "11111111-1111-4111-8111-111111111111",
  "name": "Поддержка и выбранный сотрудник",
  "audience": {"kind": "selected", "employeeIds": ["22222222-2222-4222-8222-222222222222"],
    "departments": [{"departmentId": "33333333-3333-4333-8333-333333333333", "includeDescendants": true}]},
  "leads": {"kind": "selected", "pipelines": [
    {"pipelineId": "11405630", "stages": {"kind": "allOpen"}}
  ]},
  "customers": {"kind": "disabled"},
  "showModal": true, "denyExit": true, "checkResponsible": true,
  "enabled": true, "archived": false
}
```

Audience alternatives: `{kind:"allLinked"}` либо selected как выше. allLinked —
все активные сотрудники TeamOS с однозначной активной CRM mapping этого аккаунта,
не все пользователи amoCRM. selected — объединение people и отделов без дубликатов;
includeDescendants обязательно, будущие потомки включаются при true, при false —
только непосредственные участники. Цикл дерева запрещён. Удаление человека из отдела
исключает его, если он не выбран отдельно/через другой отдел. Исключений из all/department
в v2 нет: снятие галочки отдельного человека требует явного преобразования выбора
в список людей, с потерей автоматического пополнения, не скрытой смены семантики.
«Без отдела» выбирается списком людей, не динамическим псевдоотделом.

Leads alternatives: `{kind:"disabled"}`, `{kind:"allOpen"}`, selected с pipelines.
Stages alternatives: `{kind:"allOpen"}` или `{kind:"selected",statusIds:[CrmId...]}`.
allOpen охватывает будущие воронки/открытые этапы; whole pipeline — будущие открытые
этапы только выбранной pipeline. Явно выбрать won/lost в v2 нельзя. Customers:
disabled/all/selected с statusIds; это сущность customers, не воронка с таким названием.
Пустой selected недопустим (использовать disabled там, где предусмотрен); хотя бы
одна сущность включена. Пустой вычисленный состав допустим: intent сохраняется,
правило даёт ноль исполняемых строк + диагностическую причину, никогда «все».

Draft допускает сохранение устаревших ссылок с issues для исправления; publish
отклоняет unknown/deleted explicit employee/department/pipeline/status и неактивных
явно выбранных people. Валидный существующий пустой отдел/allLinked без участников
можно публиковать с warning. Disabled/archived intent хранится, не исполняется;
структура/UID всё равно валидируются, reference errors блокируют только активные правила.
После публикации удалённые ссылки дают issue и исключаются при пересчёте; намерение
не редактируется. Повторное появление той же identity восстанавливает участие.
Новая mapping выбранного employee может изменить его CRM ID — это ожидаемая динамика,
фиксируемая mappingRevision. Никогда не связывать по имени.

Элемент `intentRules` — union `{kind:"dynamic", rule:RuleV2}` либо
`{kind:"legacyExplicit", rule:RuleV1}`; UID уникален между обоими видами.
Legacy explicit v1 сохраняется как отдельный kind `legacyExplicit` с исходными RuleV1.
Не преобразовывать all-users-like список в allLinked и не требовать TeamOS employee
для старого CRM ID. Закрытые этапы старого explicit правила сохраняют старое поведение.
Импорт v1 создаёт legacyExplicit, а не новую динамическую выборку.

## 5. Публикация, resolver и runtime v2

Publication immutable: id, version, schemaVersion=2, scopeAtPublication,
draftRevision, intentRules, intentChecksum, createdAt/actor/reason/restoredFromId.
Intent checksum — SHA-256 JCS intentRules, rules по ruleUid, наборы UUID лексически,
CRM ID численно, строки name trim без Unicode normalization; порядок входных наборов
не влияет. Discriminated union запрещает лишние поля. Draft CAS и durable idempotency
сохраняются. Publish создаёт publication и первый snapshot в одной транзакции;
при непригодных источниках publication не создаётся.

Company хранит immutable вычисленные snapshots отдельно:

```text
snapshotId: UUID
scope: {companyId,bindingId,accountId,integrationId,installationId,generation,
        bindingRevision,modeEpoch}
publicationId, publicationVersion, intentChecksum
runtimeRevision: safe positive int64, монотонно на binding + modeEpoch
sourceRevisions: {crm, organization, mapping}
resolverVersion: 1
resolvedChecksum: sha256 legacy canonicalRulesJson полного разрешённого набора
resolvedAt, sourceFreshUntil, issues: [{ruleUid,code,path}]
runtimeRules: materialized legacy rules
```

Все селекторы разрешаются в одном согласованном org/mapping snapshot и полном
CRM reference snapshot. Employee UUID преобразуются в CRM IDs только в resolver.
Пустое правило не отдаётся legacy engine, [] всего набора валидно и выключает guard.
Лимит 1000 правил, 1000 users/pipelines/statuses в правиле и 1 MiB runtime payload:
превышение блокирует новый snapshot с issue, не усечение; старый живёт по своей lease.

Resolver job: (binding, epoch, publicationId, sourceRevisions, resolverVersion).
Перед atomic продвижением currentSnapshot проверяются binding/generation/epoch,
тот же active publication, свежесть и отсутствие более новой source revision.
Числовые org/mapping revisions сравниваются по порядку; CRM hash не упорядочивается:
используется current source pointer + generation/fetch sequence. Поздний job теряется,
не перетирает новую публикацию. Идемпотентный повтор того же job не создаёт ревизию.
Изменение sourceRevisions создаёт runtimeRevision даже при равном resolvedChecksum,
чтобы provenance/ACK были однозначны; неизменный источник только обновляет lease
по новой подтверждённой свежести, без изменения immutable snapshot.

Worker company опрашивает active managed bindings раз в 60 s, заранее обновляет Core
snapshot при возрасте ≥120 s, singleflight/retry/backoff с учётом CRM limiter.
Org/mapping changes в company дают durable outbox + reconciliation polling 60 s.
Цели при здоровых сервисах и в пределах лимитов: CRM change → resolver ≤210 s
(120 s возраст + 60 s scheduler + 30 s fetch); org/mapping → resolver ≤60 s;
видимая активная вкладка получает snapshot ещё ≤60 s. Это проверяемые SLO, не обещание
при 429/недоступности/спящей вкладке. Draft worker не читает, только active publication.

Прямой runtime v2 имеет отдельный путь `/api/v2/task-control/runtime/bootstrap`,
с тем же механизмом короткой сессии; session scope остаётся actor-specific.
Envelope: `{runtimeSchemaVersion:2,scope,actorId,publicationId,publicationVersion,
intentChecksum,snapshotId,runtimeRevision,sourceRevisions,resolverVersion,
resolvedChecksum,payloadChecksum,runtimeRules,issuedAt,validUntil,minRuntimeVersion}`.
RuntimeRules — проекция для actor; payloadChecksum — existing legacy canonical hash
проекции, resolvedChecksum полного набора не раскрывает сам набор. В ответе нет
имен других сотрудников и intent/org. Client сравнивает runtimeRevision в текущем
scope, не publicationVersion; равная revision + другой payload hash отклоняется.
Новая revision при той же publicationVersion допустима. Проверяются все scope,
schema, safe integers, hash, actor. Epoch/generation смена требует новой сессии.

`validUntil = min(issuedAt + 5 min, latestVerifiedSourceFreshUntil)`; установка
должна быть подтверждена fresh перед выдачей. Lease — отдельные метаданные выдачи,
не checksum snapshot. Poll 60 s, при ошибках backoff до 5 min; при resume немедленный
bootstrap. Вкладка хранит snapshot в памяти до validUntil (монотонный локальный
таймер на основе issuedAt/validUntil, без продления при retry). Потом fail-open,
diagnostic stale, без переключения в standalone. Это сознательное ограничение
offline v2: бессрочное исполнение старых отделов не соответствует динамике.
Подтверждённый revoke/uninstall/403 очищает кеш немедленно; timeout не revoke.
401 допускает свежий auth обмен, но не продлевает lease. Новая выдача при stale
references — 503. CRM customer unknown блокирует snapshot, если опубликованные
active правила зависят от customers; lead-only конфигурация не зависит от них.

ACK v2: scope, runtimeInstanceId, actorId из session, snapshotId, publicationId,
publicationVersion, runtimeRevision, payloadChecksum, appliedAt, runtimeVersion.
Company проверяет выданный actor payload; receivedAt серверный. Unique на
(scope/epoch,actorId,runtimeInstanceId,runtimeRevision), повтор того же hash идемпотентен,
иной hash конфликт. Старый ACK сохраняется как исторический, не делает current applied.
appliedByRuntime означает лишь подтверждение конкретной вкладки; freshness ACK ≤5 min,
история 30 дней. Snapshot метаданные/actor checksum доступны для проверки ACK 30 дней.

## 6. Совместимость, восстановление, переключение

Нельзя отдать динамику существующему v1: его проверка запрещает новый checksum при
той же publication version. V1 API, checksum, правила и MVP runtime не меняются.
V2 draft/publication endpoints получают отдельный `/api/v2/task-control` namespace;
v1 запись для v2-managed binding отвечает 409 `requires_v2`, не теряет intent.
Перед включением v2 нужны установленный runtime с capability `dynamicRulesV2`
и явная mode/schema migration: новый epoch, свежая сессия, проверенный descriptor.
Минимальный semver назначается R01 по выпущенному пакету, capability проверяется
сервером по поддерживаемой версии, не произвольному флагу клиента. Старые вкладки
получают отказ и обновляются; одновременное v1/v2 исполнение одного epoch запрещено.
До перехода v1 продолжает работать как сейчас, включая прежние offline ограничения.

Restore создаёт новую publication version из старого intent с текущими справочниками,
новый snapshot/runtimeRevision; UI/API preview показывает diff и потерянные ссылки.
Исторические reference revisions нужны для аудита, не для повторного включения
уволенных сотрудников. Невалидный restore оставляет current без изменений.
Legacy restore сохраняет explicit semantics и валидирует актуальные CRM ссылки.

Standalone export материализует полный набор от свежих источников, не actor projection.
Preview явно сообщает потерю all/department/future-stage динамики; confirmation bound
к export checksum + sourceRevisions + publicationId + epoch. Изменение источников
до mode commit даёт 409 source_changed и новый preview. Превышение лимита settings
или пустые/непредставимые правила — явная диагностика; нельзя молча потерять правило.
Пустой полный набор экспортируется как выключение только с явным подтверждением.
UID сохраняются. Возврат managed использует сохранённый intent при новой валидации,
а не восстанавливает динамику догадкой из массива IDs.
## 7. Формы операций и примеры переходов

Все пути ниже — проект v2, relative к `/api/v2/task-control/connections/{bindingId}`.
Scope определяется сервером. Ошибки сохраняют `{error:{message,status,code}}`,
сообщения по-русски. Не перечисленные операции подключения/session остаются v1.

| Метод/путь | Основные поля запроса → результат |
|---|---|
| GET /draft | → `{scope,revision,schemaVersion:2,intentRules,checksum,issues}` |
| PUT /draft | `{expectedRevision,expectedBindingRevision,expectedModeEpoch,intentRules}` → новый draft |
| POST /validate | `{expectedDraftRevision}` → `{valid,intentChecksum,sourceRevisions,issues,resolvedPreview}` |
| POST /publications | `{requestId,expectedDraftRevision,expectedChecksum,expectedBindingRevision,expectedModeEpoch}` → immutable publication + initial snapshot identity |
| GET /publications; GET /publications/{id} | История intent publications с v1 cursor/limit semantics; detail содержит intent, не изменяет его |
| POST /rollback | `{requestId,sourcePublicationId,expectedPublishedVersion,expectedBindingRevision,expectedModeEpoch}` → новая publication и текущий resolved snapshot |
| GET /publications/{id}/application | → `{publicationId,currentSnapshotId,currentRuntimeRevision,runtimeState,lastRuntimeAck,issues}` |

Runtime ACK — `POST /api/v2/task-control/runtime/ack`, body из §5; ответ 204.
ACK не является продлением lease. `scope` в body сверяется с session, не задаёт её.
Validate — preview, не обещание фиксации источников: publish заново разрешает правила
по fresh references и сохраняет фактические sourceRevisions. Если клиенту требуется
подтвердить именно preview, publish допускает `expectedSourceRevisions`; расхождение
даёт 409 `source_changed`, без записи. Дубликат requestId с тем же semantic body
возвращает прежний результат после текущей auth/scope проверки, даже если draft уже изменён.

Errors: 400 invalid_selection/unsupported_schema; 404 для чужого scope/ID;
409 revision_conflict/epoch_conflict/source_changed/requires_v2;
413 references_too_large/runtime_payload_too_large;
503 references_unavailable/references_incomplete. Подробные reference issues
возвращаются в validate; failed publish не создаёт version. Public projection
не раскрывает Core grant/CRM resource rights.

Пример: publication version=7 выбирает отдел D целиком; draft revision=9 содержит
несохранённый в publication выбор другого отдела. Snapshot revision=21 разрешил
CRM users [101,102]. Добавление сотрудника с CRM ID 103 в D меняет organizationRevision,
создаёт snapshot revision=22 с [101,102,103], publication version остаётся 7,
draft revision остаётся 9. ACK revision=21 не подтверждает revision=22.

Пример: выбранная воронка P имеет open stages [10,11], won=142 и lost=143.
allOpen после добавления этапа 12 разрешает [10,11,12]; selected [10] остаётся [10].
Legacy explicit [142] сохраняется только как legacyExplicit; новый dynamic selection
[142] отклоняется. Удаление всех членов D даёт runtimeRules=[] для этого правила,
сохраняет intent D и снова включает правило при появлении нового eligible employee.

## 8. Приёмочные сценарии и владельцы реализации

| Проверка | Ожидаемый результат | Задача |
|---|---|---|
| Нет Distribution, установлен TaskControl | References работают с собственным grant | R02/R03/R06 |
| Чужой company/account, revoked grant, late CRM response после reinstall | Отказ; кеш не возвращает данные другого scope | R02/R03/R06 |
| 110 сотрудников; >1000; ошибка второй страницы; пустой отдел | Полные 110; явный лимит; нет частичного snapshot; валидный пустой состав | R03/R11 |
| Customers 403 vs unsupported; pipeline «Покупатели» | unknown vs unsupported; leads не превращаются в customers | R03/R08 |
| Новый сотрудник/потомок отдела, смена mapping, увольнение | Новый runtimeRevision без изменения publication/draft, соблюдены SLO | R11 |
| Новая воронка/этап, won/lost, explicit legacy | allOpen расширяется только открытыми; legacy не переинтерпретирован | R11/R08 |
| Publish одновременно с resolver, duplicate job, late ACK | CAS сохраняет новую publication; нет дубля revision; ACK не current | R11/R07 |
| Одинаковая publication, новый runtime checksum | v2 принимает возрастающий runtimeRevision; v1 не получает v2 | R01/R11 |
| Offline >lease, resume, revoke, unknown schema | Fail-open после lease, без standalone fallback; отказ не стирает intent | R01/R08 |
| Restore после удаления department; export после изменения refs | Preview issue/отказ публикации; source_changed до commit | R04/R05 |
| Сохранён неопубликованный draft; CRM состав изменился | Исполняется пересчитанная publication, draft не опубликован | R11/R09 |

R00 завершает проектирование. R02 вводит права; R06 lifecycle; R01 session/runtime v2.
R03 реализует provider, DTO и refresh; R11 intent/resolver/worker/API v2; R04 импорт;
R05 история/restore; R07 ACK/diagnostics; R08–R09 проверяют; R10 выпускает.
Frontend дизайн и реализация вне этих задач. Любая генерация OpenAPI/proto выполняется
в задаче реализации вместе с кодом и проверкой совместимости, без редактирования generated файлов вручную.
