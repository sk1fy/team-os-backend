# R02: серверные права и API подключения

2026-10-16. [Задача](https://app.clickup.com/t/869fhxj40),
[архитектура R00](task-control-r00-architecture.md).

## Реализовано

`GET /api/v1/task-control/capabilities` возвращает актуальные права активного
пользователя компании. Даже пользователь без доступа к Контролю задач может узнать
свои `read:false/manage:false`; отключённый пользователь/компания получают отказ.
Роль и override читаются из БД, Distribution и JWT role не дают новых прав.

```json
{
  "read": true,
  "manage": false,
  "operations": {"draft": true, "publications": true, "history": true,
    "references": true, "application": true, "publish": false, "mode": false,
    "rollback": false, "imports": false},
  "availableOperations": ["connections", "installation", "draft", "publications",
    "link", "revoke", "mode", "updateDraft", "validate", "publish", "intent"],
  "adminProofTtlSeconds": 60,
  "runtimeSessionTtlSeconds": 300
}
```

Пример сокращает operations; фактический ответ включает все известные операции.
Operations — права, availableOperations — наличие реализации, а не доступность
конкретного аккаунта. Перед действием сервер заново проверяет права, scope,
установку и proof: capabilities не является токеном или кешем авторизации.

| Субъект | read | manage |
|---|---|---|
| Owner/admin без override | да | да |
| Owner/admin с canRead=true, canManage=false | да | нет |
| Employee без override | нет | нет |
| Employee с canRead=true | да | нет, даже если canManage=true |
| Любая роль с canRead=false | нет | нет |

Read: список/установка, draft, publications, references, history, application.
Manage: updateDraft, validate, publish, rollback, imports, sourceRequests,
refreshReferences, link, revoke, mode; intent доступен только управляющему автору
операции. Для link/revoke/mode дополнительно нужен Core CRM-admin proof.
Права на будущие обработчики проверяются в company через ACTION_AUTHORIZE до 501.
Импорт/история/справочники/ACK остаются соответственно R04/R05/R03/R07 — 501 после
авторизации не считается готовой бизнес-функцией. Core references service capability
из R00 вводится вместе с provider R03; этот API не выдаёт service grants.

Чтение сохранённых draft/publications не требует свежего Core status и не создаёт
draft в БД. История остаётся доступной после revoke/reinstall при текущем доступе
пользователя и сохранённой активной связи CRM-аккаунта с компанией. Мутации требуют
активного binding и свежего installed status. Чужой binding/publication — 404.

## Proof, повторы и состояние операции

Core JWT механизм сохранён. Proof подписан, содержит company/account/installation/
integration/generation/CRM actor/purpose/requestId, TTL ≤60 s. Browser companyId,
isAdmin и лишние поля управляющих DTO отклоняются strict decode; TeamOS actor приходит
из аутентификации. TeamOS admin и CRM admin — две отдельно проверяемые роли;
они не обязаны иметь одинаковый идентификатор или mapping сотрудника.

JTI потребляется транзакционно; после ожидания account lock проверяется срок proof.
Два одновременных exchange одного proof дают ровно одну сессию. Изменение режима
в БД атомарно меняет mode/epoch и удаляет прежние runtime sessions; bootstrap выдаёт
managed publication только в managed. Одновременно двух серверных режимов нет.

RequestId/intentId повтор допустим только для исходного TeamOS actor и того же
semantic input, после текущей авторизации. Publish receipt также привязан к createdBy.
Возврат receipt не потребляет proof второй раз. Тот же ключ с другим input/actor —
409. Повтор mode после lost response не увеличивает epoch. Новый запрос в уже
выбранный режим — 409 mode_unchanged. GET intent проверяет автора и текущую связь
аккаунта; completed — результат исторической операции, текущий режим читается из
GET connections. Timeout не означает отмену: повторить исходный ключ/прочитать intent,
не создавать новый ключ наугад.

Полный descriptor ACK/export/reconciliation протокол из R00 пока не реализован:
MVP по-прежнему требует явного сохранения настройки в amoCRM после server mode switch.
Этот релиз прав не доказывает отсутствие старых автономных вкладок и не заменяет
R01/R04/R08/R09. Перед выпуском нужно проверить переход обоих writers на живом runtime.

## Стабильные ошибки

| HTTP | code | Значение |
|---|---|---|
| 400 | task_control_invalid_request | Некорректные доменные параметры |
| 401 | task_control_assertion_invalid / task_control_assertion_replayed | Недействительный/истёкший proof либо повтор JTI |
| 401 | task_control_session_invalid | Runtime session истекла/отозвана |
| 403 | task_control_access_denied | Нет прав, неверный proof scope/purpose/admin |
| 403 | task_control_binding_revoked | Gate будущей мутации отозванного binding |
| 404 | task_control_not_found | Нет сущности в текущем scope |
| 409 | task_control_revision_conflict / task_control_epoch_conflict | Устарели данные или режим |
| 409 | task_control_idempotency_conflict | Ключ занят другим input/actor |
| 409 | task_control_mode_unchanged | Новый запрос на уже текущий режим |
| 503 | task_control_core_unavailable | Не подтверждена установка |
| 501 | task_control_not_implemented | Авторизация прошла, бизнес-операция ещё не реализована |

Ошибки strict HTTP/gRPC decode и общей аутентификации сохраняют общий ApiError,
code может отсутствовать; UI всегда учитывает HTTP status, а не текст сообщения.

## Проверки

Табличная матрица domain access; PostgreSQL lifecycle с read override, spoofed role,
отзывом прав, чужим binding/автором, неверным/истёкшим proof, concurrent consume,
идемпотентностью mode/publish и сохранностью истории; gateway guards всех девяти
будущих операций. Полные unit-наборы company/gateway, race lifecycle, make gen,
make check-contract, legacy DTO/checksum check. Это локальные проверки, не production
деплой или live amoCRM-приёмка. UI и generated TS в frontend не изменяются.
