# РС-10: наблюдение распределения

Canonical REST: `contracts/openapi/teamos.yaml`. Новый GET
`/api/v1/distribution/rules/{ruleId}/observations?limit&offset` возвращает immutable
plans, checkedAt, physical hasMore и ограниченную страницу. Scope берётся из
авторизации, не из browser company/account fields. Private widget runtime
поддерживает read kind=observations с id=ruleUUID и fresh actor/resource checks.

Create/update rule принимает optional executionMode=live|observe. Create по
умолчанию active=false; mode не заменяет ordinary pause. Read rule содержит
executionMode, executionEpoch, nullable liveStartedAt. Mode write использует
expectedRevision и блокирует unsettled живую queue. Сначала оператор отдельно
отменяет допустимую waiting работу; unknown guards остаются сохранёнными.

Наблюдение — новая company migration28, durable jobs и immutable log; никаких
live queue/claims/command/turn. Одинаковая chooseDistributionPlan используется
в live и observe. Исторический currentResponsibleUserId относится к
crmObservedAt; checkedAt — время вычисления плана. Widget lead дополнительно
возвращает fresh currentLead, даже при items=[]; оба факта отображаются раздельно.

sourceOccurredAt/sourceReceivedAt, entry/event/epoch, rule/availability/observation
revisions сохраняют происхождение плана. Lead permission проверяется per row:
неразрешённая строка полностью скрыта, недоступность permission source — ошибка.
Нет operation/actions из observation. Pagination metadata не является приватным
количеством доступных/скрытых сделок.

Проверки: `TestDistributionObservationIsolationDedupPrivacyAndEnableBoundary`,
`TestDistributionModeChangeSerializesAdmissionAndPendingObservationPoint`,
`TestChooseDistributionPlanSharedKeepOrderAndWaiting`. Paired profile в Core
проверяет настоящий HTTPS/HMAC Core↔Team, same native/widget observation,
zero effects, fresh live boundary, two-owner dump/restore/reconnect и unknown.

Семантика и ограничения: [ADR-010](../adr/ADR-010-distribution-observation-mode.md).
Полная матрица/операторский runbook — в соседнем Core репозитории. Реальный сервер,
OAuth/установленный ZIP/legacy cutover этим этапом локально не проверяются.
