# Развёртывание TeamOS Backend на отдельном сервере

Этот документ — пошаговый runbook для ИИ-агента или инженера, который разворачивает backend TeamOS
на одном Linux-сервере после клонирования репозитория из Git. Он описывает текущую схему с одним
Docker Engine, одним физическим PostgreSQL-кластером и отдельной логической базой для каждого
сервиса.

Инструкции проверялись относительно:

- `deploy/docker-compose.yaml`;
- `.env.example`;
- корневого `Makefile`;
- health-check эндпоинтов сервисов.

Перед публичным запуском обязательно выполнить требования из [production-security.md](production-security.md).
Текущий основной Compose-файл публикует наружу внутренние порты и сам по себе не является
безопасной production-конфигурацией.

## 1. Итоговая схема

На одном сервере запускаются:

```text
Internet
   |
   | HTTPS :443
   v
Reverse proxy (Caddy/Nginx/Traefik)
   |
   +--> gateway :8080
           |
           +--> company       :9081 gRPC
           +--> kb            :9082 gRPC
           +--> tasks         :9083 gRPC
           +--> academy       :9084 gRPC
           +--> notifications :9085 gRPC
           +--> files         :9086 gRPC

Docker network
   +--> PostgreSQL :5432
   |      +-- teamos_company
   |      +-- teamos_kb
   |      +-- teamos_tasks
   |      +-- teamos_academy
   |      +-- teamos_notifications
   |      +-- teamos_files
   +--> NATS JetStream :4222
   +--> MinIO :9000
```

Frontend должен обращаться только к gateway. Напрямую публиковать доменные сервисы и их gRPC
порты не требуется.

## 2. Что агент должен получить до начала

Не начинать публичное развёртывание, пока неизвестны:

- адрес Git-репозитория и ветка или release tag;
- SSH-доступ к серверу;
- Linux-дистрибутив и архитектура сервера (`amd64` или `arm64`);
- домен API, например `api.example.ru`;
- домен frontend, например `app.example.ru`;
- домен файлового хранилища, например `storage.example.ru`;
- способ хранения production-секретов;
- место хранения резервных копий вне этого сервера;
- допустимое окно недоступности при первом запуске и обновлениях.

Если DNS, TLS или production Compose ещё не подготовлены, агент может выполнить только локальную
проверку на сервере через `127.0.0.1`. Нельзя временно публиковать все внутренние порты в интернет.

## 3. Требования к серверу

Рекомендуемая стартовая конфигурация для всего стека:

- Ubuntu 24.04 LTS или другая поддерживаемая Linux-система;
- 4 vCPU;
- 8 ГБ RAM; 4 ГБ допустимы для небольшого тестового стенда;
- не менее 40 ГБ SSD с возможностью расширения;
- отдельное внешнее хранилище для backup;
- статический публичный IP;
- синхронизация времени через `systemd-timesyncd`, chrony или эквивалент.

На сервере нужны:

- Git;
- Make;
- OpenSSL;
- Docker Engine;
- Docker Compose plugin;
- `curl`.

Go, PostgreSQL, NATS и MinIO на хост устанавливать не требуется: они собираются или запускаются в
контейнерах.

Docker Engine и Compose устанавливать из официального репозитория Docker. Не использовать
`curl | sh` для production-сервера. После установки проверить:

```bash
docker --version
docker compose version
sudo systemctl is-enabled docker
sudo systemctl is-active docker
```

Для `deploy/docker-compose.prod.yaml` с тегом `!override`, предложенным в
`production-security.md`, нужен Docker Compose 2.24.4 или новее.

## 4. Подготовка DNS

До включения TLS создать DNS-записи типа `A`/`AAAA`:

```text
api.example.ru      -> PUBLIC_SERVER_IP
storage.example.ru  -> PUBLIC_SERVER_IP
```

Frontend может находиться на этом же или другом сервере. Рекомендуется держать frontend и API под
одним регистрируемым доменом, например `app.example.ru` и `api.example.ru`: это упрощает корректную
работу refresh-cookie с `SameSite=Lax`.

Проверить распространение DNS:

```bash
getent ahosts api.example.ru
getent ahosts storage.example.ru
```

Не продолжать выпуск сертификатов, если адреса указывают не на целевой сервер.

## 5. Пользователь и каталог приложения

Работать постоянно под `root` не рекомендуется. Создать отдельного пользователя, например
`teamos`, и предоставить ему только необходимый доступ. Участие в группе `docker` фактически даёт
root-доступ к серверу, поэтому в эту группу нельзя добавлять недоверенных пользователей.

Пример подготовки каталога:

```bash
sudo install -d -o teamos -g teamos -m 0750 /opt/teamos
sudo -iu teamos
cd /opt/teamos
```

Если пользователь не входит в группу `docker`, выполнять Docker-команды через контролируемый
`sudo`. Не менять разрешения `/var/run/docker.sock` на всемирно доступные.

## 6. Клонирование репозитория

Для приватного репозитория настроить deploy key с доступом только на чтение. Затем:

```bash
git clone <GIT_REPOSITORY_URL> /opt/teamos/backend
cd /opt/teamos/backend
git fetch --tags --prune
git checkout <RELEASE_TAG_OR_BRANCH>
git status --short
git rev-parse HEAD
```

Агент должен записать SHA разворачиваемого коммита в отчёт о деплое. Для production предпочтителен
неизменяемый release tag или конкретный commit SHA, а не произвольное состояние ветки.

Ожидаемый `git status --short` перед созданием локальных конфигурационных файлов — пустой.

## 7. Создание `.env`

Команда проекта генерирует новую пару Ed25519 и копирует `.env.example`:

```bash
cd /opt/teamos/backend
make dev-keys
chmod 0600 .env
```

Несмотря на имя `dev-keys`, сама пара Ed25519 генерируется случайно. Однако остальные значения,
скопированные из `.env.example`, тестовые и должны быть заменены до production-запуска.

Если `.env` уже существует, `make dev-keys` намеренно откажется его перезаписывать. Агент не должен
удалять существующий `.env`: сначала определить, является ли это повторным деплоем, и сделать
защищённую резервную копию конфигурации.

Минимальные значения, которые нужно проверить или заменить:

```dotenv
POSTGRES_USER=teamos
POSTGRES_PASSWORD=<URL_SAFE_RANDOM_PASSWORD>
POSTGRES_DB=postgres

MINIO_ROOT_USER=<RANDOM_ACCESS_KEY>
MINIO_ROOT_PASSWORD=<RANDOM_SECRET_KEY>

COMPANY_JWT_PRIVATE_KEY=<GENERATED_BASE64_PRIVATE_KEY>
GATEWAY_JWT_PUBLIC_KEY=<GENERATED_BASE64_PUBLIC_KEY>
COMPANY_JWT_ISSUER=teamos-company
COMPANY_JWT_AUDIENCE=teamos-api

GATEWAY_JWT_ISSUER=teamos-company
GATEWAY_JWT_AUDIENCE=teamos-api
GATEWAY_CORS_ORIGINS=https://app.example.ru
GATEWAY_COOKIE_SECURE=true

FILES_S3_PUBLIC_ENDPOINT=storage.example.ru
FILES_S3_PUBLIC_SECURE=true
FILES_S3_BUCKET=teamos-files
FILES_S3_REGION=us-east-1
```

`deploy/docker-compose.prod.yaml` принудительно выставляет `GATEWAY_COOKIE_SECURE=true`, поэтому
dev-значение `false`, скопированное из `.env.example`, не может отключить secure-cookie в
production.

Текущий Compose подставляет пароль PostgreSQL непосредственно в URI. До перехода на file-based
secret использовать длинный случайный пароль из URL-safe символов (`A-Z`, `a-z`, `0-9`, `-`, `_`)
либо корректно percent-encode значение.

Сгенерировать подходящие значения можно локально, не выводя их в общий лог агента:

```bash
openssl rand -base64 48 | tr -d '=+/\n' | cut -c1-48
```

Не печатать содержимое `.env` командами `cat`, `env`, `docker compose config` без фильтрации и не
включать его в отчёт, issue или чат. Проверить права:

```bash
stat -c '%a %U:%G %n' .env
```

Ожидаемые права — `600`, владелец — deployment-пользователь.

Важно: `FILES_S3_SECURE` относится к внутреннему соединению files↔MinIO и внутри compose-сети
остаётся `false`. Схему presigned-ссылок задаёт отдельная переменная `FILES_S3_PUBLIC_SECURE`;
production-override `deploy/docker-compose.prod.yaml` принудительно выставляет значения `false` и
`true` соответственно. Это намеренно защищает повторный деплой от старого `.env`, в котором
`FILES_S3_SECURE` мог быть равен `true` по прежней инструкции.

## 8. Production Compose

Основной файл `deploy/docker-compose.yaml` предназначен для локальной разработки и публикует
PostgreSQL, NATS, MinIO, gateway и HTTP-порты доменных сервисов. Перед публичным запуском должен
существовать проверенный `deploy/docker-compose.prod.yaml`, удовлетворяющий разделу «Сетевая
изоляция» в `production-security.md`.

Рекомендуемая команда Compose:

```bash
export COMPOSE_PROD='docker compose --file deploy/docker-compose.yaml --file deploy/docker-compose.prod.yaml'
```

Проверить объединённую конфигурацию:

```bash
$COMPOSE_PROD config --quiet
$COMPOSE_PROD config > /tmp/teamos-compose.rendered.yaml
```

В `/tmp/teamos-compose.rendered.yaml` вручную проверить разделы `ports`. В production не должно
быть wildcard-публикации `0.0.0.0` для `5432`, `4222`, `8222`, `9000`, `9001`, `8081-8086` и
`9081-9086`. Gateway и MinIO, если TLS завершается на хосте, должны слушать только loopback:

```text
127.0.0.1:8080 -> gateway:8080
127.0.0.1:9000 -> minio:9000
```

После проверки удалить отрендеренный файл: он может содержать раскрытые переменные окружения.

```bash
rm -f /tmp/teamos-compose.rendered.yaml
```

## 9. Предварительная проверка перед первым запуском

Агент выполняет:

```bash
git status --short
docker compose version
df -h / /var/lib/docker
free -h
sudo ss -lntup
```

Проверить, что:

- диска и памяти достаточно;
- порты `80`, `443`, `8080`, `9000` не заняты неожиданными процессами;
- `.env` не отслеживается Git (`git check-ignore .env` должен находить файл);
- DNS настроен;
- backup destination доступен;
- production Compose прошёл `config --quiet`;
- секреты не совпадают со значениями из `.env.example`.

До первого запуска полезно выполнить тесты в CI. На production-сервере необязательно устанавливать
Go только ради `make test`; сервер должен получать уже проверенный commit.

## 10. Первый запуск

Запустить стек:

```bash
cd /opt/teamos/backend
$COMPOSE_PROD up --build --detach
```

При первом старте:

1. PostgreSQL создаёт отдельные базы через `deploy/postgres/init-databases.sh`.
2. Миграционные контейнеры применяют миграции каждого сервиса.
3. Запускаются NATS JetStream и MinIO.
4. Запускаются доменные сервисы.
5. Gateway запускается после зависимостей.

Следить за состоянием без бесконечного блокирования агента:

```bash
$COMPOSE_PROD ps
$COMPOSE_PROD logs --tail=200
```

Если контейнер не стал healthy, смотреть только его логи:

```bash
$COMPOSE_PROD logs --tail=300 <SERVICE_NAME>
```

Не использовать `down -v`, `docker system prune --volumes`, ручное удаление volume или миграций для
«исправления» ошибки запуска.

## 11. Проверка внутри сервера

Gateway:

```bash
curl --fail --silent --show-error http://127.0.0.1:8080/healthz
curl --fail --silent --show-error http://127.0.0.1:8080/readyz
```

Проверить итоговое состояние:

```bash
$COMPOSE_PROD ps
$COMPOSE_PROD exec -T postgres \
  sh -c 'pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

Проверить наличие отдельных баз без вывода паролей:

```bash
$COMPOSE_PROD exec -T postgres \
  sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc \
  "SELECT datname FROM pg_database WHERE datname LIKE '\''teamos_%'\'' ORDER BY datname"'
```

Ожидаются:

```text
teamos_academy
teamos_company
teamos_files
teamos_kb
teamos_notifications
teamos_tasks
```

## 12. Reverse proxy и TLS

HTTPS должен быть включён не только для API, но и для страницы frontend. Если frontend открыт по
обычному HTTP на IP-адресе, браузер считает верхнеуровневый документ небезопасным контекстом и
может отключить Web Crypto API, включая `crypto.randomUUID`, даже когда отдельный API уже доступен
по HTTPS. Для стенда нужны DNS-имена, доверенный браузером сертификат и HTTPS для обоих origin;
локальный fallback генерации UUID остаётся только защитой клиента, а не заменой TLS.

Reverse proxy должен:

- принимать `80/443`;
- автоматически обновлять TLS-сертификаты;
- перенаправлять HTTP на HTTPS;
- проксировать `api.example.ru` на `127.0.0.1:8080`;
- проксировать `storage.example.ru` на `127.0.0.1:9000`;
- передавать `Host`, `X-Forwarded-For`, `X-Forwarded-Proto`;
- поддерживать длительные соединения для SSE;
- иметь разумные timeout и ограничения размера запроса.

Пример минимального Caddyfile:

```caddyfile
api.example.ru {
    encode zstd gzip
    reverse_proxy 127.0.0.1:8080
}

storage.example.ru {
    reverse_proxy 127.0.0.1:9000
}
```

Это стартовая конфигурация, а не полный hardening. Ограничения запросов, журналирование и защита от
перегрузки описаны в `production-security.md`.

После запуска TLS проверить:

```bash
curl --fail --silent --show-error https://api.example.ru/healthz
curl --fail --silent --show-error https://api.example.ru/readyz
curl --head https://storage.example.ru/minio/health/live
```

Проверить, что HTTP перенаправляется на HTTPS:

```bash
curl --head http://api.example.ru/healthz
```

## 13. Подключение frontend

Во frontend указать базовый URL API:

```text
https://api.example.ru
```

Фактическое имя frontend-переменной нужно взять из frontend-репозитория, который является
источником правды для клиента. Backend должен содержать точный origin frontend без завершающего
пути:

```dotenv
GATEWAY_CORS_ORIGINS=https://app.example.ru
GATEWAY_COOKIE_SECURE=true
```

Если разрешено несколько frontend-origin, перечислить их в формате, поддерживаемом конфигурацией
gateway, через запятую. Нельзя использовать `*` вместе с cookie-аутентификацией.

HTTP-клиент frontend должен отправлять cookie (`credentials: "include"`). Проверить login,
refresh, logout и повторное открытие приложения после истечения access token.

## 14. Создание первых данных

Публичный REST-контракт содержит регистрацию компании/владельца. Для чистой production-базы
предпочтительно создать первую компанию штатным API/интерфейсом регистрации.

`make seed` предназначен для импортируемых frontend-фикстур. Не запускать его в production без
явного решения владельца данных, подготовленного `SEED_DIR`, проверки состава фикстур и backup.
`COMPANY_SEED_PASSWORD` из примера — development-значение и не должно использоваться как пароль
production-пользователей.

## 15. Проверка после развёртывания

Минимальный smoke-check:

1. `GET /healthz` возвращает успешный статус.
2. `GET /readyz` возвращает успешный статус.
3. Регистрация или вход работают через HTTPS.
4. В ответе устанавливается `HttpOnly; Secure` refresh-cookie.
5. Refresh access token работает.
6. CORS разрешает только production frontend.
7. Создание и чтение тестовой сущности проходят через gateway.
8. Загрузка и скачивание небольшого файла работают через `storage.example.ru`.
9. SSE-уведомления не обрываются reverse proxy преждевременно.
10. После `sudo systemctl restart docker` стек восстанавливается автоматически.

Запуск существующего e2e-сценария разрешён только после просмотра `tests/e2e/smoke.sh`: он создаёт
данные. Не запускать его против production без согласования.

## 16. Обновление

Перед каждым обновлением:

- прочитать release notes и миграции;
- убедиться, что миграции аддитивны и совместимы с предыдущей версией;
- сделать и проверить backup PostgreSQL и MinIO;
- записать текущий commit SHA и версии образов;
- проверить свободное место;
- определить окно наблюдения после релиза.

Последовательность:

```bash
cd /opt/teamos/backend
git status --short
git fetch --tags --prune
git checkout <NEW_RELEASE_TAG_OR_COMMIT>
git rev-parse HEAD
$COMPOSE_PROD config --quiet
$COMPOSE_PROD build --pull
$COMPOSE_PROD up --detach
$COMPOSE_PROD ps
curl --fail --silent --show-error https://api.example.ru/readyz
```

Затем проверить логи:

```bash
$COMPOSE_PROD logs --since=10m --tail=500
```

Не выполнять `git pull` при грязном worktree. `.env` и production override должны либо быть
игнорируемыми локальными файлами с отдельным управлением конфигурацией, либо версионироваться без
секретов. Агент не должен затирать неизвестные локальные изменения.

## 17. Откат

Откат приложения и откат базы — разные операции.

Если новая версия не меняла схему несовместимым образом:

```bash
git checkout <PREVIOUS_RELEASE_TAG_OR_COMMIT>
$COMPOSE_PROD build
$COMPOSE_PROD up --detach
curl --fail --silent --show-error https://api.example.ru/readyz
```

Если были применены миграции, нельзя автоматически выполнять `migrate down`: это может удалить
данные и нарушить совместимость. Решение об откате схемы принимается после анализа конкретных
миграций. Предпочтительная стратегия — исправляющий forward migration. В аварийной ситуации полное
восстановление выполняется из проверенного backup в соответствии с RPO/RTO.

Агент обязан остановиться и запросить решение владельца, если откат требует потери данных,
восстановления snapshot или переключения DNS.

## 18. Резервное копирование перед опасной операцией

Пример логического backup всех баз в custom format по отдельности:

```bash
sudo install -d -o "$(id -un)" -g "$(id -gn)" -m 0700 /var/backups/teamos/postgres
for db in teamos_company teamos_kb teamos_tasks teamos_academy teamos_notifications teamos_files; do
  $COMPOSE_PROD exec -T postgres \
    sh -c 'pg_dump -U "$POSTGRES_USER" -Fc "$1"' sh "$db" \
    > "/var/backups/teamos/postgres/${db}-$(date -u +%Y%m%dT%H%M%SZ).dump"
done
```

Это только локальная копия. Её необходимо зашифровать, проверить и отправить во внешнее хранилище.
Подробная политика backup и обязательная проверка восстановления описаны в
`production-security.md`.

## 19. Диагностика

### Контейнер постоянно перезапускается

```bash
$COMPOSE_PROD ps
$COMPOSE_PROD logs --tail=300 <SERVICE_NAME>
docker inspect "$($COMPOSE_PROD ps --quiet <SERVICE_NAME>)"
```

Проверить конфигурацию, доступность зависимости, миграции, память и диск. Не удалять volume.

### Gateway не становится ready

```bash
$COMPOSE_PROD logs --tail=300 gateway
$COMPOSE_PROD ps company kb tasks academy notifications files
```

Gateway зависит от готовности доменных сервисов. Найти первый нездоровый dependency и разбирать его
логи.

### Ошибка подключения к PostgreSQL

```bash
$COMPOSE_PROD exec -T postgres \
  sh -c 'pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
$COMPOSE_PROD logs --tail=200 postgres
```

Проверить совпадение credentials и наличие базы. Помнить: init-скрипты PostgreSQL исполняются только
при создании пустого volume. Изменение `POSTGRES_PASSWORD` в `.env` не меняет автоматически пароль
в уже существующей базе.

### Закончился диск

```bash
df -h
docker system df
$COMPOSE_PROD exec -T postgres sh -c \
  'psql -U "$POSTGRES_USER" -d postgres -c \
  "SELECT datname, pg_size_pretty(pg_database_size(datname)) FROM pg_database ORDER BY pg_database_size(datname) DESC"'
```

Не запускать автоматическую очистку volumes. Сначала определить потребителя: Docker images,
container logs, PostgreSQL, NATS или MinIO.

### TLS работает, но файлы открываются по HTTP

Проверить итоговую переменную внутри контейнера:

```bash
$COMPOSE_PROD exec -T files sh -c 'printf "%s\n" "$FILES_S3_PUBLIC_ENDPOINT" "$FILES_S3_PUBLIC_SECURE"'
```

Ожидаются `storage.example.ru` и `true`. Если выводится `false` или пустая строка, production
override не переопределил значение основного Compose.

## 20. Остановка

Плановая остановка без удаления данных:

```bash
$COMPOSE_PROD down --remove-orphans
```

Повторный запуск:

```bash
$COMPOSE_PROD up --detach
```

Запрещено без отдельного подтверждения владельца данных:

```bash
docker compose down --volumes
docker volume rm ...
docker system prune --volumes
```

## 21. Итоговый отчёт агента

После деплоя агент сообщает без раскрытия секретов:

- hostname и окружение;
- развернутый commit SHA/tag;
- версию Docker и Compose;
- состояние контейнеров;
- результаты `/healthz` и `/readyz`;
- результат внешнего HTTPS smoke-check;
- время и идентификатор последнего backup;
- были ли применены миграции;
- известные предупреждения;
- план отката.

Нельзя включать в отчёт `.env`, JWT-ключи, пароли, MinIO credentials, refresh/access token или
полные дампы HTTP-заголовков с cookie.
