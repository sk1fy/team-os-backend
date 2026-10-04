# Независимое ревью РС-03–РС-10: TeamOS backend

Дата: 2026-10-04. Основа: `499da4f8593077d2bd0ac64ee237027092f029f7`
(`codex/lead-distribution-stage-10`). После fetch `origin/main`:
`55f425edfe2018a876ad6f2a2429d20cd46bbf5c`; main является предком, ветка
опережала его на 7 коммитов и не требовала разрешения конфликтов.

Изучены описания и комментарии этапов и подзадач ClickUp List
«Rakurs Infrastructure 2.0»: РС-03–РС-10. Проверка этого репозитория охватывает
company/gateway, межсервисный контракт Core, PostgreSQL queue/inbox/mirror,
авторизацию, графики, общие widget API и observe/live. Диагностика РС-09
принадлежит Core/Admin и отдельно проверяется в соответствующих проектах.

## Исправленное замечание

**P1 — истёкший запрос с неизвестным результатом приёма навсегда удерживал группу.**
После сохранения frozen assignment и потери POST/ответа Core мог возвращать 404.
После `validUntil` admission gate переводил строку в
`requires_configuration/expired_never_admitted`, сохраняя lead/group claims.
Повторная сверка, отмена и перезапуск не давали доказательства для освобождения;
остальные сделки группы не могли получить следующий ход. Такое поведение было
явно сохранено тестом и описано в комментариях РС-10.

Worker теперь отправляет исходные assignment body и Idempotency-Key в
`POST /internal/v1/distribution/assignments/expire` после expiry и GET 404.
Core сериализует этот запрос с обычным admission. Если операция уже есть,
возвращается её актуальное состояние. Иначе Core сохраняет terminal
`rejected/no_attempt` результат, запрещающий позднему исходному POST создать
исполняемую операцию. Существующая cancelled job служит связанной записью аудита.

TeamOS проверяет полную immutable identity и terminal evidence существующим
путём mirror/result. Только после этого одной транзакцией освобождает оба
claims. Cursor не продвигается. Если эпизод ещё актуален и отмена не запрошена,
используется существующий пересчёт `waiting/decision_recalculation`.

Отсутствующий endpoint в старом Core, ошибка сети, 4xx или некорректный ответ
сохраняют исходный intent и claims. Потеря ответа уже сохранённого expiry
восстанавливается GET после перезапуска. Реальный `outcome_unknown` после
возможного PATCH не превращается в доказанное отсутствие эффекта.

**P1 — межсервисный код ошибки не запускал безопасный пересчёт.** Независимая
проверка Core выявила, что `wireErrorCode` заменял `decision_expired` и
`recipient_unavailable` на `operation_unresolved`. TeamOS ожидает исходные
коды для пересчёта после доказанного отсутствия эффекта. Из-за этого актуальный
эпизод становился `failed`, хотя можно было продолжить распределение.
Исправление в Core сохраняет эти коды в ответе и обоих OpenAPI enum. Paired
регрессия проверяет именно `waiting/decision_recalculation`, очищенную command,
оба освобождённых claims и неизменный cursor без предварительного cancel.

## Остальные проверенные границы

- РС-03: текущая компания/роль/section перечитываются из БД; widget principal
  имеет deadline, текущий mapping и серверный scope; приватные callbacks
  проверяют HMAC, nonce и grants для company/installation/capability.
- РС-04–05: результат применяется только к зарегистрированному operation
  mirror; identity и версии защищены; inbox commit предшествует ACK,
  historical binding и повторная доставка не создают нового решения.
- РС-06: frozen command и keys сохраняются до HTTP; группа и account/lead
  защищены claims; leases отсекают старого writer. RR меняется только при
  подтверждённом результате, доступность проверяется через registry/графики.
- РС-07–08: запись общих правил требует текущего администратора, CAS revision;
  UI получает разрешённые действия. Неизвестный outcome виджета не даёт
  автоматически выполнить mutation повторно. История и observation скрываются
  при отсутствии права на сделку.
- РС-10: observe отделён от business queue/claims, mode change сериализован
  с admission, новая live boundary исключает старые наблюдения. Pause сохраняет
  очередь; неизвестные внешние эффекты требуют подтверждения Core.

## Проверки

- `make test` — все Go-модули, PASS.
- `make lint` — все Go-модули, PASS, 0 issues.
- `make check-contract` — OpenAPI, protobuf и frontend sync, PASS.
- `GOWORK=off go test -tags integration -race ./internal/application
  ./internal/transport/distributionhttp -run 'Distribution|RS06' -count=1`
  в company — PASS, отдельные PostgreSQL testcontainers, migrations 1–28.
- Регрессия application: потерянный admission, старый Core без expiry endpoint,
  pause, потерянный expiry ACK, restart/GET, no-attempt release обоих claims
  без расхода RR и повторного Assign.
- Финальный focused PG/race прогон этой регрессии после добавления потерянного
  expiry ACK и restart — PASS (9.5 с).
- Добавлен paired сценарий
  `expired_missing_admission_is_fenced_before_claim_release`: настоящие
  Core↔TeamOS handlers/HMAC и две PostgreSQL, terminal result, late POST,
  exact expiry replay, сохранённый cursor и следующий ход группы. Полный запуск
  выполняется совместно из Core через `make distribution-team-bridge-test`.

## Совместимость и ограничения

Публичный REST/protobuf, migrations и UI DTO не меняются. Новый внутренний Core
endpoint аддитивен. Рекомендуемый порядок — Core, затем company. При обратном
порядке TeamOS сохраняет claims до обновления Core; небезопасной очистки нет.

Не требуется ручное изменение PostgreSQL, удаление claims или замена IDs.
Существующие unresolved CRM эффекты остаются защищены. OAuth/re-OAuth,
установленный ZIP/SDK, тестовый сервер и production/legacy cutover здесь не
подтверждены: ограничения РС-03.1.2, РС-08.3.2, РС-10.2.1/10.2.2/10.3.1
из комментариев ClickUp сохраняются.
