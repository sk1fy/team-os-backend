# Отключение старого входа amoCRM

`legacy-amo-entry.locations.conf` включается через `include` внутри HTTPS `server`
TeamOS API рядом с существующим `location /api/`. Оба прежних endpoint, в том числе
со старым challenge, отвечают `403 AMO_BROWSER_ENTRY_DISABLED` без upstream.
Не добавляйте служебный `Authorization` в публичный proxy location.

Это дополнительная защита: gateway и company также безусловно отклоняют старый
bootstrap/self-login. Они не создают пользователей, не меняют роли и не выдают
access-link. `AMO_BROWSER_ADMIN_ASSERTION_ENABLED=true` запрещён в gateway;
production Compose принудительно устанавливает `false`, включая старые `.env`.
Служебная авторизация provisioning обязательна: прежний
`GATEWAY_PROVISIONING_ALLOW_UNAUTHENTICATED=true` также запрещён.
`AMO_BROWSER_CHALLENGE_SECRET` больше не используется. Справочный `/accounts/{id}/exists`
не выдаёт challenge и всегда возвращает `adminSelfLoginEligible=false`.

Новый сценарий: виджет → Core с одноразовым JWT amoCRM → служебный provisioning
TeamOS → `/auth/amo#code=...` → одноразовый `entry-exchange` с обратной проверкой
Core. См. [ADR-012](../../docs/adr/ADR-012-amo-widget-company-entry.md).
Обычные ссылки сотрудников, существующая регистрация по серверному токену,
парольный вход и служебный provisioning сохраняются.

Автоматическая проверка nginx в отдельном контейнере без изменения сервисов:

```sh
python3 scripts/test-legacy-amo-nginx.py -v
```

Фикстурный upstream подтверждает маршрутизацию новых путей; авторизацию нового
входа проверяют Go-тесты, а не этот nginx fixture. Применение include, `nginx -t`,
reload и ручная приёмка живого аккаунта выполняются отдельно при развёртывании.
