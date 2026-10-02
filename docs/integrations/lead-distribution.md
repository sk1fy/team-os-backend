# Подключение распределения через Core — РС-03

Реализован серверный слой подключений, идентичностей, справочников и прав; следующие разделы фиксируют дополнения РС-04–РС-06. Существующие distribution CRUD/simulate и legacy provider остаются совместимыми.

## Владельцы и подтверждение

Company хранит company-local `distribution_bindings`, неизменяемые версии связей, текущие сопоставления сотрудников, историю и снимки зеркала Core. Внешние Core UUID не имеют FK в другую базу. TeamOS companyId берётся из проверенной сессии; роль, активность и доступ к разделу перечитываются из текущих данных.

`POST /api/v1/distribution/connections/link-intents` принимает intentId, installationId, integrationId, accountId и одноразовый widgetToken. Это server-to-server подтверждение: owner/admin TeamOS инициирует local pending, Core проверяет точный service grant и live CRM admin. Local active появляется после подтверждения exact scope Core. Существующая компания и сотрудники не создаются заново. Срок pending — 15 минут. Повтор того же intentId сначала читает результат Core, поэтому потерянный ответ не вызывает слепого повторного расходования widget JWT.

Истёкший pending требует явного `POST .../{bindingId}/revoke`, затем нового intentId. Core revoke создаёт tombstone и блокирует запоздалое подтверждение даже ещё не видимой связи. Company освобождает текущий слот после подтверждённого revoke. Недоступный Core оставляет reservation для повторной сверки; автоматически истекать неопределённые внешние исходы нельзя.

## Идентичности и справочники

- `GET /api/v1/distribution/connections` отдаёт состояние и mappingRevision/mappingAckRevision текущей компании.
- `GET /api/v1/distribution/references?bindingId=...` получает полный свежий снимок пользователей, воронок и этапов из Core. Core проходит страницы источника. TeamOS проверяет freshness, decimal ID, дубликаты. Partial/failure возвращается ошибкой, демоданные не подставляются.
- `GET/PUT .../connections/{bindingId}/mappings` читает сопоставления и подтверждает выбранный existing UUID сотрудника с реальным CRM ID.
- `POST .../{bindingId}/mappings/reconcile` переиспользует только существующие `users.external_id` при совпадении legacy account либо `user_external_identities` с точным company/account/provider. Имена и email не являются доказательством идентичности. Повторный импорт не создаёт сотрудников. Неоднозначные соответствия требуют ручного решения; удалённые/неактивные элементы недоступны.
- `POST .../{bindingId}/mappings/sync` повторяет последний durable снимок без изменения revision/payload.

UUID, графики, группы и история сотрудников сохраняются. После удаления сотрудника остаётся mapping tombstone с nullable userId и неизменяемым userIdSnapshot, а исторические версии не имеют FK на удалённого пользователя. Деактивация немедленно переводит текущее сопоставление в unavailable; реактивация сама по себе права CRM не восстанавливает.

Снимок сопоставлений фиксируется локально вместе с monotonic revision до обращения к Core. Межсетевой вызов не держит SQL company lock. Core проверяет монотонность и idempotence версии. Пока mappingAckRevision меньше mappingRevision, доступ к сделкам закрыт. Ошибка доставки может означать уже сохранённое локальное сопоставление: читать connection/mappings и использовать sync, а не создавать другого сотрудника.

## Доступ сотрудника и виджета

`POST .../{bindingId}/lead-permission` проверяет текущего TeamOS сотрудника, company/section, active exact binding, синхронизированное mapping и live CRM ACL через Core. После сетевого ответа локальные роль/раздел/mapping/binding перечитываются. Обычный employee с distribution section участвует в сценарии; admin-only fallback не используется.

Core виджет проверяет amoCRM JWT и актуальные CRM права, затем вызывает private `POST /internal/v1/distribution/widget-access` Company. Bootstrap может опускать leadId и проверяет company/mapping/section; доступ к конкретной сделке дополнительно проверяет положительный leadId и CRM ресурсный ACL в Core. Неизвестные/удалённые пользователи, чужой company/install scope, revoked binding, pending mirror и недоступная policy не дают доступ.

## Серверные ключи и развёртывание

Настроить COMPANY_DISTRIBUTION_CORE_URL (HTTPS), COMPANY_DISTRIBUTION_KEY_ID и server-only JSON-карту COMPANY_DISTRIBUTION_SERVICE_KEYS. Callback подключается на private Company HTTP listener только при наличии ключей. Не публиковать listener наружу; TLS завершается доверенным внутренним прокси. Gateway использует существующий TeamOS JWT, браузер не получает service keys или CRM OAuth.

Оператор отдельно выдаёт exact capability `widget-access` в `distribution_service_grants` для keyId/companyId/installationId. Наличие ключа без строки grant не разрешает компанию. У Core независимые grants для исходящих TeamOS операций.

Подпись HMAC-SHA256 по строкам: keyId, method, RequestURI (включая query), company UUID, installation UUID, Unix timestamp, UUID nonce, lowercase SHA256 raw body. Заголовки X-Distribution-Key-Id/Company/Installation/Timestamp/Nonce/Signature. Допуск времени ±60 секунд, nonce хранится PostgreSQL 5 минут; replay отклоняется и после перезапуска процесса. Ротация: добавить новый keyId на обе стороны и grants, переключить sender, затем отключить старые grants и убрать старый ключ. Redirect запрещён. Upstream bodies и токены не попадают в клиентские ошибки.

Применить migration 000022 через обычный migrate runner. Down предназначен для пустого тестового подключения, не для удаления production истории. При rollback сервиса сохранять таблицы и закрывать grants.

## Проверка

`make gen`, `make check-contract`, Go тесты Company/Gateway и профильная PostgreSQL integration suite проверяют реальные миграции, duplicate/concurrent link, UUID-preserving повторный импорт, tenant rejection, mapping lifecycle/tombstone/history, partial freshness, durable mirror retry, employee ACL, concurrent section revoke, service grant/signature/nonce replay, revoke/rebind. Colima требует DOCKER_HOST с адресом существующего Docker context и TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock. Тестовая БД изолирована и удаляется тестом.

Живые OAuth/reauth/диагностика на amoCRM тестовом аккаунте требуют предоставленного тестового account/integration/installation; локальные проверки не считаются этой приёмкой.

## РС-04: историческая граница проверки business decision (до РС-06)

Private `POST /internal/v1/distribution/validate-decision` использует отдельную capability `decision-validation` (migration 000023). Grant `widget-access` её не разрешает. Scope, HMAC, durable nonce, active exact binding и verified current target mapping проверяются сервером. Контракт — [distribution-internal.yaml](../../contracts/openapi/distribution-internal.yaml).

Ответ возвращает exact operationId/decisionId/workerFence и **allowed=false**. Для валидного scope причина `decision_not_ready`: authoritative rule/episode/decision registry, доступность по графику и отмена принадлежат РС-06 и ещё не реализованы. Проверка одного mapping не заменяет эти business preconditions. Реальный Core worker должен остановиться до PATCH; положительные контролируемые test fixtures проверяют исполнителя, но не означают готовность автоматического распределения в production.

После реализации владельцем очереди полного registry этот endpoint сможет выпускать короткий allow-token (максимум 5 секунд) только после сверки всех business revisions, episode/rule/claim/cancellation и текущего target. Без такой реализации этапная интеграционная приёмка 04.3 остаётся зависимой от РС-06; не включать реальное выполнение через заглушку, флаг или admin-only обход.

Выдача capability производится отдельно оператором с правом администрирования Company DB. Ни привязка установки, ни наличие widget-access, ни знание ключа не создают этот grant автоматически. Использовать параметризованный запрос с заранее проверенными keyId/companyId/bindingId; ключевой secret остаётся только в серверном окружении:

```sql
INSERT INTO distribution_service_grants(key_id, company_id, installation_id, capability)
SELECT $1, b.company_id, b.installation_id, 'decision-validation'
FROM distribution_bindings b
JOIN companies c ON c.id = b.company_id
WHERE b.company_id = $2 AND b.id = $3 AND b.state = 'active' AND c.status = 'active'
ON CONFLICT(key_id, company_id, installation_id, capability)
DO UPDATE SET active = true;
```

Нулевой результат не разрешает другой scope. Отзыв — `UPDATE distribution_service_grants SET active=false WHERE key_id=$1 AND company_id=$2 AND installation_id=$3 AND capability='decision-validation'`. Ротация создаёт отдельный grant нового keyId и отзывает старый; права widget-access не расширяются.

Для пользовательского решения callback получает подписанный Core actor `{kind:'user', teamosUserId, crmUserId}`: Company перечитывает active сотрудника, distribution section и точное verified CRM mapping. Обычный employee разрешён этой проверкой; owner/admin роль не требуется. Core отдельно проверяет live CRM ресурсный ACL. Actor `system` не содержит пользовательских identity, но также не получает allow без registry. `operator` и смешанные/неполные principals отклоняются. Срок исходного decision обязателен, уже истёкший или превышающий 15 минут decision не принимается.

## РС-05: durable delivery и наблюдаемая проекция

Company принимает подписанные `POST /internal/v1/distribution/events` и `/results` только с отдельными capabilities `event-delivery` и `result-delivery`. Существующий `widget-access` или `decision-validation` не расширяется автоматически. Выдача каждому доверенному key/company/install осуществляется оператором:

```sql
INSERT INTO distribution_service_grants(key_id,company_id,installation_id,capability)
VALUES (:key_id,:company_id,:installation_id,'event-delivery'),
       (:key_id,:company_id,:installation_id,'result-delivery');
```

Core должен направлять HTTPS callback на Company, минуя публичный Gateway. Подписанный consumer — `teamos-distribution-v1`, namespace событий и результатов различается. HTTP 202 выдаётся после COMMIT inbox и содержит точные messageId/receiptId/acceptedAt. ACK означает сохранение, обработка может быть отложена. Потерянный ACK повторяется с новым nonce и исходным frozen envelope: неизменный messageId возвращает тот же receipt. Другой payload для messageId или eventId отвергается с 409. Перевыпуск событий с новым messageId не запускает повторную бизнес-обработку.

При настроенном COMPANY_DISTRIBUTION_CORE_URL worker каждые пять секунд обрабатывает не более двадцати inbox rows и двадцати зарегистрированных операций. Inbox lease хранится в PostgreSQL, просроченная аренда восстанавливается после перезапуска; повторные ошибки имеют ограниченный backoff до пяти минут. Логи содержат backlog, blocked, возраст старейшей доставки, количество незавершённых операций и коды ошибок, без токенов и исходных CRM payload. Наличие 202 при blocked означает необходимость восстановления источника/регистрации, а не завершение распределения.

Lead head — наблюдаемое актуальное состояние account/lead. Его поколение выделяется до внешнего GET; сеть никогда не вызывается под SQL lock. Перед применением сверяются lease, поколение, Core observationRevision и текущая immutable binding. Старые in-flight ответы и события от отозванной связи записываются как ignored и не меняют текущую проекцию. HTTP 204 источника означает observed_missing (`absent`, `not_found_or_deleted`), отдельное от доказанного удаления. Неавторизованный/недоступный источник оставляет обработку blocked; 403/404 не превращаются в удаление.

Наблюдаемый выход/удаление/отсутствие отменяет прежний кандидат. Наблюдаемый переход создаёт новый UUID и последовательность входа. Первый вход без подтверждённого source stage остаётся needs_configuration. Повторный вход в ту же секунду невозможно восстановить по timestamp: при противоречивом source evidence кандидат получает ambiguous_reentry и требует следующего бизнес-решения. Ответственный, изменившийся в прежнем этапе, не создаёт нового входа. РС-06 будет использовать current_entry_id, sequence, pipeline/status и evidence; это ещё не очередь правил, не round-robin и не claim/cursor.

Результат сам не создаёт business decision. Внутренний доверенный вызов RegisterDistributionOperationMirror сначала получает существующую Core operation и проверяет точные scope/lead/episode/decision/rule/group/event/correlation/revisions/target; затем сохраняет регистрацию. Результат, пришедший раньше, остаётся durable blocked и пробуждается после регистрации. Фоновый GET выполняется только для зарегистрированных unfinished operations, включая их историческую immutable связь после rebind/revoke.

Dedup результата использует operationId+resultVersion и canonical Operation hash: pull и push с другими envelope timestamps/messageId совпадают по семантике. Hash полного envelope отдельно защищает messageId. Frozen результаты РС-04 без leadId допускаются как legacy pending; leadId добавляется в semantic Operation исключительно из зарегистрированного immutable Core mirror. Исторический envelope не переписывается, snapshot не является источником полномочий на leadId. Меньшая версия сохраняет историю и не регрессирует latest mirror. Изменение содержания той же версии — conflict. Terminal/releasable требует согласованных effect/evidence/outcome; наблюдение само по себе не доказывает авторство PATCH. Mirror никогда не освобождает current entry, claim или cursor: правила соответствующего эффекта принадлежат РС-06. validate-decision продолжает отвечать decision_not_ready.

Проверки РС-05: реальные PostgreSQL migrations 1–24, signed capability/nonce/tenant receiver, commit-before-ACK и lost ACK, restart, semantic duplicate/conflict, выход/повторный вход/observed_missing, перестановка live GET, pending result до регистрации, pull/push version dedup и исторический GET после revoke. Live amoCRM отсутствует; production сделки не изменялись.

## РС-06: authoritative очередь, графики и решения

Migration 000025 добавляет company settings, реальные правила, сохраняемую очередь,
отдельные group/account+lead claims, подтверждённый cursor, immutable history и
сохраняемые control requests. Legacy `distribution_events` и simulate не являются
источниками cursor и не создают реальные CRM-команды.

Публичный канонический REST (`contracts/openapi/teamos.yaml`) содержит settings,
rules, availability, queue и history. Gateway/Company protobuf и REST адаптеры
генерируются стандартным `make gen`. Настройки и правила изменяют текущие
owner/admin, чтение требует текущего distribution section. Company берётся из
проверенной сессии. CRM leadId в очереди nullable: он появляется только после
проверки текущего verified employee mapping и живого Core resource permission;
owner TeamOS не получает CRM права автоматически. Общий бюджет проверки страницы
— три секунды; недоступные/непроверенные элементы получают `leadId:null`. AccountId
— существующая метаинформация подключения. Публичная history отдаёт безопасные
state/reason/time и `payload:{}`; frozen commands, исходные/подтверждённые CRM
snapshot и приватные control bodies доступны серверу, не раскрываются этим API.

IANA timezone задаётся явно; `Local` и отсутствующий пояс не превращаются в UTC.
Чистая schedule availability проверяет текущую и предыдущую локальные даты,
`[start,end)`, ночные хвосты, рабочие overrides и nonwork exceptions, которые
обрывают вчерашний хвост в начале своей даты. Шаблонный выходной хвост не обрывает.
Нулевая смена, malformed legacy data и gap/fold DST endpoints исключают интервал.
Недельные/циклические графики считаются по гражданским календарным датам.
Ближайший старт ищется не далее 35 локальных дней; отсутствие результата имеет
явную причину, а не обещание бесконечного ожидания. Активность TeamOS, внешнее
удаление, disabled membership, exact account mapping/ACK и полный свежий Core
справочник CRM пользователей дают отдельные причины недоступности через API.

Одно активное правило account/pipeline/status защищено глобальным unique index.
Первая активация сохраняет watermark: старые наблюдаемые сделки автоматически не
импортируются. Пауза и редактирование watermark не сбрасывают; новые подходящие
входы существовавшего правила сохраняются и при паузе. При нескольких исторических
правилах новый вход детерминированно выбирает активное, затем последнее ранее
активированное paused правило. Уже сохранённая очередь владельца не меняет.
Активация/перенос точки блокируется, пока её операции не получили выясненный исход.
Только доказанные `created_in_stage`/`observed_transition` entries допускаются в
очередь; snapshot reconciliation и ambiguous reentry не выдумывают новый вход.

Серверный цикл каждые пять секунд обрабатывает bounded inbox, затем не более 20
queue steps и зарегистрированные mirrors. Admission ограничен 200 entries;
пересчёт dirty availability — 20 компаний/200 queue rows за проход, с persisted
revision, поэтому wake не теряется между порциями. `next_attempt_at`, причины,
leases и история хранятся в PostgreSQL. Изменение графика/исключений, сотрудника,
группы, сопоставления, правила, timezone или binding/company status записывает
revision до commit. Ожидание ночной смены переживает перезапуск и не зависит от
открытого браузера. При отсутствии пригодного сотрудника/известной ближайшей смены
состояние требует настройки; bounded retry сохраняет элемент для пересчёта.

Порядок группы устойчив по createdAt+UUID: leased внешний GET старшего элемента
не позволяет младшему его обогнать. Группы независимы. Group claim и глобальный
account+lead claim сохраняются до доказанного результата, включая новый вход и
повторное подключение. Сеть выполняется без SQL locks; любой записывающий поздний
worker проверяет свой актуальный lease. Frozen assignment/operation/key и исходное
состояние фиксируются до POST. Потерянный ACK приводит к GET и повтору той же
операции/тела/key; GET404 не разрешает создать другое назначение.

Private validate-decision теперь проверяет реальный registry. Совпадают точные
operation/decision/episode/rule/group/scope/revisions/recipient/kind/expiry; затем
перечитываются authoritative active company/binding/mapping, membership/schedule,
rule, head/current entry и claims. Grant привязан к Core worker fence и действует
не более пяти секунд, до конца смены и expiry решения. Внешняя race после grant
не обещает CRM CAS. Без registry endpoint продолжает отвечать decision_not_ready.
РС-04 historical deny seam заменён этой реальной проверкой, без admin-only bypass.

Keep доступного владельца включён по умолчанию, аудируется и cursor не расходует.
При keep=false выбранный тот же владелец подтверждается `no_change/already_target`
и расходует один turn без PATCH. Cursor меняется лишь в той же транзакции с
подтверждённым результатом и settlement once; selection order сохраняет следующую
границу даже после удаления последнего выбранного участника. Неуспешный turn не
двигает cursor. Постоянные pre-send errors закрывают ошибочный элемент и освобождают
group, позволяя следующему; только явные source/decision/recipient/policy причины
и отмена без попытки разрешают новый расчёт того же entry.

Первый валидный Core GET регистрирует mirror из собственного frozen command даже
для unfinished операции; push/pull используют прежний version dedup. Business
settlement применяется только для совпавшей текущей операции и валидного terminal
evidence, атомарно с результатом. Historical или дублирующий результат не
освобождает новый claim. Для sent/unknown отмена сохраняется и сверяется до
выясненного исхода. Control request body/key/expectedResultVersion записываются
до HTTP; UUID ключа определяется persisted base+action+version, повтор читает
сохранённое тело. Старый delayed control не проходит Core version CAS. Unknown
без надёжного ACK удерживает guards: GET target, таймер и ручная пауза их не
освобождают. Durable ACK с последующим подтверждением может завершиться через
reconcile без повторного PATCH.

Локальные проверки включают pure calendar/table cases, PostgreSQL lease/FIFO/wake/
immutable history/point ownership/privacy, night wait → restart → real registry →
confirmed settlement, полную Company application suite и подписанный HTTP.
Отдельная парная acceptance запускает реальные Core HTTP/Store/worker и TeamOS
PG/registry/подписанный callback с контролируемым CRM transport; это не live amoCRM.
Живой OAuth/E2E РС-03.1.2 остаётся незавершённым. UI, виджет и production включение
относятся к следующим этапам; никаких production CRM изменений эти проверки не
выполняют.

При rolling upgrade старый РС-05 writer может сохранить entry без новых
sourceReceived/sourceOccurred timestamps. Такой entry не допускается к
автоматическому назначению даже если локально создан после активации правила:
оба доверенных времени должны быть известны и не раньше initial watermark.
Новый writer сохраняет provenance из подписанного сообщения; неизвестное время
оставляет `source_time_unknown / needs_configuration`. Известное старое событие,
доставленное/обработанное позднее, также не обходит cutoff. Timestamp применяется
лишь как консервативная граница первой активации, а не идентичность эпизода.

Проверки текущего РС-06: `make gen`, `make test`, `make check-contract` (включая
199 вызовов фронтенда) пройдены; Company lint — 0 issues. Полная PG application
suite — 70.298 s, signed transport — 8.616 s до последнего cutoff delta. Текущая
профильная PG suite с race detector после строгого known-source cutoff и
immutable control requests — 10.390 s / 5.083 s; отдельный current delta profile
night/lease/200-row wake/история/права/cutoff — 5.521 s. Проверка двух соединений
покрывает schedule writer, ожидающий revision, и чтение committed schedule;
порционный wake для 205 новых entries сохраняется между проходами. Night profile
также проверяет точный повтор control body/key после reload, отдельный key для
следующей resultVersion и запрет изменения frozen body, permanent pre-send
failure не блокирует следующую готовую сделку.

Финальный парный Core↔TeamOS профиль с действительными HTTP/HMAC/nonce/grants,
двумя PG базами и controlled CRM прошёл после strict cutoff: Team 22.84 s, Core
44.79 s. Девять сценариев охватывают assignment/restart, keep/no cursor,
already_target/один turn без PATCH, concurrency, настоящий five-second grant
expiry перед dispatch и новое решение, CRM inactivity, ручную смену владельца,
durable ACK+reconcile без второго PATCH и unknown с удержанием claims.
