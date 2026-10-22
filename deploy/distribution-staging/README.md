# Staging распределения: TeamOS

`docker-compose.staging-local.yaml` — дополнительный слой для
`deploy/docker-compose.yaml`; он не запускается самостоятельно. Переменные
TeamOS описаны в [VARIABLES.md](VARIABLES.md). Живые значения хранятся вне Git.
Указывайте `--env-file` и имя Compose-проекта явно; слой `deploy/docker-compose.ci.yaml`
нужен только соответствующему тестовому стеку.

Перед применением проверьте `compose config`. Сохраните loopback-порты и ACL
приватного моста. Миграции уже применены в staging; проверки up/down проводятся
только на изолированных тестовых БД.

Для новой среды создаются отдельные ключи и сертификаты. При обновлении
существующей среды сохраняйте ключи шифрования и версии key ring; для их замены
нужна отдельная согласованная ротация.

Core worker и nginx принадлежат репозиторию Core:
[инструкции](https://github.com/sk1fy/amocrm-pro/blob/6f14c78adc1ccb29c1c56d0f6d6ed883cb5d0471/deploy/distribution-staging/README.md),
[переменные](https://github.com/sk1fy/amocrm-pro/blob/6f14c78adc1ccb29c1c56d0f6d6ed883cb5d0471/deploy/distribution-staging/VARIABLES.md).
Ссылки закреплены на ревизию и работают без соседнего checkout. Для проверки
совместимости следующего релиза сверяйте обе стороны bridge.

[Исходники и воспроизводимость переноса 06.10.2026](SOURCE-SYNC.md).
