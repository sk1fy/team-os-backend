# Проверить связь «Контроля задач» с TeamOS

Автоматические проверки используют синтетические JWT, временные PostgreSQL и локальный HTTP.
Они не устанавливают виджет, не изменяют OAuth или grants и не подтверждают живой аккаунт.

## Проверить backend без соседних репозиториев

Выполните команду из корня backend на машине с Go 1.26.10 и Docker.

```sh
python3 scripts/task-control-r09-acceptance.py connection-backend \
  --output /tmp/task-control-connection
```

Фаза проверяет четыре обязательных PostgreSQL-теста с race detector.
Пропуск, исчезновение или падение обязательного теста завершает фазу с ошибкой.
CI выполняет эту команду в задании Task Control connection acceptance.
Она не требует checkout Core или виджета.

`TestTaskControlHTTPStatusGuardsLinkTransaction` использует настоящий TLS/HMAC-клиент Core.
Ответ сверх 64 КиБ, второй JSON или отказ источника не создают binding и не расходуют assertion.
Корректный ответ после отказа позволяет связать точную установку.
Истёкшее подтверждение, чужая компания, неверное поколение и отсутствие CRM admin отклоняются.
Повтор одного intent возвращает ту же связь. Соседняя связь сохраняется.

Существующие тесты проверяют lifecycle, отзыв, переустановку, права и применение правил v1/v2.
Ошибки проверки справочников не превращаются в успешное сохранение правил.
Один код подключения не заменяет company registration, service grant или capability.

## Проверить границы с Core и виджетом

Дополнительно запустите unit-тесты `internal/taskcontrolbridge` и `internal/domain/taskcontrol`
в company, а также Task Control transport-тесты gateway с `GOWORK=off` и `-race`.

В Core используйте штатный Docker-стек `docker-compose.test.yml` с отдельным проектом,
применёнными миграциями и `TEST_DATABASE_RESET_ALLOWED=true`.
Выполните тесты `internal/taskcontrol`, `internal/oauth` и `internal/integrations` с `-race`.
Без тестовой PostgreSQL эти Core-тесты могут быть пропущены, поэтому проверяйте JSON-результаты.

Для виджета выполните `npm test` и `npm run check` в `rkrs_lead_notasks`.
Три backend-скрипта `check-task-control-*-contract.cjs` принимают `WIDGET_DIR` с путём к этому каталогу.

Настоящий код виджета проверяется вместе с company и PostgreSQL следующей командой.

```sh
cd services/company
GOWORK=off WIDGET_DIR=/absolute/path/rkrs_lead_notasks \
  go test -race -tags=integration ./internal/application \
  -run '^TestTaskControlR09ActualWidgetRoundTrip$' -count=1 -timeout=5m
```

Этот тест требует Node.js и Docker. Он проверяет session/bootstrap, правила движка
и отдельные received/applied-сообщения. Внешнее подтверждение amoCRM/Core синтетическое.
Отдельные проверки Core и TeamOS не являются единым живым OAuth-прогоном.

## Подготовить проверяемый ZIP

Соберите пакет штатным `rkrs_lead_notasks/scripts/build.sh` с явными
`TASK_CONTROL_CORE_ORIGIN`, `TASK_CONTROL_BACKEND_ORIGIN` и `TASK_CONTROL_OUTPUT`.
Оба origin должны быть публичными HTTPS origin нужного стенда без путей.
Для общего входа также обязательны `COMPANY_ENTRY_CORE_ORIGIN` и
`COMPANY_ENTRY_FRONTEND_ORIGIN`. Первый совпадает с Core origin; второй указывает
на публичный HTTPS frontend TeamOS.
Release CLI принимает второй адрес через `--company-entry-frontend-origin`, сохраняет
его в `candidate.json` и передаёт сборщику. Deployment audit сверяет его с ZIP.
Фаза `artifacts` использует только `.invalid`-адреса и не даёт готовый к установке managed ZIP.
Сверьте manifest, встроенный deployment.js, версию runtime и SHA-256 ZIP/JS.
Повторная сборка тех же исходников с тем же SOURCE_DATE_EPOCH должна совпадать побитово.
Файл на диске не доказывает, что такой ZIP установлен в amoCRM.

Runtime 1.2.5 требует как минимум Core migration 36 и company migration 42.
При обновлении применяются все миграции согласованной версии, без отката схемы к этим минимумам.
Descriptor подтверждается оператором для точного installation/generation.
Browser capability или heartbeat не заменяют это подтверждение.

## Передать живую приёмку пользователю

До ручного подключения сверяются следующие условия.

- ZIP загружен в нужную интеграцию с сохранённым OAuth UUID. Callback и установка относятся к нужной среде.
- Capability task-control включена только у нужной интеграции. Grant ограничен компанией, аккаунтом и интеграцией.
- Service credentials согласованы между сервисами. Assertion public keys соответствуют ключу Core, без копирования private key в TeamOS.
- Код связывает точные installation/integration/account/generation. Повтор другого запроса и чужие данные отвергаются.
- Runtime descriptor соответствует установленному ZIP. Сохранение правил доходит до runtime, received и applied различаются.

На момент read-only проверки этой задачи Devcorps использует backend `0ebe7db`,
а ZIP на диске имеет версию 1.2.4 при исходнике 1.2.5.
Фактические callback, grants, ключи TeamOS, схемы БД и версия установленного в аккаунте ZIP не подтверждены.
Публичный HTTPS origin frontend TeamOS также не подтверждён. До его проверки
тестовый пакет остаётся синтетическим и не используется для живой установки.
Доступный MCP не раскрывает container environment; Docker-тесты под codex там недоступны.
Это ограничения живой приёмки, а не результат автоматического тестирования.
Развёртывание и ручные проверки выполняет пользователь отдельно.
