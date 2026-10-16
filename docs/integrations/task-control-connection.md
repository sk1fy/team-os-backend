# КТ-01.03: подключение и короткая сессия «Контроля задач»

Основание MVP — ADR-0038, редакция 3. Текущая [редакция 4](../../../amocrm-pro/docs/adr/0038-task-control-hybrid-integration.md)
и [контракт R00](task-control-r00-architecture.md) расширяют будущую реализацию общими
справочниками и runtime v2; описанное ниже подключение MVP само по себе их не реализует.
Изменения ограничены Контролем задач. Распределение/Активность используют свои
прежние bindings, service grants и правила. Исполнение автономного виджета не меняется.

## Реализованная цепочка

1. Виджет получает свежий JWT amoCRM и вызывает Core
   `POST /api/v1/widget/task-control/auth` через существующий transport свежих
   disposable tokens. Обычный `$authorizedAjax` может кешировать JWT: нельзя повторять
   тот же jti при обновлении сессии. Пример уже есть в backend-client.js общего пакета.
   Body: `purpose`, `requestId` UUID.
2. Core использует существующий widgetauth, Origin/issuer binding и rate limiter.
   Он проверяет active installation конкретной integration с service `task-control`,
   отдельный включённый grant и live CRM user через существующий GET users transport.
   Runtime разрешён активному менеджеру; link/revoke/mode/adminBridge требуют CRM admin.
3. Company берётся из server grant; несколько разных компаний для установки —
   ошибка, не выбор первой. Browser companyId, роль или email не являются доказательством.
4. Core атомарно потребляет amoCRM jti и выдаёт Ed25519 assertion на 60 секунд:
   aud `teamos-task-control`, jti, iat/exp, company/account/installation/integration,
   product, actor, generation, admin, purpose и requestId. Формат и fixture находятся
   в контрактах. Signing private key и client secret amoCRM остаются в Core.
5. Backend TeamOS принимает `coreAssertion`: при runtime —
   `POST /api/v1/widget/task-control/sessions`, при link/revoke — соответствующий
   authenticated TeamOS API. Core assertion не является JWT amoCRM.
6. Backend проверяет public-key подпись, срок, назначение и scope. Company/account
   должны уже иметь активную verified связь в company_integrations. Ничего не
   провиженится: новые компании, пользователи и TeamOS admin-роли не создаются.
7. Потребление assertion jti и сохранение opaque runtime session происходят одной
   транзакцией. Сессия живёт 5 минут, в PostgreSQL — только SHA-256 токена.
   `GET /api/v1/widget/task-control/bootstrap` принимает Bearer session; эти два
   точных маршрута используют собственную авторизацию вместо TeamOS JWT.
8. Будущие config/ACK handlers обязаны вызывать `AuthorizeTaskControlRuntime`.
   Он проверяет company/account, session expiry и текущие binding/epoch/generation,
   а также свежий installation snapshot. Конфигурацию Core не получает.

Потерянный ответ обмена session требует нового Core assertion, а не повтора jti.
Link/revoke идемпотентны по intent/requestId и original input/owner; повтор не
создаёт второй binding и не потребляет старый JWT ещё раз. Другой body — конфликт.
Режим после link остаётся standalone. Переход в managed относится к .07/.12;
публикация и получение правил — .04–07, настройки TeamOS — .09–11.

## Статус установки и права

Core status endpoint подписан HMAC с отдельным account/company/product grant.
Нужна запись конкретной установки, а не просто heartbeat или наличие OAuth другой
интеграции. Active installation + active integration + enabled task-control capability
даёт installed; достоверный uninstalled — notInstalled; отсутствие записи, disabled,
неоднозначность/недоступность — unknown. Unknown не утверждает отсутствие виджета.

Core migration 24 отслеживает status revision и поколение повторной установки.
Здесь нет правил/настроек продукта. Backend migration 32 хранит connection, snapshots,
permission overrides, consumed assertion IDs, session hashes и idempotency receipts.
Внешние ID не имеют FK в Core. Один active binding на installation и company/account.
Явный link новой установки/поколения сохраняет новый binding и отзывает прежний.

Cache Core ≤5 минут. Свежие чтения используют его без сетевого запроса Core каждый раз.
Истёкший cache + Core outage даёт unknown в Интеграциях и 503 для доступа runtime;
достоверный uninstall — 403. Старые ревизии не заменяют новые даже при конкуренции.
Локальный кеш виджета не сбрасывается сетевым сбоем; immediate offline revoke не обещается.
Heartbeat отсутствует — это не uninstall. Платёжного gate нет.

Owner/admin TeamOS имеют taskControl.read/manage по умолчанию. Employee не получает
эти права из distribution или CRM-роли. Таблица task_control_permissions может
ограничить администратора или разрешить сотруднику read; manage employee всё равно
не разрешается. Политика перечитывает текущую роль/активность, не доверяет старым claims.
UI изменения прав относится к .09. Административные source/mode handlers в следующих
этапах должны проверять свежий Core admin assertion, не флаг из runtime session.

## Настройка перед запуском пилота

Ничего из перечисленного не включается и не провиженится автоматически.

Core:

- Применить migration 24 после штатных миграций; зарегистрировать реальную integration
  Контроля задач и её installation штатным процессом. Для старого автономного виджета
  без инфраструктурной записи Core честно вернёт unknown; регистрация нового пакета — .12/.15.
- Явно включить integration_services service_code `task-control` для нужной integration.
  Не включать его всем интеграциям миграцией.
- `TASK_CONTROL_HTTP_ADDRESS`, `TASK_CONTROL_TLS_PROXY=true` и
  `TASK_CONTROL_SERVICE_KEYS` (JSON keyId→HMAC secret, минимум 32 символа).
- `TASK_CONTROL_ASSERTION_KEY_ID`, `TASK_CONTROL_ASSERTION_PRIVATE_KEY`: base64
  Ed25519 private key 64 байта. Не использовать тестовый fixture-ключ.
- `go run ./cmd/task-control-grant --key-id … --company-id … --installation-id … --actor … --enabled true`
  создаёт только scoped grant и audit; секреты передаются штатной конфигурацией, не аргументами.
- Опциональный `docker-compose.task-control.yml` открывает порт только внутри сети.
  Reverse proxy публикует только widget auth route; `/internal/v1/task-control/*` остаётся
  приватным за TLS и signed authentication. Существующий listener распределения не меняется.

Backend TeamOS:

- Применить company migration 32; сохранить существующую company_integrations связь.
- `COMPANY_TASK_CONTROL_CORE_URL`: HTTPS origin Core, без path/query/userinfo.
- `COMPANY_TASK_CONTROL_KEY_ID`, `COMPANY_TASK_CONTROL_SECRET`: credential только для status.
- `COMPANY_TASK_CONTROL_ASSERTION_PUBLIC_KEYS`: JSON keyId→base64 public key (32 байта).
  Приватный ключ и amoCRM client secret сюда не передаются. Ротация допускает несколько public keys.
- Существующий gateway/company service credential требуется для exchange/bootstrap RPC.
  Только gateway имеет этот credential, браузер — нет. Redirect Core не разрешён; budget 3 секунды.
- CORS gateway должен разрешать нужные amoCRM origins; текущие стандартные настройки уже
  включают `https://*.amocrm.ru`/`.com`. Это не заменяет проверку session/scope.

## Проверки и пределы результата

Локально проверяются реальные PostgreSQL migrations и транзакции, signed HTTP,
настоящий Core widgetauth с синтетическими amoCRM JWT, replay/tenant/product/role
отказы, concurrent exchange, restart, stale/unavailable/uninstalled/reinstalled,
revocation и сохранность количества компаний/пользователей. Общий Ed25519 fixture
проверяется отдельно Core signer и TeamOS verifier. Race detector используется для
профильных проверок. Live amoCRM, deployment secrets, установленный новый пакет,
редактор TeamOS и мониторинг админки не выдаются за выполненную приёмку КТ-01.03.

Подтверждённый локальный прогон 2026-10-15: `make test` во всех backend-модулях,
`make gen`, `make check-contract` (238 frontend-вызовов), company/gateway tests,
профильная company PostgreSQL suite с `-race` (4.856 s), Core Task Control
PostgreSQL/real-widgetauth suite с `-race` (2.223 s), прежний widgetauth package,
Core `go vet`, frontend build/contract:check и 2 клиента, standalone 58/58 и syntax check.
Тестовые PostgreSQL изолированы; миграции production не применялись.

## Сквозной MVP: правила и runtime

Миграция company `000033_task_control_rules` добавляет черновик, неизменяемые публикации и указатель текущей публикации. Правила читаются и изменяются только текущим owner/admin с разрешением `taskControl.manage`, в пределах компании и подтверждённой установки. PUT черновика использует CAS (`expectedRevision`, binding revision, mode epoch). Публикация использует `requestId`, ожидаемые ревизию/ checksum и причину; повтор того же запроса возвращает ту же публикацию, другое тело — 409. Удаление правила выполняется полным PUT списка без него; архивирование сохраняет правило и флаг.

Последовательность MVP: администратор получает в настройках amoCRM виджета одноразовый Core-код `link` → вставляет в TeamOS → сохраняет/публикует правила → получает отдельный код `mode` → явно включает managed в TeamOS → включает режим TeamOS в настройках виджета и нажимает основную кнопку сохранения amoCRM. Коды действуют не более минуты. Смена режима повышает modeEpoch и отзывает старые runtime-сессии. До этого существующий standalone режим не меняется.

`GET /api/v1/widget/task-control/bootstrap` в managed возвращает `publication` с canonical rules и `runtimeRules` в legacy формате. Scope runtime-публикации отражает текущую привязку/эпоху; исторический scope неизменяемой публикации в management API сохраняется. Checksum рассчитан по нормализованному legacy JSON виджета (сортировка UID, числовых ID/воронок/статусов и обрезка пробелов имени). Runtime получает только опубликованную версию, никогда черновик.

В MVP валидация структурная (`referencesRevision=structural-v1`): существование/активность CRM ID не подтверждается, справочники и импорт ещё не реализованы. UI явно использует ручной ввод CRM ID. Расширенные source/import/history/rollback/ACK операции сохраняют 501. Список публикаций возвращает последние 100 записей; cursor пока не используется. MVP не заявляет подтверждение применения правил всеми вкладками или полный мониторинг.

## Уточнение runtime R01 (16.10.2026)

Реализованы ограниченная offline lease (до 300 секунд, только память вкладки),
renewal/retry и очистка/лимиты сессий; актуальные TTL, ошибки, совместимость
пакета 1.1.4 и граница v2 описаны в [task-control-r01-runtime.md](task-control-r01-runtime.md).
При outage последний снимок действует только до своей lease, cold offline reload
работает fail-open. Poll — 60 секунд в видимой вкладке, backoff до 300 секунд.
