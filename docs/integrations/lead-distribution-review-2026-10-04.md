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
  exact expiry replay, сохранённый cursor и следующий ход группы. Финальный запуск
  `make distribution-team-bridge-test` — PASS: 13 сценариев, настоящий
  Core↔TeamOS HTTP/HMAC, две PostgreSQL и согласованный backup/restore.

## Совместимость и ограничения

Публичный REST/protobuf, migrations и UI DTO не меняются. Новый внутренний Core
endpoint аддитивен. Рекомендуемый порядок — Core, затем company. При обратном
порядке TeamOS сохраняет claims до обновления Core; небезопасной очистки нет.

Не требуется ручное изменение PostgreSQL, удаление claims или замена IDs.
Существующие unresolved CRM эффекты остаются защищены. OAuth/re-OAuth,
установленный ZIP/SDK, тестовый сервер и production/legacy cutover здесь не
подтверждены: ограничения РС-03.1.2, РС-08.3.2, РС-10.2.1/10.2.2/10.3.1
из комментариев ClickUp сохраняются.

## Замечания GitHub CI после публикации PR

Run `37204039149` на `fe18663` выявил три reachable advisory в девяти
Go-модулях: [GO-2026-6505](https://pkg.go.dev/vuln/GO-2026-6505),
[GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443),
[GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348). Эти версии уже присутствуют
в `origin/main` (`55f425e`); этапы распределения не меняли go.mod/go.sum.

Согласованное исправление: gRPC `1.82.1 → 1.83.2`, OpenTelemetry
`1.44.0 → 1.45.0` во всех девяти затронутых модулях, с обязательными
транзитивными обновлениями. Обе версии требуют Go 1.25.0; существующие CI и
Docker Go 1.25.13 сохранены, major версии и контракты не меняются.

Проверки после обновления на **Go 1.25.13**: `make test`, `make lint` (0 issues),
`make check-contract` и `govulncheck@v1.6.0 ./...` отдельно в каждом из девяти
модулей — PASS, 0 reachable vulnerabilities. Сканирование не отключалось.

В том же run E2E smoke остановился до запуска сервисов: Docker Hub отклонил
pull `minio/minio:RELEASE.2025-04-22T22-12-26Z`. Тот же image pin существует
на main; PostgreSQL миграции и сам smoke в этом job не запускались. Official
Quay mirror не предоставил ни этот tag, ни прежний digest. Это отдельная
проблема CI-инфраструктуры, не результат выполнения очереди распределения.

Восстановление E2E изолировано в `deploy/docker-compose.ci.yaml` и
`deploy/ci/minio.Dockerfile`: сборка того же официального release из commit
`0d7408fc9969caf07de6a8c3a84f9fbb10a6739e`, с проверкой SHA-256 архива до
компиляции, неизменным upstream go.sum и pinned multiarch builder/runtime.
Go 1.25.14 используется только для этого CI fixture; основной backend остаётся
на Go 1.25.13. Dev/production image pin, версия MinIO и S3 API не изменены.
См. [инструкцию smoke](../../tests/e2e/README.md).

Проверки fixture: final Docker build PASS; `minio --version` подтверждает
исходные release/commit и Go 1.25.14; live/ready health endpoints — HTTP 200;
CI Compose config и `make check-production-compose` — PASS. Дополнительная
cross-build проверка того же source на Go 1.25.13 также прошла. Временный
health-контейнер остановлен и удалён. Полный smoke после этого выполняется в
GitHub CI, не подменяется проверкой health.
