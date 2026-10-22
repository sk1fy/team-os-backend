# Переменные TeamOS staging

`docker-compose.staging-local.yaml` не содержит живых значений. Задайте переменные
в `.env` backend checkout (на сервере `/root/team-os-backend/.env`), права `0600`,
без добавления в Git. Перед запуском проверьте итоговую конфигурацию Compose.

**[S]** — секрет, **[I]** — идентификатор среды, **[C]** — настройка.

| Переменная | Тип | Назначение |
|---|---|---|
| `COMPANY_DISTRIBUTION_SERVICE_KEYS` | [S] | TeamOS key ring приватного моста; должен совпадать с Core |
| `COMPANY_DISTRIBUTION_KEY_ID` | [I] | ID ключа из key ring |
| `COMPANY_DISTRIBUTION_CORE_URL` | [C] | URL приватного listener Core worker |
| `DISTRIBUTION_DOMAIN` | [C] | Публичный TLS-домен |
| `TEAMOS_BRIDGE_GATEWAY` | [C] | Docker bridge gateway, на котором доступен company |
| `POSTGRES_PORT` | [C] | По умолчанию `5532`, loopback |
| `NATS_MONITOR_PORT` | [C] | По умолчанию `8223`, loopback |
| `MINIO_PORT` / `MINIO_CONSOLE_PORT` | [C] | По умолчанию `9100` / `9101`, loopback |
| `COMPANY_DISTRIBUTION_HTTP_PORT` | [C] | По умолчанию `18081`, loopback |
| `GATEWAY_HOST_PORT` | [C] | По умолчанию `8180`, loopback |

Для новой среды генерируйте отдельный shared key ring. Значения и ID ключей
Core/TeamOS должны совпадать. При обновлении существующей среды сохраняйте её
key ring; ротация выполняется отдельно. Не копируйте production-ключи в тестовую среду.

Переменные Core worker, nginx, TLS и ACL описаны в
[Core VARIABLES.md](https://github.com/sk1fy/amocrm-pro/blob/6f14c78adc1ccb29c1c56d0f6d6ed883cb5d0471/deploy/distribution-staging/VARIABLES.md)
на закреплённой ревизии. Эти шаблоны принадлежат Core и не дублируются здесь.
