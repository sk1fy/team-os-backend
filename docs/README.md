# Документация TeamOS backend

## Контроль задач

- [ADR КТ-01.01](../../amocrm-pro/docs/adr/0038-task-control-hybrid-integration.md) —
  каноническое решение в соседнем репозитории Core: company владеет правилами,
  ревизиями и аудитом; Core проверяет установку и принимает телеметрию; tasks TeamOS остаётся отдельно.
  Последующие контракты и реализация перечислены ниже.
  [Копия решения в ClickUp](https://app.clickup.com/t/869fhtvty).

- [Модель данных и API КТ-01.02](integrations/task-control-contract.md).
- [Подключение и auth handoff КТ-01.03](integrations/task-control-connection.md).
- [Архитектура общих справочников и правил R00](integrations/task-control-r00-architecture.md).
- [Runtime R01](integrations/task-control-r01-runtime.md), [права R02](integrations/task-control-r02-access.md),
  [справочники R03](integrations/task-control-r03-references.md).
- [Импорт R04](integrations/task-control-r04-import.md), [история R05](integrations/task-control-r05-history.md),
  [диагностика R07](integrations/task-control-r07-diagnostics.md).
- [Семантика R08](integrations/task-control-r08-semantics.md),
  [приёмка R09](integrations/task-control-r09-acceptance.md),
  [выпуск R10](integrations/task-control-r10-release.md),
  [динамические правила R11](integrations/task-control-r11-dynamic.md).
- [Продолжение серверной приёмки R09/R10](runbooks/task-control-r09-r10-server-prompt.md) —
  передача с незакрытыми проверками; сохранена до получения живых evidence.

## Архитектура и контракты

- [План архитектуры](architecture/teamos-go-microservices-plan.md).
- [ADR](adr/) — границы сервисов, контракты и решения Academy/распределения.
- [REST OpenAPI](../contracts/openapi/teamos.yaml).
- [Правила работы](../AGENTS.md).

## Academy

- [Миграция и cutover](academy-migration-cutover.md).
- [Эксплуатация](academy-operations-runbook.md).
- [Диаграммы состояний](academy-state-diagrams.md).

## Распределение

- [Подключение Core](integrations/lead-distribution.md).
- [Интерфейс TeamOS](integrations/lead-distribution-interface.md).
- [Private API виджета](integrations/lead-distribution-widget.md).
- [Режим наблюдения](integrations/lead-distribution-observation.md).
- [Ожидание и очередь](integrations/lead-distribution-queue-ux.md).

## Развёртывание и проверки

- [Production-развёртывание](runbooks/deploy.md).
- [Безопасность production](runbooks/production-security.md).
- [Проверки после развёртывания](runbooks/test-services.md).
- [Staging распределения](../deploy/distribution-staging/README.md).
- [E2E](../tests/e2e/README.md) и [нагрузочные проверки](../tests/k6/README.md).
- [Наблюдаемость](../deploy/observability/README.md).

Результаты локальных проверок не подтверждают production или установленный виджет.
