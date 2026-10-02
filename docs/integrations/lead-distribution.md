# Подключение распределения через Core — РС-03

Реализован серверный слой подключений, идентичностей, справочников и прав. Назначение ответственных, бизнес-очередь и графики будут реализованы следующими этапами. Существующие distribution CRUD/simulate и legacy provider остаются совместимыми.

## Владельцы и подтверждение

Company хранит company-local `distribution_bindings`, неизменяемые версии связей, текущие сопоставления сотрудников, историю и снимки зеркала Core. Внешние Core UUID не имеют FK в другую базу. TeamOS companyId берётся из проверенной сессии; роль, активность и доступ к разделу перечитываются из текущих данных.

`POST /api/v1/distribution/connections/link-intents` принимает intentId, installationId, integrationId, accountId и одноразовый widgetToken. Это server-to-server подтверждение: owner/admin TeamOS инициирует local pending, Core проверяет точный service grant и live CRM admin. Local active появляется после подтверждения exact scope Core. Существующая компания и сотрудники не создаются заново. Срок pending — 15 минут. Повтор того же intentId сначала читает результат Core, поэтому потерянный ответ не вызывает слепого повторного расходования widget JWT.

Истёкший pending требует явного `POST .../{bindingId}/revoke`, затем нового intentId. Core revoke создаёт tombstone и блокирует запоздалое подтверждение даже ещё не видимой связи. Company освобождает текущий слот после подтверждённого revoke. Недоступный Core оставляет reservation для повторной сверки; автоматически истекать неопределённые внешние исходы нельзя.

## Идентичности и справочники

- `GET /api/v1/distribution/connections` отдаёт состояние и mappingRevision/mappingAckRevision текущей компании.
- `GET /api/v1/distribution/references?bindingId=...` получает полный свежий снимок пользователей, воронок и этапов из Core. Core проходит страницы источника. TeamOS проверяет freshness, decimal ID, дубликаты. Partial/failure возвращается ошибкой, демоданные не подставляются.
- `GET/PUT .../connections/{bindingId}/mappings` читает сопоставления и подтверждает выбранный existing UUID сотрудника с реальным CRM ID.
- `POST .../{bindingId}/mappings/reconcile` переиспользует только существующие `users.external_id` при совпадении legacy account либо `user_external_identities` с точным company/account/provider. Имена и email не являются доказательством идентичности. Повторный импорт не создаёт сотрудников. Неоднозначные соответствия требуют ручного решения; удалённые/неактивные элементы недоступны.
- `POST .../{bindingId}/mappings/sync` повторяет последний durable снимок без изменения revision/payload.

UUID, графики, группы и история сотрудников сохраняются. После удаления сотрудника остаётся mapping tombstone с nullable userId и неизменяемым userIdSnapshot, а исторические версии не имеют FK на удалённого пользователя. Деактивация немедленно переводит текущее сопоставление в unavailable; реактивация сама по себе права CRM не восстанавливает.

Снимок сопоставлений фиксируется локально вместе с monotonic revision до обращения к Core. Межсетевой вызов не держит SQL company lock. Core проверяет монотонность и idempotence версии. Пока mappingAckRevision меньше mappingRevision, доступ к сделкам закрыт. Ошибка доставки может означать уже сохранённое локальное сопоставление: читать connection/mappings и использовать sync, а не создавать другого сотрудника.

## Доступ сотрудника и виджета

`POST .../{bindingId}/lead-permission` проверяет текущего TeamOS сотрудника, company/section, active exact binding, синхронизированное mapping и live CRM ACL через Core. После сетевого ответа локальные роль/раздел/mapping/binding перечитываются. Обычный employee с distribution section участвует в сценарии; admin-only fallback не используется.

Core виджет проверяет amoCRM JWT и актуальные CRM права, затем вызывает private `POST /internal/v1/distribution/widget-access` Company. Bootstrap может опускать leadId и проверяет company/mapping/section; доступ к конкретной сделке дополнительно проверяет положительный leadId и CRM ресурсный ACL в Core. Неизвестные/удалённые пользователи, чужой company/install scope, revoked binding, pending mirror и недоступная policy не дают доступ.

## Серверные ключи и развёртывание

Настроить COMPANY_DISTRIBUTION_CORE_URL (HTTPS), COMPANY_DISTRIBUTION_KEY_ID и server-only JSON-карту COMPANY_DISTRIBUTION_SERVICE_KEYS. Callback подключается на private Company HTTP listener только при наличии ключей. Не публиковать listener наружу; TLS завершается доверенным внутренним прокси. Gateway использует существующий TeamOS JWT, браузер не получает service keys или CRM OAuth.

Оператор отдельно выдаёт exact capability `widget-access` в `distribution_service_grants` для keyId/companyId/installationId. Наличие ключа без строки grant не разрешает компанию. У Core независимые grants для исходящих TeamOS операций.

Подпись HMAC-SHA256 по строкам: keyId, method, RequestURI (включая query), company UUID, installation UUID, Unix timestamp, UUID nonce, lowercase SHA256 raw body. Заголовки X-Distribution-Key-Id/Company/Installation/Timestamp/Nonce/Signature. Допуск времени ±60 секунд, nonce хранится PostgreSQL 5 минут; replay отклоняется и после перезапуска процесса. Ротация: добавить новый keyId на обе стороны и grants, переключить sender, затем отключить старые grants и убрать старый ключ. Redirect запрещён. Upstream bodies и токены не попадают в клиентские ошибки.

Применить migration 000022 через обычный migrate runner. Down предназначен для пустого тестового подключения, не для удаления production истории. При rollback сервиса сохранять таблицы и закрывать grants.

## Проверка

`make gen`, `make check-contract`, Go тесты Company/Gateway и профильная PostgreSQL integration suite проверяют реальные миграции, duplicate/concurrent link, UUID-preserving повторный импорт, tenant rejection, mapping lifecycle/tombstone/history, partial freshness, durable mirror retry, employee ACL, concurrent section revoke, service grant/signature/nonce replay, revoke/rebind. Colima требует DOCKER_HOST с адресом существующего Docker context и TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock. Тестовая БД изолирована и удаляется тестом.

Живые OAuth/reauth/диагностика на amoCRM тестовом аккаунте требуют предоставленного тестового account/integration/installation; локальные проверки не считаются этой приёмкой.
