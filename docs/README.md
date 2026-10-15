# Документация TeamOS backend

## Контроль задач

- [ADR КТ-01.01](../../amocrm-pro/docs/adr/0038-task-control-hybrid-integration.md) —
  каноническое решение в соседнем репозитории Core: company владеет правилами,
  ревизиями и аудитом; Core проверяет установку и принимает телеметрию; tasks TeamOS остаётся отдельно.
  Согласовано 2026-10-15; контракты описаны, хранение ещё не реализовано.
  [Копия решения в ClickUp](https://app.clickup.com/t/869fhtvty).

- [Модель данных и API КТ-01.02](integrations/task-control-contract.md).
- [Подключение и auth handoff КТ-01.03](integrations/task-control-connection.md).

## Архитектура и контракты

- [План архитектуры](../teamos-go-microservices-plan.md).
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
- [Ревью 04.10.2026](integrations/lead-distribution-review-2026-10-04.md).

Инструкции и свежий отчёт сохранены: содержат действующие контракты,
границы восстановления и условия внешней приёмки. Результаты локальных
проверок не подтверждают production или установленный виджет.
Развёртывание и безопасность — в [корневом README](../README.md).
