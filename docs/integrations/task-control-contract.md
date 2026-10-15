# Контроль задач: модель данных и API v1 (КТ-01.02)

Дата: 2026-10-15. Основание: согласованный [ADR-0038](../../../amocrm-pro/docs/adr/0038-task-control-hybrid-integration.md).
Задача: [КТ-01.02](https://app.clickup.com/t/869fhtvv7).

КТ-01.02 фиксирует контракт; КТ-01.03 реализует подключение, статус и auth handoff. Правила и UI пока не реализованы. REST опубликован
в `contracts/openapi/teamos.yaml`, прямая виджетная/инфраструктурная граница — в
`contracts/openapi/task-control-internal.yaml`. Go gateway и TS-типы генерируются;
`team-os/src/api/taskControl.ts` использует эти типы без независимых DTO.
Gateway возвращает 501 только для будущих обработчиков правил/импорта/режима. Подключение, список, состояние установки, link intent, revoke, обмен сессии и bootstrap реализованы.
Миграция 32, company RPC и обработчики подключений реализованы в КТ-01.03; правила — .04–08; UI — .09–11,
адаптация runtime — .12. Только реализованные операции подтверждают сохранение; 501 не считается успехом.

## Владение и формат идентификаторов

Company владеет всеми бизнес-таблицами ниже. Core хранит реестр установок,
телеметрию и nonce в своей БД. Нет общей БД или FK между сервисами.
`tasks` TeamOS и `TaskSource` не используются: CRM-задачи остаются в amoCRM.

Все TeamOS ID — UUID, CRM ID — положительные десятичные строки (до 16 цифр).
Для передачи в legacy JS runtime дополнительно требуется `Number.isSafeInteger`;
превышение безопасного диапазона отклоняет публикацию, не округляется.
Revision/version/epoch — положительные int64, ограниченные 2^53−1 в JSON.
Время — UTC RFC3339. Все поля REST camelCase. `ruleUid` — wire-имя стабильного
`rule_uid` в БД и `uid` legacy snapshot. CRM IDs не становятся UUID сотрудников.

`companyId` находится в каждой строке и response scope, но **не принимается**
из пользовательского тела/параметра. Его получает gateway из сессии; company
повторно проверяет активность компании, пользователя и права. Чтение любой
сущности выполняется по companyId + bindingId + ID; чужой ID даёт 404.
Core получает company из подписанного grant для installation query; runtime scope проверяет backend TeamOS.

## Логическая схема хранения (миграции — КТ-01.04)

| Таблица company | Ключи и данные | Ограничения |
|---|---|---|
| task_control_connections | company_id, id UUID, installation_id, integration_id, account_id, binding_revision, mode_epoch, mode, state, timestamps | Один active/pending владелец installation/product; единственность owner обеспечивает company по проверенному installation ID |
| task_control_intents | company_id, id, binding_id, expected_revision/epoch, target_mode, publication_id, descriptor_checksum, state, expires_at | Unique(company_id,id); normalized request hash; один незавершённый mode intent на binding; prepared→confirmed→completed либо cancelled/expired до commit |
| task_control_drafts | company_id, binding_id, revision, schema_version, checksum, updated_by/at | PK(company_id,binding_id); UPDATE WHERE revision=expected; revision начинается с 1, даже для пустого набора |
| task_control_rules | company_id, binding_id, rule_uid, все поля Rule, порядок незначим | PK(company_id,binding_id,rule_uid); archived не удаляет UID; полная замена draft в одной транзакции |
| task_control_publications | company_id, id, binding_id, version, draft_revision, schema_version, checksum, rules JSONB, scope, created_by/at, reason, restored_from_id | Unique(company_id,binding_id,version); immutable; rule UID может повторяться между версиями; полный снимок, включая архив |
| task_control_idempotency | company_id, binding_id, action, request_id, request_hash, result_id, state | Unique(company_id,binding_id,action,request_id); durable результат/конфликт; для link scope — installation и intentId |
| task_control_imports | company_id, id, binding_id, source_catalog_id, source_checksum, base_revision, complete, rules JSONB, issues, expires_at | Неполный источник не применяется; source rule_uid обязателен; один UID не создаётся повторно |
| task_control_audit | company_id, id, binding_id, actor_id, action, request_id, publication_id, reason, created_at | Append-only; OAuth/coreAssertion/подписи не сохраняются в audit |
| task_control_runtime_acks | company_id, binding_id, mode_epoch, publication_id, actor_id, runtime_instance_id, version, payload_checksum, applied_at, received_at | Unique(company_id,binding_id,mode_epoch,runtime_instance_id,version); чужой scope отклоняется |

Дополнительно: task_control_installation_snapshots (company/account/product,
revision/state/checked_at/fresh_until); task_control_source_requests (company/binding,
actor, requestId/kind/catalog/state/expiry); task_control_source_snapshots (company,
requestId, ID/hash/body/server_received_at, complete); task_control_runtime_sessions
(company/binding/epoch/actor, token hash/expiry/revoked). Nonce/JTI consumption
хранится отдельно и не выдаёт полномочий по одному факту наличия записи.

FK внутри company составные `(company_id, ...)`, чтобы не связать данные разных
компаний. Внешние installation/integration/CRM ID не имеют FK. Номер publication
выделяется под lock строки connection; транзакция сохраняет publication, audit
и idempotency result вместе. Технические повторы не создают новую версию.
Active pointer company продвигается CAS в текущем epoch. Публичный actor не задаёт scope.

## Поля Rule и совместимость

| REST/БД | Legacy runtime | Смысл |
|---|---|---|
| ruleUid / rule_uid | uid | Стабильный UUID, независимый от catalog element ID |
| name | name | Непустое название до 200 символов |
| users | users | CRM users, не UUID сотрудников TeamOS; минимум один |
| pipelines[].pipelineId, statusIds | pipeline_id, status_ids | Совпадение этапа внутри воронки |
| customers, customerStatuses | customers, customer_statuses | Покупатели; пустые статусы = все |
| showModal, denyExit | show_modal, deny_exit | OR по совпавшим правилам |
| checkResponsible | check_responsible | Применять только к текущему ответственному; не меняет проверяемого исполнителя карточки |
| enabled, archived | enabled, archived | Отключение/архив исключает исполнение, но сохраняет правило |

Максимум 1000 правил/пользователей/воронок на правило — верхний контрактный предел,
не обещание поддержки такого объёма в amoCRM settings. Тело bridge ограничивается
1 MiB; превышение — 413. Лимит settings и реальный пакет проверяются в КТ-01.12/14.
Справочники должны быть полными и свежими; unknown ID, повтор UID/pipeline ID,
неактивный пользователь и несовместимая схема не публикуются. При недоступном
источнике — 503, не пустой справочник. `customersSupported=false` означает
недоступность сущности: новые active customer rules не публикуются.

Просроченная незавершённая задача считается задачей. Guard карточки покупателя
ищет задачу ответственного; список покупателей — текущего пользователя и сохраняет
лимит первых 250 карточек на запрос. Это намеренно остаётся legacy-семантикой.

## REST и условия операций

Все пути начинаются с `/api/v1/task-control`. Связь в URL — `{bindingId}`.

| Операция | Поведение |
|---|---|
| GET /accounts/{accountId}/installation | Installed/notInstalled/unknown для блока Интеграции |
| POST /connections/{bindingId}/source-requests; GET /connections/{bindingId}/source-requests/{sourceRequestId} | Запрос/состояние admin bridge для полного CRM снимка |
| GET /connections; POST /connections/link-intents; GET /intents/{intentId} | Подтверждение установки с live CRM admin, повтор intentId восстанавливает результат; link не меняет standalone mode |
| POST /connections/{bindingId}/mode-intents | Явное переключение: expectedBindingRevision/expectedModeEpoch, publicationId, targetMode, requestId; не мгновенная смена |
| POST /connections/{bindingId}/revoke | Отзыв без fallback; обновляет binding revision, инвалидирует epoch |
| GET /connections/{bindingId}/references | CRM users, pipelines/statuses, customers/statuses; revision и интервал свежести |
| GET/PUT /connections/{bindingId}/draft | Полный draft; PUT заменяет набор атомарно с expectedRevision и scope fence |
| POST /connections/{bindingId}/validate | Проверка указанной draft revision и справочников, без записи publication |
| GET/POST /connections/{bindingId}/publications | История / публикация expectedDraftRevision+expectedChecksum |
| GET /connections/{bindingId}/publications/{publicationId} | Полный immutable snapshot; полный admin export использует этот путь |
| POST /connections/{bindingId}/rollback | sourcePublicationId + expectedPublishedVersion → новая version, текущие CRM ID проверяются заново |
| POST /connections/{bindingId}/imports; GET /imports/{importId} под binding | Чтение полного каталога через admin bridge непосредственно в backend TeamOS; preview без изменения draft |
| POST /connections/{bindingId}/imports/{importId}/apply | Только сохранённый preview с тем же source hash и draft revision; обновление draft, не публикация |
| GET /connections/{bindingId}/publications/{publicationId}/application | Опубликованная версия и ACK конкретной вкладки |
| GET /connections/{bindingId}/history | Безопасный аудит без секретов |

История/публикации используют opaque cursor + limit 1..100 (default 50), стабильный
порядок `(created_at,id)` либо `(version,id)` по убыванию. Cursor связан с company,
binding и фильтром; подмена/несовместимый cursor — 400. Нет cursor — конец списка.

Чтение блока в Интеграциях требует `taskControl.read` (owner/admin по умолчанию; отдельный override task_control_permissions для чтения/ограничения прав). Запись — текущий owner/admin плюс
`taskControl.manage`. Подключение/mode/revoke дополнительно требуют CRM admin.
`distribution` не предоставляет этих прав. Серверная policy реализована отдельно в .03; редактор прав относится к .09. Существующий employee API не расширяется.

## Ревизии, идемпотентность и ошибки

Каждая запись проверяет expected binding revision и epoch; draft PUT также
expectedRevision. Ошибки не обновляют данные. Успешный PUT увеличивает revision
один раз. Повтор старого PUT получает 409; клиент перечитывает draft, не сливает
чужие правки автоматически. Publish повтор с тем же requestId проверяется
**до сравнения устаревшей draft revision**, после текущей аутентификации/scope и проверки installed,
и возвращает исходную publication. Запросы с тем же ключом и другим semantic
body дают 409. Идемпотентность не позволяет обойти revoke/смену полномочий.

Request hash включает action, binding scope и нормализованный body; одноразовый
coreAssertion исключается из business hash, но каждый сетевой повтор заново
аутентифицируется. Для link используется intentId; для остальных durable действий
requestId. Записи ключей хранятся не меньше истории объекта, а не короткий TTL.
Новый случайный ключ после timeout не допускается без выяснения старого результата.

`{error:{message,status,code?}}` совместим с ApiError. Сообщения — по-русски.

| HTTP | code | Действие клиента |
|---|---|---|
| 400 | task_control_invalid_rule / unsupported_schema / invalid_cursor | Исправить вход, не повторять автоматически |
| 401/403 | task_control_access_denied | Заново проверить вход/права, не менять режим |
| 404 | task_control_not_found | Не раскрывать принадлежность чужой сущности |
| 409 | task_control_revision_conflict / epoch_conflict / idempotency_conflict / source_changed | Перечитать состояние, показать конфликт |
| 413 | task_control_payload_too_large | Уменьшить конфигурацию |
| 503 | task_control_references_unavailable / core_unavailable | Ограниченный retry только безопасных чтений/сохранённого idempotent запроса |
| 501 | task_control_not_implemented | Контракт есть, функция ещё не реализована |

## Импорт и hash

Admin bridge читает все страницы каталога, проверяет CODE/field IDs и UID, сохраняет
enabled/archived. Preview действует 30 минут; перед apply проверяется полный
sourceChecksum ещё раз. Нет UID или обнаружен повтор — issue, не генерация нового
UID при каждом повторе. Apply заменяет весь draft содержимым preview после явного
предпросмотра; существующий draft не теряется без expectedRevision. Исходный
каталог не изменяется. Одинаковые source+UID+hash не размножают правила.

Для schemaVersion=1 checksum — SHA-256 UTF-8 результата legacy
`lib/snapshot.js:canonicalRulesJson`. Mapper camelCase→legacy сохраняет поля таблицы
выше; ID становятся точными safe JS integers, users/statuses сортируются численно
и дедуплицируются, pipelines по ID, rules по UID. Порядок ключей — как `stableRule`.
Строки названий trim как `normalizeRule`, без дополнительной Unicode-нормализации.
Publication checksum включает весь набор, даже архив/disabled. Payload checksum
runtime — тот же алгоритм над отфильтрованными правилами текущего actor. Отдельный
request hash — SHA-256 канонического JSON semantic body, с сортировкой object keys;
его нельзя подменять checksum правил.

Новый managed envelope содержит scope, publicationId/version/schemaVersion,
actorId, publicationChecksum/payloadChecksum и rules. В нём нет обязательного
catalog_id. Он **не передаётся** старому `parseRuntimeConfig`: КТ-01.12 добавит
версионный parser и mapper перед существующим RuleEngine. Standalone schema
не изменяется. Unknown schema или hash/scope mismatch не вытесняют valid cache.

## Прямой runtime, Core и переключение

Машинная граница находится в `task-control-internal.yaml`. Все session/bootstrap/
config/ACK/mode/source endpoints принадлежат backend TeamOS. У Core остаются
auth handoff, GET installation status и POST telemetry. В публикации нет Core outbox,
ACK Core или зеркала правил. Методы Core не принимают Rule/Publication.

JWT amoCRM проверяет только Core существующим widgetauth. Core server-side CRM
GET подтверждает активность/роль и exact account/integration grant определяет company.
Core выдаёт одноразовый Ed25519 assertion (TTL 60с, aud teamos-task-control, jti,
scope, actor, generation, purpose, requestId); backend имеет только public key.
`coreAssertion` не является JWT amoCRM. Backend проверяет подпись/срок/назначение
и атомарно потребляет jti с записью подключения или opaque session (TTL 5мин).
Runtime session не создаёт TeamOS пользователя и не повышает его роль.

При link purpose=link + intentId, при revoke purpose=revoke + requestId,
при выдаче сессии purpose=runtime. Для будущих административных источников/режима
Core принимает purpose=adminBridge/mode с CRM admin; session не заменяет эти права.
Ошибку ответа после обмена сессии нельзя повторять тем же assertion: запросить
новый JWT у amoCRM, новое подтверждение Core, новый обмен. Link/revoke повторяют
тот же intent/requestId, читая сохранённый результат; новый ключ вслепую запрещён.

Core status query подписывается отдельным service grant task-control.installation.read;
HMAC с X-Task-Control-* headers и company/account/product scope (точная строка
подписи в OpenAPI), окно ±60 секунд, durable nonce,
запрет redirects. Источник — реестр установки именно Контроля задач, не heartbeat,
не просто наличие OAuth общей интеграции. Server scope проверяет account/company.
Установки другого продукта не активируют блок. Core telemetry использует локальную
проверку amoCRM JWT, не зависит от наличия компании TeamOS и может работать standalone.

### Интеграции и свежесть установки

GET `/api/v1/task-control/accounts/{accountId}/installation` работает для уже
проверенного аккаунта компании, в том числе когда managed binding ещё не создан.
Он не связывает произвольный accountId с текущей компанией. `installed` делает
блок активным; `notInstalled` — видимым неактивным; `unknown` — явно неподтверждённым.
Отсутствие Core-записи без полного подтверждения источника означает unknown,
а не notInstalled. Только installation ID может быть ещё неизвестен.

Backend сохраняет snapshot Core с revision/checkedAt/freshUntil, срок до 5 минут.
При открытии Интеграций и перед операцией проверяется его свежесть; свежий статус
используется без network call на каждое config-чтение. Истёкший статус обновляется;
при ошибке Core UI видит unknown, новый config/мутации получают 503, а последний
валидный offline snapshot виджета продолжает работать. Подтверждённый uninstall
даёт 403, инвалидирует runtime sessions, новые записи запрещены; правила/история
не удаляются. Старые revision игнорируются. Повторная установка даёт новую identity
и требует явной привязки, прежний кеш автоматически не применяется.
Платёжный/лицензионный gate не входит в задачу.

### Admin bridge и источники

TeamOS POST source-requests создаёт requestId для references/catalog, bound к
company/binding/account/epoch и текущему admin; TTL 10 минут. Только соответствующий
проверенный admin widget получает pending request и читает same-origin CRM API.
Результат PUT идемпотентен по requestId+semantic hash, другой результат — 409.
Body ограничен 1 MiB, complete=false не создаёт успешный snapshot. Timestamp
свежести назначает сервер при приёме, fetchedAt браузера информационный.
Snapshot действует 5 минут; повтор upload не продлевает его срок.
Для catalog обязателен catalogId в запросе; для references его быть не должно.
References получает server-generated sourceSnapshotId. Browser payload refs
не включает этот ID: он назначается после проверки результата.
Полнота и целостность проверяются относительно запроса; администратор может
предоставлять данные как разрешённый редактор, но эти данные не предоставляют
новых прав и не доказывают чужую установку или роль.

Импорт preview принимает sourceSnapshotId полного каталога. Перед apply нужен
новый verificationSnapshotId того же каталога с тем же checksum; изменение
источника — 409 source_changed. Повтор того же apply возвращает сохранённый
результат до проверки срока snapshot, но только после актуальной проверки прав.
Отсутствие открытого admin виджета отображается pending/expired, не подменяется
пустыми справочниками. Правила, уже опубликованные, не зависят от admin bridge.

### Публикация, режим и ACK

Company транзакционно сохраняет immutable publication/audit/idempotency result.
Виджет читает latest подходящего scope напрямую, с polling 30 секунд только
в активной вкладке и backoff до 5 минут. Bootstrap идёт перед config.
Payload ограничен 1 MiB. Неизвестная schema или checksum/scope mismatch не
вытесняют валидный кеш. Пустой snapshot — явное выключение правил.

Company сохраняет mode intent: prepared→confirmed→completed. До confirmed
проверены установленность, admin, версия виджета и целевая publication/export.
В confirmed компания фиксирует новый epoch/fence; виджет сохраняет descriptor
и подтверждает hash через прямой mode ACK. До completed записи обоих редакторов
приостановлены. Prepared можно отменить/expire; confirmed требует reconciliation,
а не автоматического возврата по таймеру. Повтор ACK идемпотентен, другой hash —
409. Core не участвует. Возврат standalone не использует урезанный runtime snapshot.

Статус применения: publicationId/publishedVersion + runtimeUnknown либо
appliedByRuntime + lastRuntimeAck. ACK содержит server receivedAt; ACK одной
вкладки не означает применения всеми. ACK хранится 30 дней, затем состояние
неизвестно при отсутствии свежих подтверждений. При смене epoch прежние ACK
не относятся к новому режиму. Нет deliveredToCore.

Телеметрия отправляется отдельно Core: eventId, runtimeInstanceId/version,
heartbeat/configApplied/runtimeError, необязательные publicationVersion/errorCode.
Нет правил, текста задач или лишних персональных данных. Receipt Core подтверждает
только приём телеметрии. Ошибка мониторинга не мешает чтению правил из TeamOS.
В amocrm-pro-admin отдельно видны installation state, последняя активность и
сообщённая версия; отсутствие heartbeat не означает uninstall.

## Проверка следующей реализации

Нужны tenant/actor/product isolation, expired/unknown installation, uninstall при
свежем кеше, повторная установка, admin spoofing, stale revisions/epochs, duplicate
publication/import, конфликт request hash, rollback, unknown schema и сохранение
standalone. Локальная генерация/сборка подтверждает только формы и совместимость;
живые сценарии и конкретные JWT интеграции проверяются в КТ-01.03/14.

## Локальная проверка 2026-10-15

Пройдены make gen, make check-contract (238 вызовов frontend), все тесты Go gateway,
сборка TeamOS, contract:check, 2 теста нового клиента и его ESLint. Проверка
`node scripts/check-task-control-widget-contract.cjs` подтверждает соответствие
DTO legacy-правилам и SHA-256, включая покупателей, архив и unsafe CRM ID.
Существующие REST paths/schemas сравнены с HEAD: изменений нет, добавлена 21 операция.
Это локальная проверка контрактов, не работающий сервер хранения/мониторинга и не live amoCRM.
