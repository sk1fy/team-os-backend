# КТ-01.R03 — общий CRM provider и API выбора

Реализация 16.10.2026. Core читает CRM через существующий amoCRM client, OAuth и
account limiter; TeamOS company добавляет свои employees, departments и mapping.
Distribution не является условием работы. Правила, публикации и вычисление
динамических областей действия остаются в company; resolver — R11, импорт
legacy-каталога — R04. Standalone продолжает прямое same-origin чтение CRM.

## Источник и права

`GET /internal/task-control/accounts/{accountId}/references` — внутренний Core
server-to-server endpoint, без query/body, с существующими HMAC X-Task-Control-*
headers и durable nonce. Подпись включает точный URI. Не публиковать путь в
публичном ingress. Отдельная capability `references_read` в service grant по
умолчанию выключена, включается оператором явно; installation.read её не заменяет.
Core проверяет компанию, аккаунт, integration, installation, generation и grant
revision до и после чтения, включая cache hit. OAuth identity change инвалидирует
generation. Пользовательский runtime bearer не открывает этот API.

DTO `ReferenceSnapshotV1`: schemaVersion=1; полный scope, revision `sha256:…`,
provenance `{source:coreAmocrm,fetchStartedAt,fetchedAt}`, freshUntil, complete=true;
users (имя/активность/groupId), полные groups, pipelines (sort), statuses
(sort/color/kind), customers (availability/statuses/checkedAt/errorCode).
Email, resource rights и OAuth secrets в ответ не входят. Lead stages 142/143 —
won/lost по CRM identity; названия воронок не определяют тип сущности.

Core читает все страницы до подтверждённого конца. Ошибка страницы, цикл, дубли,
неизвестная активность или превышение лимита не превращаются в частичный snapshot.
Лимиты: 1000 users/groups/pipelines/customer statuses и 1000 statuses на pipeline,
4 MiB полного ответа, 30 секунд на чтение. Groups берутся из account source,
сохраняются и пустые группы. Customers 401/403/timeout — unknown; unsupported
требует подтверждённого отсутствия возможности аккаунта. Unknown не выдаётся за [].

Core cache разделён по security scope, schema и grant revision, до 256 scopes;
singleflight объединяет конкурентные чтения. Refresh при возрасте ≥120 секунд,
TTL 300 секунд от fetchStartedAt, cache hit не продлевает TTL. Hash рассчитан по
отсортированному semantic DTO с JCS-строками и null, без времени/transport error.
Одинаковые данные сохраняют revision. Company независимо проверяет hash,
полноту, scope, свежесть и лимит тела. Общий fixture с Unicode и максимальным
safe CRM ID проверяется обеими реализациями; дополнительный пример сохранён отдельно.

## API для будущего редактора

| Метод | Путь | Результат |
|---|---|---|
| GET | `/api/v2/task-control/connections/{bindingId}/references` | 200: view и реальное состояние источника |
| POST | `…/references/refresh` | 202: coalesced refreshId, pending |
| GET | `…/references/refresh/{refreshId}` | pending/completed/failed, errorCode, времена |

GET требует taskControl.read; POST refresh — текущий owner/admin + manage.
Scope берётся из текущей компании и binding. Чужой binding/job — 404, отзыв — 403.
GET не ждёт CRM: неизвестный/устаревший источник ставит ограниченное обновление.
Company хранит полный проверенный снимок отдельно от последней ошибки. Состояния:
fresh, stale, refreshing, unavailable, error. Источник и ошибки видны даже если
обновление не удалось; подтверждённый отказ Core очищает сохранённые CRM-данные.
Stale данные пригодны для отображения, но не для validate/publish.

Параметры GET: search (до 200 символов по имени/должности), departmentId либо
withoutDepartment=true; offset (0 по умолчанию), limit (100 по умолчанию, до 1000),
selectedEmployeeIds (до 1000 UUID, через запятую). Total — точное число после
фильтров, до страницы. SelectedEmployees возвращаются отдельно независимо от
страницы/поиска/отдела, неизвестный выбранный ID получает employee_missing.
Общий view ограничен 7 MiB, gRPC transport — 8 MiB; превышение даёт явный 413.
CRM-часть полная в пределах лимитов, поэтому ID воронок/этапов не теряются за
пределами страницы сотрудников. Ограничение проекции TeamOS — 10000 сотрудников
и 1000 отделов, превышение даёт 413 references_too_large вместо усечения.

Employees содержат employeeId, name, positionName, departmentIds, crmUserId,
eligible/ineligibleReason. Account — из scope, не из входного employee DTO.
Отделы включают прямые назначения и department должности; [] означает «Без отдела».
CRM groups не заменяют оргструктуру TeamOS. Existing amo user sync записывает
mapping для новых сотрудников при создании из успешно прочитанного аккаунта;
совпадение email/имени не подтверждает mapping. Смена аккаунта во время sync
отклоняется. Исторические импорты без записанного account не связываются автоматически:
они остаются mapping_missing без подтверждённой связи. Уже проверенный mapping
TeamOS переиспользуется через общую проекцию, включая verified identity data
существующего mapping Distribution. Права и установка Distribution не наследуются;
без Distribution работают собственные mapping новых импортов. Удаление или изменение
подтверждённой связи отражается в проекции и mappingRevision.

Причины недоступности: mapping_missing/mapping_ambiguous, employee_inactive,
crm_user_missing/crm_user_inactive, references_unavailable, employee_missing.
OrganizationRevision и mappingRevision — монотонные int64, изменяются триггерами
в той же транзакции, что org/mapping. View читается в repeatable-read snapshot.
Счётчиков активных сделок нет: отсутствие аналитики не подменено нулём.

V1 `/api/v1/task-control/connections/{bindingId}/references` теперь отдаёт прежнюю
форму как совместимую проекцию того же provider. При unknown customers или без
fresh источника — 503, потому что старый boolean customersSupported не способен
представить unknown. SourceSnapshotId этой проекции относится к CRM revision,
не является импортированным legacy catalog snapshot для R04.

## Worker, публикация и восстановление

Refresh jobs сохраняются в PostgreSQL и объединяются для одного scope. Четыре worker
восстанавливаются после рестарта и опрашивают очередь раз в 2 секунды. Отдельный sweep
активных bindings работает раз в минуту. Lease job — 45 секунд; перед завершением worker блокирует
запись и проверяет принадлежность lease. Поздний ответ не перезаписывает новую lease,
более свежий fetch, другой epoch или переустановку. Завершённые jobs хранятся 7 дней,
очистка — до 1000 строк за sweep. Результат с ошибкой не становится current snapshot.

Validate/publish активных правил используют только проверенный fresh snapshot
с текущим scope, под тем же account lock, что публикация и смена режима.
Проверяются существование/активность пользователей, pipeline и связь status→pipeline;
покупатели требуют supported и существующих customer statuses. Unknown customers
не мешает lead-only правилу. Archived/disabled reference selections не блокируют
публикацию; структурные ошибки/UID проверяются всегда. Legacy explicit CRM IDs
не требуют employee mapping и сохраняют возможность выбрать закрытые этапы.
Validate возвращает конкретную referencesRevision и issues с путями.
Publish заново проверяет источник; при ошибке publication/version не создаётся.

## Ввод в эксплуатацию и проверки

Сначала Core migration 000026 и company migrations 000035–000036, затем новые binaries;
явно выдать references capability нужному company/account/integration grant.
Существующие grants не расширяются миграцией. Новых обязательных секретов нет;
используются действующие scoped HMAC credentials. Внутренний proxy должен разрешать
новый references path только company service. Клиентам можно переходить на v2
независимо от будущего редактора и runtime v2.

Проверки включают более 100 пользователей, переполнение 1000, ошибку второй
страницы, пустые группы, unknown/unsupported customers, scope/grant/replay/reinstall,
OAuth identity A→B→A, lease CAS, асинхронное завершение после epoch change,
пагинацию 115 employees и selected ID вне страницы, чужой account mapping,
отзыв доступа, numeric revisions, fresh validate/publish и legacy checksum.
PostgreSQL и синтетические ключи проверяют локальную интеграцию; live amoCRM,
production миграции и редактор TeamOS не заявляются выполненными этой задачей.

Проверено локально: company/gateway с race detector и golangci-lint (0 замечаний);
PostgreSQL TaskControl lifecycle/reference/shared-mapping и регрессия импорта amo users;
Core provider/amoCRM/TaskControl/Distribution с race detector и PostgreSQL, 50 пакетов
Core и go vet; make gen, make check-contract (238 frontend-вызовов), общий Unicode
fixture/hash, legacy checksum. Миграции Core проверены up→down→up; company — все
миграции 1–36 и down/up для новых 35–36.

Примеры будущего UI в contracts/fixtures: task-control-reference-selection-v2.json
(fresh view, сотрудник без отдела, удалённый selected ID),
task-control-reference-refresh-v2.json (failed refresh с references_denied),
task-control-references-core-v1.json (полный CRM wire с проверенным SHA-256).
