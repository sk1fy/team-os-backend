# КТ-01.R11: динамические намерения и runtime v2

Company сохраняет намерение выбора отдельно от вычисленных CRM ID. Core отвечает
за установку и CRM-справочники; company — за сотрудников, отделы, подтверждённые
account-scoped связи, публикации и resolver. UI редактора не меняется.

## Выбор и совместимость

`intentRules` содержит строгий union `{kind:"dynamic",rule:RuleV2}` или
`{kind:"legacyExplicit",rule:RuleV1}`. Пример:
[task-control-intent-v2.json](../../contracts/fixtures/task-control-intent-v2.json).
OpenAPI `contracts/openapi/teamos.yaml` — источник wire-контракта. Intent/payload/resolved checksum — 64 hex без префикса; CRM reference revision имеет префикс `sha256:`.

Audience `allLinked` включает активных сотрудников компании с однозначной
подтверждённой активной CRM-связью текущего аккаунта. `selected` объединяет
employeeIds и departments без дублей. Для каждого отдела includeDescendants
обязателен: true включает текущих и будущих потомков, false — только прямых
участников. Учитываются явные назначения отделов и отдел должности. «Без отдела»
выбирается перечислением людей. Отдельно выбранный человек остаётся в выборке
после перехода в другой отдел. Сопоставление по имени/email не выполняется.

Leads `allOpen` включает будущие воронки и открытые этапы; selected pipelines со
stages `allOpen` — будущие открытые этапы выбранных воронок. Явные statusIds
сохраняют фиксированный состав. Won/lost (142/143) для dynamic исключены.
Customers disabled/all/selected — отдельная сущность; название воронки ничего
не включает автоматически. Пустой selected не означает «все» и не принимается.
Пустой вычисленный состав даёт [] и диагностическую причину, guard выключается.

LegacyExplicit сохраняет исходные CRM IDs и прежнюю семантику закрытых этапов;
он не превращается в allLinked и не требует сотрудника TeamOS для старого CRM ID.
Неизвестные/лишние поля union, повторные JSON-ключи и несовместимые режимы
отклоняются. Сохранённые устаревшие ссылки не теряют intent; validate показывает
причины, publish блокирует недоступные explicit ссылки активных правил. После
публикации удалённые ссылки исключаются resolver, с диагностикой. Disabled и
archived не исполняются, структура продолжает проверяться.

## Включение и API

Нужны Core migration 000027, company migration 000037, company/gateway и пакет
`rkrs_lead_notasks` 1.2.0. Оператор подтверждает развёрнутый пакет командой
Core `task-control-runtime-descriptor` для точного installation/generation.
Подробности — Core runbook `docs/runbooks/task-control-mvp.md`. Строка версии
или capabilities из браузера сама по себе не разрешает включить v2. Подтверждение
1.1.4 не разрешает динамику. Reinstall требует нового подтверждения.

Переход schema v1 → v2 разрешён в standalone: завершить прежнюю смену режима,
затем POST `/api/v2/task-control/connections/{bindingId}/activate` с requestId и
expectedBindingRevision/expectedModeEpoch. Активация меняет epoch, отзывает сессии,
сохраняет прежние явные правила как legacyExplicit. Опубликовать intent v2, затем
явно перейти в managed. Это исключает подмену исполнения активной v1-публикации.

Для `/api/v2/task-control/connections/{bindingId}` реализованы:

- GET/PUT `/draft`: намерение, revision/checksum; запись по CAS и scope fences.
- POST `/validate`: expectedDraftRevision → состав preview, sourceRevisions, issues.
- POST `/publications`: idempotent requestId, expectedDraftRevision/checksum/fences;
  publication и первый snapshot создаются атомарно на актуальных источниках.
- GET `/publications`, GET `/publications/{id}`: история неизменяемых намерений.
- POST `/rollback`: новая публикация исторического intent с текущими источниками.
- GET `/publications/{id}/application`: текущая runtime revision, ACK и stale.
- POST `/mode-preview`, POST `/mode-intents`: полный экспорт и подтверждённая
  смена режима с checksum/sourceRevisions fences, явной потерей динамики и
  отдельным подтверждением пустого экспорта. Ни actor-проекция, ни черновик
  не подменяют полный экспорт. Intent сохраняется для возврата в managed.

V1 не выдаёт v2 и не принимает запись поверх v2-intent (`requires_v2`). Сессия
создаётся прежним `/api/v1/widget/task-control/sessions`, дополнительно передаются
runtimeVersion 1.2.0 и capabilities `["dynamicRulesV2"]`. Сервер возвращает
runtimeSchemaVersion, выбранный по binding и проверенному descriptor.

## Пересчёт, доставка, свежесть

Immutable snapshot связан с publicationId/version, intentChecksum, scope/epoch,
CRM/organization/mapping revisions, resolverVersion, runtimeRevision и checksum
полного разрешённого набора. Публикация и черновик при пересчёте не меняются.
Изменение provenance создаёт новую runtimeRevision даже при прежнем payload;
повтор той же задачи её не увеличивает. Новая подтверждённая свежесть того же
источника обновляет lease metadata, не immutable snapshot.

Изменение org/mapping пишет durable append-only outbox. Worker забирает события
каждые 2 секунды и делает reconciliation всех active managed bindings раз в
60 секунд. R03 worker обновляет CRM при возрасте 120 секунд через Core provider.
Account advisory lock, scope/epoch/active-publication проверки и блокировка
строки source revisions защищают публикацию и пересчёт от поздних результатов.
Worker читает опубликованный intent, а не draft.

При здоровых сервисах: CRM change → resolver ≤210 секунд (120+60+30),
org/mapping → resolver ≤60 секунд, видимая вкладка получает его ещё ≤60 секунд.
При outage/429 эти сроки не обещаются. Новый runtime bootstrap при stale источнике
отвечает 503; старый снимок живёт только до исходной lease, затем fail-open с
диагностикой stale. Нет переключения на standalone. Неизвестные customers
блокируют customer-зависимую публикацию; lead-only от них не зависит.

GET `/api/v2/task-control/runtime/bootstrap` возвращает actor-проекцию без intent
и оргструктуры, с payloadChecksum. validUntil не позже min(issuedAt+5min,
verifiedSourceFreshUntil). Widget проверяет scope/actor/UUID/schema/hashes,
возрастающую runtimeRevision, timestamps и монотонную локальную lease. Новая
revision той же publication применяется engine. Равная revision с другим hash
отклоняется; v2 → v1 в том же scope запрещён.

POST `/api/v2/task-control/runtime/ack` принимает scope/runtimeInstanceId/actorId,
snapshot/publication identity, runtimeRevision, payloadChecksum, appliedAt и
runtimeVersion. Actor сверяется с сессией; проверяется выданная этому actor
проекция. ACK отправляется после применения engine. Исторический ACK не
подтверждает текущую revision, повтор того же hash идемпотентен и обновляет
receivedAt. Применённость конкретной вкладки актуальна 5 минут; история 30 дней.
ACK не продлевает lease.

Лимиты: 1000 правил, 1000 users/pipelines/statuses на правило, 1 MiB intent и
runtime payload; org ≤10000 сотрудников/1000 отделов, диагностика ≤10000 issues.
Переполнение блокирует новый снимок, не усечение или расширение «на всех».
Standalone продолжает исполнять локальные явные правила без TeamOS; динамику
отделов TeamOS автономный пакет не обещает.

## Проверки и выпуск

Локальные проверки включают domain race/повторные прогоны канонизации, company и
gateway tests/race/lint, PostgreSQL integration с CAS/поздним worker, сменой
отдела/отвязкой, новыми этапами, outage/recovery, ACK heartbeat, реальными
mode proof/epoch и rollback, а также widget tests. Контракты генерируются штатно;
совместимость checksum v1 проверяется общим скриптом. Сквозной скрипт
`scripts/check-task-control-v2-widget-contract.cjs` передаёт JSON, сформированный
Go-моделью, в настоящий клиент виджета, engine и ACK; та же fixture проверяется
OpenAPI и checksum-функциями company. Core migration 27 и company
migration 37 проверяются на изолированном PostgreSQL, включая down/up без v2-данных.
Company down 37 специально запрещён при сохранённых v2 intent/publications:
удаление schema marker не является миграцией назад в v1. Для отката приложения
сохранить схему и выполнять отдельную явную миграцию намерений; самовольного
переосмысления v2 как v1 нет.

Проверки используют синтетические Core assertions и CRM snapshots. Live amoCRM,
production migrations, установка пакета и операторское подтверждение descriptor
в окружении пользователя не выполнялись; это последующие rollout/приёмка R09–R10.
