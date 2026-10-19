# КТ-01.R09: сквозная приёмка

Дата: 16.10.2026. По поручению пользователя в текущем этапе выполняются **только
локальные проверки**. Полная приёмка не завершена: актуальная живая цепочка amoCRM
и браузерная матрица отложены. Новый редактор TeamOS для этих проверок не требуется.

## Поставка и воспроизводимость

Проверяемая бизнес-реализация после слияния R07/R08:

- Core `960c7c2c90019e89997043903785ed89970abec1`, миграции 1–30.
- Backend `4f765bbc07afbf8e4e8462993647316e4ceb3173`, company миграции 1–40.
- Виджеты `1720c6cf8abb7f0d610ed1de73dc80ae1166cafa`, Контроль задач **1.2.3**.

R09 добавляет проверочный инструментарий; SHA его коммита фиксируется отдельно
в отчёте задачи. Изменений бизнес-логики, production env и данных аккаунтов нет.

Из корня backend (Go 1.26.9, Node.js 22.12+, Python 3, npm, buf и Docker;
зависимости Distribution должны быть установлены):

```sh
python3 scripts/task-control-r09-acceptance.py contracts --output /tmp/r09-contracts
python3 scripts/task-control-r09-acceptance.py unit --output /tmp/r09-unit
python3 scripts/task-control-r09-acceptance.py artifacts --output /tmp/r09-artifacts
# Для Colima дополнительно задать DOCKER_HOST и TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE.
python3 scripts/task-control-r09-acceptance.py postgres --output /tmp/r09-postgres
```

Пути соседних checkout задаются `--core` и `--widgets`. Каждая фаза сохраняет
`evidence.json`: SHA, состояние checkout, команды, коды завершения, пути логов,
SHA-256 ZIP. Результат `passed` относится только к выбранной локальной фазе.
Наличие незакоммиченных файлов отражается в манифесте и не скрывается.
Переменные окружения и секреты в манифест не копируются.

### Результат локального этапа

- Core: полный Go race-прогон `cmd/internal/api`; отдельный PostgreSQL race-прогон
  `internal/taskcontrol` на миграциях 1–30 — успешно (3.880 s). Полный Docker
  PostgreSQL-прогон и up/down/up, включая конкурентный migrate up, тоже прошли.
  В общем прогоне пропущен только opt-in `TestDistributionTeamBridgeServer`
  (отдельный сервер для внешнего bridge-теста, не сценарий task-control).
- Company и gateway: полные Go race-прогоны — успешно. Все 25 `TestTaskControl*`
  application-сценариев с PostgreSQL и actual-widget R09 прошли (116.943 s), без skips.
- Контроль задач: **134/134** Node-теста и syntax check; Distribution: собранный
  bundle и **17/17** тестов. Node-тесты не являются браузерной приёмкой.
- Все три OpenAPI, buf и сверка 238 frontend вызовов; v1/v2/import межрепозиторные
  контракты — успешно. Сохранилось прежнее предупреждение об unused schema.
- Обычный company lint — 0 issues; lint изменений R09 с integration tag — 0 issues.
  Полный lint с этим tag дополнительно выявляет 8 прежних замечаний вне R09
  (errcheck, errorlint, goimports, unusedwrite); его нельзя считать зелёным.
- Проверены ZIP 1.2.3: целостность, manifest, standalone/managed deployment,
  отсутствие тестов и `.DS_Store`. SHA-256 standalone:
  `925fce2fd258eda0558a1a78cbb8a4624d9f74f7c6d352201efcbfaff22cbfa6`;
  managed synthetic:
  `00d72fc4c74a469abaf191dccf4c897c6995eb3a1fb003bf746cee2c8207343a`.
- CI базовых слитых SHA: [Core — 13 jobs](https://github.com/sk1fy/amocrm-pro/actions/runs/38950157315),
  [backend Go — 43 jobs](https://github.com/sk1fy/team-os-backend/actions/runs/38950165245),
  [backend contracts](https://github.com/sk1fy/team-os-backend/actions/runs/38950165229) — успешно.
  Это CI базовой реализации; новые R09-коммиты потребуют собственного прогона после push/PR.

Managed ZIP этой фазы содержит `example.invalid`: это артефакт проверки сборки,
**его нельзя устанавливать для живого managed-прогона**. Для живой приёмки нужен
отдельный ZIP со штатными HTTPS origins, новый SHA-256 и запись версии/поколения
в Core. Standalone ZIP не содержит адресов TeamOS/Core.

## Матрица синтетических проверок

| Область | Основание |
|---|---|
| JWT/grant, одноразовый proof, replay и concurrent consume, generation, роли | Core `internal/taskcontrol`; company `TestTaskControlLifecycle` |
| Link, установка, outage/unknown, epoch, revoke, standalone | `TestTaskControlLifecycle`, `TestTaskControlDynamicModeConfirmationEpochAndRollback`; widget reliability/dynamic tests |
| Общие справочники, account-scoped mapping и отозванный доступ | `TestTaskControlReferencesDurableIsolationAndRevocation`, `TestTaskControlSharedMappingPreservesAccountProvenance` |
| Импорт: preview/apply, повтор, другой tenant, изменившийся источник | `TestTaskControlImportIsolationVerificationAndReplay`; actual widget import contract |
| Draft/validate/publish, immutable история, restore и pagination >100 | `TestTaskControlR05*`, `TestTaskControlDynamicPublicationRuntimeAndCAS` |
| Оргструктура/CRM → пересчёт опубликованного intent; пустой состав | `TestTaskControlR08DynamicReleaseMembershipScopesAndEmptyProjection` |
| Конкуренция пересчёта и новой публикации | `TestTaskControlDynamicLateWorkerCannotReplaceNewPublication` |
| Session → настоящий JS-клиент → actor engine → received/applied → application API | **`TestTaskControlR09ActualWidgetRoundTrip`** |
| Stale ACK, rate limit, tenant/actor/instance isolation, retention | `TestTaskControlR07*` |
| Malformed snapshot, lease/outage/reconnect, поздние ответы и destroy | Widget protocol/reliability/dynamic/execution suites |
| Условия создания задачи, owner, Web Locks/маркеры, неизвестный POST | Widget execution R08; CRM API в этих тестах подменён |
| Standalone и managed семантика, отсутствие расширения legacy selection | Widget release semantics + domain resolver tests |
| Distribution | Собранный bundle и 17 существующих widget-app тестов; Go domain/transport регрессии |

В Core R09 исправляет покрытие CI: `internal/taskcontrol` добавлен в обязательный
PostgreSQL `integration-test` Docker target. Обычный unit run без TEST_DATABASE_URL
пропускает его DB-сценарии и сам по себе не подтверждает эту строку матрицы.

Новый межрепозиторный тест требует `WIDGET_DIR`; без него обычный company CI
явно пропускает тест. Фаза `postgres` выставляет путь и запускает тест обязательно.
Тест поднимает PostgreSQL, создаёт публикацию через application API, выдаёт сессию
по тестово подписанному proof, вызывает настоящий JS-клиент через loopback HTTP
адаптер company, сохраняет реальные события и читает application status.
Затем удаляет mapping, запускает штатный resolver и повторяет доставку пустой
actor-проекции той же публикации. Gateway, Core HTTP, amoCRM и браузер в этой
цепочке не участвуют; adapters не подменяют эти границы живой приёмкой.

## Готовность API для будущего frontend

Источник DTO/операций — `contracts/openapi/teamos.yaml`; gateway сгенерирован.
`TestTaskControlContractsHaveNoRemainingStubs` и transport suites проверяют
контракт/маршрутизацию, включая application status вместо прежнего 501.
Примеры wire payload находятся в `contracts/fixtures/task-control-*.json`.

| Действие клиента | API и особенности |
|---|---|
| Показать установку | v1 `accounts/{accountId}/installation`; installed/notInstalled/unknown различаются |
| Привязать/отозвать | v1 `connections`, link/revoke intents; TeamOS JWT + актуальное manage; proof CRM admin для предусмотренных операций |
| Выбрать состав | v2 `connections/{bindingId}/references`; search, offset/limit, department/withoutDepartment/selectedEmployeeIds; отсутствие mapping видно явно |
| Обновить справочники | refresh + чтение операции; pending/error не подменяются пустыми данными |
| Сохранить и проверить intent | v2 draft/validate; expected revisions/checksum, 409 при конфликте; preview содержит exclusions |
| Импортировать legacy | source request → preview → новый verification snapshot → apply; apply не публикует draft |
| Опубликовать/восстановить | publications/rollback; immutable publication, requestId и optimistic revisions; restore не исполняет промежуточный draft |
| История/публикации | непрозрачный cursor + limit; применяются текущие read permissions |
| Переключить режим | V1: prepared → confirmed → completed; confirmed требует reconciliation. V2: mode-preview → mode-intents с fences/proof/подтверждениями возвращает server completed и full export receipt; это не доказывает сохранение настроек amoCRM |
| Диагностика применения | application status; observedInstancesOnly, allInstancesApplied=false; ACK одной вкладки не означает всех |

Точные request/response, codes и permissions: [контракт](task-control-contract.md),
[R02](task-control-r02-access.md), [R03](task-control-r03-references.md),
[R04](task-control-r04-import.md), [R05](task-control-r05-history.md),
[R11](task-control-r11-dynamic.md), [R07](task-control-r07-diagnostics.md).
Runtime bearer не является TeamOS JWT и не разрешает управление правилами.
Виджет обращается к отдельным runtime routes; backend не создаёт задачи amoCRM.

## Остаток живой приёмки

Перед началом записать разрешённый тестовый аккаунт, карточки, actor, реальные
SHA/images, SHA-256 установленного ZIP, installation/generation, binding/epoch,
publication/runtime revision. Восстановление исходного режима и правил обязательно.

1. Штатные lifecycle/auth/link и чтение реальных справочников без подмены кешей.
2. Чтение автономного каталога → preview/verification/apply → validate/publish;
   повтор requestId и изменившийся источник без потери исходных правил.
3. Выдача реальной конфигурации, received/applied и чтение application API;
   изменение оргструктуры/mapping и открытого этапа → новая runtime revision
   без новой intent-публикации, измерение срока обновления.
4. На согласованной карточке: задача владельцу, задача другому человеку,
   просроченная/завершённая, смена владельца, две вкладки, повторный клик;
   проверить факт создания через Tasks API. Не распространять helper-гарантию
   на native/mobile/API/другое устройство.
5. Outage/reconnect/lease; revoke/standalone и восстановление. Для uninstall
   подтвердить настройку callback, настоящий сигнал и recovery, учитывая
   известный native OAuth callback R06.
6. Повторить согласованный набор в явно согласованных браузерах и версиях.
   Node/jsdom не доказывают Web Locks, SPA/native CRM и storage поведение браузера.

Прежняя [живая приёмка R06](https://github.com/sk1fy/amocrm-pro/blob/960c7c2c90019e89997043903785ed89970abec1/docs/runbooks/task-control-r06-acceptance.md)
относится к прежним SHA и не закрывает эти пункты на 1.2.3. До их выполнения
задачу R09 нельзя считать полностью принятой или переводить в review.


## Продолжение20.10.2026: browser evidence и исправления

Стенд обновлён с main Core bbd11fb / backend3f5235f, схемы30/40. В Brave
подтверждён установленный1.2.3: четыре hashes JS совпадают с managed ZIP
`01f33450a3dc7d3a7978c9051efb00719b37f047b6f39a69c966c68f09e2a942`.
Public nginx обновлён файлом main; новые telemetry/source/v2/ACK routes доступны,
изоляция private8443 и проверка Origin сохранены. Descriptor текущей generation3
подтверждён штатной CLI; received/applied/heartbeat v1 наблюдались на сервере.
Это не подтверждает живой v2 ACK.

Реальная owner-матрица на тестовой39230235 (owner10912522) выполнена:
- Без задач — клик «Сделки» блокируется, отмена оставляет карточку открытой.
- Только чужая активная task26589597/owner198392 — предупреждение о чужой задаче,
  выход блокируется. Это воспроизводит случай менеджера с задачей другого сотрудника.
- Добавлена owner task26589599/owner10912522 через форму из guard — выход разрешён.
- Owner task завершена, чужая ещё активна — выход снова блокируется.
- Обе новые test tasks завершены штатным UI, API подтвердил `is_completed=true`.
  Владелец/этап сохранены, рабочие задачи не менялись.
- Отдельная39246629 с прежней просроченной незавершённой задачей владельца
  допускает выход. Смена этапа не является проверкой `denyExit`.

Найден и локально исправлен отдельный пробел повторных напоминаний: на той же
странице закрытый list modal не появлялся снова. В1.2.4 run вызывается таймером
и после закрытия, с прежним30s throttle, проверкой активной вкладки и защитой от
параллельных долгих выборок. 136 Node tests и syntax check прошли. Новое поведение
ещё требует установки1.2.4 и живой проверки>30s; smoke1.2.3 этого не подтверждает.

Другие локальные исправления: Core empty-events reconcile вместо неверного POST
с settings[]; builder уникальных image tags и сохранение точных image tar;
аудит origins внутри ZIP; version-aware acceptance/release scripts. Полный
PostgreSQL/race webhook + amoCRM client прошёл; 9 Python audit/release tests прошли
(образ Docker в CLI-тесте подменён, настоящая Docker-сборка этим не подтверждается).
Первый общий webhook-прогон имел ошибки timing/job claims и невалидную null-fixture;
после исправления fixture целевые тесты и полный повторный прогон зелёные.

R09/R10 ещё не приняты: новые исправления требуют review/merge, server rollout,
установки1.2.4 и остатка живой матрицы выше. Registry digests/образа и reversible
full-stack rollback нельзя заменить локальными тестами. Выполнимый порядок и
условия остановки — [промпт pi](task-control-r09-r10-server-prompt.md).


Проверка совместимости1.2.4 выявила прежние allowlists до1.2.3. Исправлены Core
CLI/status/admin-read и CHECK через новую миграцию31, backend разрешает проверенную
1.2.4 в v2/session/telemetry/import. Неизвестные версии по-прежнему отклоняются.
R09 actual-widget fixture берёт operator-confirmed version из проверяемого package,
а wire comparator сверяет runtimeVersion с ним, сохраняя точное сравнение остальных
полей. PostgreSQL/race `TestTaskControlR09ActualWidgetRoundTrip` с настоящим1.2.4
прошёл: v2→engine→received/applied→application, затем пустая projection после
удаления mapping. Это loopback application test, не живая amoCRM/gateway-приёмка.
Новый целевой rollout: **Core31/company40**, установленный1.2.4 и descriptor текущей
generation. Старый стенд30/40 остаётся прежним до merge/deploy.
