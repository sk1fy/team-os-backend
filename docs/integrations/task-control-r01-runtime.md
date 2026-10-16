# КТ-01.R01 — runtime-сессия, кеш и совместимость

Реализация 16.10.2026: TeamOS company/gateway и runtime/bridge пакета
`rkrs_lead_notasks` 1.1.4. UI не меняется. Core JWT verifier, durable replay store,
Ed25519 assertion и worker очистки используются существующие.

## Контракт и время жизни

Цепочка: свежий amoCRM disposable JWT → Core `/api/v1/widget/task-control/auth`
→ одноразовый assertion (60 секунд) → TeamOS `/api/v1/widget/task-control/sessions`
→ opaque bearer (32 random bytes, base64url 43 символа, TTL 300 секунд).
В company сохраняется SHA-256 bearer; JWT передаётся только Core.
Каждый повтор обмена начинает цепочку с нового disposable token/assertion/requestId.
Сессии не используют cookies и не сохраняются в localStorage/sessionStorage.

`GET /api/v1/widget/task-control/bootstrap` дополнен необязательными для старых
клиентов полями `runtimeSchemaVersion:1`, `actorId`, `issuedAt`, `validUntil`.
DTO/OpenAPI сгенерированы штатно. Пример дополнительных метаданных:

```json
{
  "runtimeSchemaVersion": 1,
  "actorId": "7",
  "issuedAt": "2026-10-16T12:00:00Z",
  "validUntil": "2026-10-16T12:05:00Z",
  "minRuntimeVersion": "1.1.0"
}
```

Полный ответ сохраняет scope/mode/state/installation/publication прежнего v1 API.
V1 publication checksum и семантика явных правил не изменены.
`minRuntimeVersion` — минимальная версия runtime, а не текущая версия ZIP или
schema. Пакет 1.1.4 принимает минимум 1.1.0, 1.1.3 и 1.1.4; будущую/некорректную
версию отклоняет. Prerelease не считается стабильной совместимой версией.

Runtime кеширует только в памяти вкладки, в пределах полного scope и actorId.
Lease максимум 300 секунд, ограничена issuedAt/validUntil и оставшимся временем
по серверной дате; после приёма отсчитывается монотонным performance.now.
Retry, ошибки валидации, сетевой сбой и перевод системных часов назад не продлевают
lease. Отдельный таймер снимает уже применённые правила при её окончании.
Для старого v1 backend без всех трёх новых полей действует локальная lease 300 секунд
от успешного получения. Это позволяет поэтапное обновление backend/пакета.
Cold reload offline не восстанавливает правила с диска: fail-open, без standalone fallback.

## Renewal, ошибки и вкладки

Renewal начинается при остатке сессии ≤15 секунд. Concurrent refresh в одной
вкладке объединён в один Promise; разные вкладки получают независимые bearer.
Bootstrap 401 допускает один новый обмен и повтор bootstrap. Повторный 401,
403/404 очищает сессию и кеш; 403 не запускает повторный обмен. Изменение scope,
actor, binding revision или epoch не сохраняет старый кеш при неудачной новой выдаче.
Destroy увеличивает generation, очищает память и отбрасывает поздний ответ.

Транзиентные 0 (network/timeout), 408/429/502/503/504: максимум 3 попытки refresh,
между ними jitter 50–100% от 1 и 2 секунд. Retry-After используется с верхней
границей 300 секунд. После исчерпания — backoff polling до 300 секунд; успешный
refresh сбрасывает backoff. Обычный poll 60 секунд, только видимая вкладка.
Online/pageshow/visibilitychange запускают немедленную проверку; скрытие не
останавливает истечение lease. Checksum/unknown schema/regression сохраняют
предыдущий валидный снимок только до его исходной lease.

Server 401 `task_control_session_invalid` — неизвестная/истёкшая сессия;
403 `task_control_access_denied` — подтверждённый отказ/revoke;
503 `task_control_session_unavailable` — ошибка чтения сессии/привязки в БД;
503 `task_control_core_unavailable` — нет свежего подтверждения установки;
503 `task_control_session_limit` — достигнут лимит активных сессий;
503 `task_control_runtime_payload_too_large` — bootstrap превышает 1 MiB.
Форма ошибки прежняя: `{error:{message,status,code}}`. Outage не объявляется revoke.

## Ограничения и очистка

64 одновременно действующих сессии на company/binding/actor. Admission выполняется
под account advisory lock; обновление одной вкладки не удаляет bearer других.
Просроченные строки не учитываются при admission. Не заменять глобальной ротацией
одного токена на пользователя.

Company worker при старте и раз в 60 секунд удаляет до 1000 истёкших сессий и
1000 assertion replay records за проход, timeout 5 секунд, индексы expires_at,
`FOR UPDATE SKIP LOCKED`. Expiry проверяется при каждом чтении независимо от GC.
Replay-proof с exp ≤ now уже не может пройти verifier после удаления записи.
Core used_widget_tokens сохраняет TokenRetainUntil с leeway и очищается штатным
maintenance worker; новый механизм очистки Core не требуется.

Company migration 000034 создаёт три индекса. Сначала применить миграцию, затем
обновить company/gateway, затем собрать/установить ZIP 1.1.4 обычным процессом.
Размер runtime envelope ≤1 MiB UTF-8, максимум 1000 правил. IDs/revisions на клиенте
должны быть safe positive integers. Кеш: одна сессия и один снимок на вкладку.

## Подготовка R11

V2 выбирается только сессией `runtimeSchemaVersion:2`, bootstrap по отдельному
`/api/v2/task-control/runtime/bootstrap`. Runtime 1.1.4 умеет проверять envelope
R00: actor/scope, publicationVersion, runtimeRevision, sourceRevisions,
resolverVersion, snapshot/publication identity, intent/resolved/payload checksums,
lease. Применение по runtimeRevision допускает новую проекцию при той же
publicationVersion; равная revision с другим payload checksum отклоняется.

R01 не выпускает resolver и не выдаёт v2-сессии. R11 должен добавить серверную
проверку версии ≥1.1.4/capability `dynamicRulesV2`, новый epoch и endpoint с реальными
снимками. Произвольный флаг браузера не включает динамику. V2 lease дополнительно
ограничивается verified source freshness по R00. Старый v1 API не получает v2.

## Проверки

Матрица в `tests/teamos_reliability.test.js` и `tests/teamos_controller.test.js`:
renewal, fresh JWT, 401 renewal/repeated denial, 403/404 revoke, 429/503/timeout,
reconnect, independent tabs, singleflight, cold offline, lease/clock rollback,
late response after destroy, unknown protocol, payload size, actor/epoch isolation,
v2 revision ordering и совместимость версий. Прежние тесты standalone сохранены.

PostgreSQL `TestTaskControlLifecycle` с race detector: replay/concurrent exchange,
64-сессионный лимит, сохранность соседних вкладок, bounded cleanup, невозможность
повторного истёкшего proof, restart, revoke/uninstall/reinstall, bootstrap metadata.
Это локальная интеграционная проверка с синтетическими ключами; live amoCRM и
публикация ZIP относятся к последующей приёмке/выпуску.

Подтверждено локально: 78/78 Node tests и syntax check; company/gateway unit tests
с `-race`; PostgreSQL lifecycle с `-race`, включая конкурентный admission 63→64,
изоляцию actors, batch 1000 из 1005 истёкших proof и отказ при ошибке чтения БД;
Core taskcontrol/widgetauth/maintenance с реальными PostgreSQL migrations и `-race`;
`make gen`, `make check-contract` (238 вызовов), cross-repository v1 checksum test.
Сборка ZIP 1.1.4 проверена с `.invalid` origins только как smoke-test; это не
готовый production-архив. Все тестовые контейнеры удалены.
