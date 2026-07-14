# Проверка всех сервисов TeamOS после production-развёртывания

Этот документ — post-deploy test runbook для ИИ-агента или инженера. Его выполняют после
[deploy.md](deploy.md) и проверки требований [production-security.md](production-security.md).

Цель — подтвердить не только ответ `/healthz`, но и полный путь запроса:

```text
клиент -> TLS/reverse proxy -> gateway -> gRPC -> доменный сервис -> его PostgreSQL
                                  |                         |
                                  +-> NATS/outbox ----------+
                                  +-> MinIO для файлов
```

Источники правды для проверки:

- `contracts/openapi/teamos.yaml` — REST-контракт;
- `tests/e2e/smoke.sh` — существующий межсервисный smoke-сценарий;
- `deploy/docker-compose.yaml` и production override — фактический runtime;
- `/healthz` и `/readyz` — liveness/readiness.

Если фактический ответ расходится с этим документом, сначала свериться с текущим OpenAPI. Не
подгонять тест под случайное поведение реализации и не редактировать сгенерированный
`services/gateway/internal/api/teamos.gen.go`.

## 1. Уровни проверки

Проверки разделены по риску:

| Уровень | Где запускать | Создаёт данные | Когда применять |
|---|---|---:|---|
| L0 | Production | Нет | После каждого deploy |
| L1 | Production | Минимально/нет | После каждого deploy |
| L2 | Выделенная test-компания | Да | Перед открытием пользователям и после значимых релизов |
| L3 | Staging/копия production | Много данных или нагрузка | Перед релизом и по расписанию |
| L4 | Изолированное restore-окружение | Восстанавливает backup | Регулярный DR drill |

Полный e2e создаёт компанию, пользователей, приглашения, статью, курс, уведомления и другие
сущности. В REST-контракте нет удаления компании, поэтому тестовые данные останутся. Не запускать
L2/L3 против реальной компании пользователя.

Агент обязан получить явное разрешение перед:

- регистрацией test-компании в production;
- созданием/изменением пользовательских данных;
- запуском load tests;
- перезапуском Docker или сервера;
- проверкой restore;
- удалением тестовых сущностей.

## 2. Критерии успеха

Deploy принят, если одновременно выполнено следующее:

- все долгоживущие контейнеры запущены, обязательные healthchecks healthy;
- внешний `/healthz` возвращает `200`;
- внешний `/readyz` возвращает `200`;
- наружу открыты только согласованные порты;
- TLS, CORS и refresh-cookie настроены правильно;
- регистрация, login, refresh и logout работают;
- каждый доменный сервис проходит хотя бы один write/read round trip;
- события outbox доходят до получателей;
- SSE доставляет notification;
- файл загружается, скачивается по HTTPS и удаляется;
- tenant isolation и проверки ролей не позволяют межфирменный доступ;
- после контролируемого рестарта данные сохраняются;
- нет новых panic, постоянных restart, 5xx и застрявшего outbox;
- backup свежий, а restore-процедура проверена отдельно.

Один успешный `/readyz` не заменяет эти проверки.

## 3. Подготовка тестового клиента

Основные проверки запускать с отдельной машины вне production-сервера. Так проверяются DNS, TLS,
firewall, reverse proxy и реальный сетевой путь.

Нужны:

- POSIX shell;
- `curl`;
- `jq`;
- `openssl`;
- `grep`, `sed`, `mktemp`;
- `nmap` только для согласованной проверки портов;
- доступ к репозиторию с тем же commit SHA, что развёрнут на сервере.

Задать значения без завершающего `/`:

```bash
export BASE_URL=https://api.example.ru
export STORAGE_URL=https://storage.example.ru
export FRONTEND_ORIGIN=https://app.example.ru
export EXPECTED_COMMIT=<DEPLOYED_COMMIT_SHA>
```

Создать временный каталог с закрытыми правами:

```bash
TEST_DIR=$(mktemp -d)
chmod 0700 "$TEST_DIR"
```

В нём будут cookie jar и ответы с access token. После проверки каталог нужно удалить. Не сохранять
его в CI artifacts и не включать содержимое в отчёт.

Тестовые значения:

```bash
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
OWNER_EMAIL="owner-${RUN_ID}@e2e.test"
MEMBER_EMAIL="member-${RUN_ID}@e2e.test"
E2E_PASSWORD="$(openssl rand -base64 36 | tr -d '=+/\n' | cut -c1-28)Aa1"
OWNER_COOKIE_JAR="$TEST_DIR/owner.cookies"
MEMBER_COOKIE_JAR="$TEST_DIR/member.cookies"
```

Не выводить пароль и access tokens в терминал, который собирается централизованно.

## 4. Фиксация контекста до теста

На сервере записать:

```bash
cd /opt/teamos/backend
git rev-parse HEAD
docker --version
docker compose version
docker compose \
  --file deploy/docker-compose.yaml \
  --file deploy/docker-compose.prod.yaml \
  ps
date -u
```

Убедиться, что commit совпадает с `EXPECTED_COMMIT`, а worktree не содержит неизвестных изменений:

```bash
git status --short
```

Локальные `.env` и production override могут быть ожидаемыми untracked/ignored файлами. Любые
изменения исходного кода на сервере требуют расследования до теста.

Зафиксировать начальные restart count и последние ошибки:

```bash
docker compose \
  --file deploy/docker-compose.yaml \
  --file deploy/docker-compose.prod.yaml \
  ps --format json

docker compose \
  --file deploy/docker-compose.yaml \
  --file deploy/docker-compose.prod.yaml \
  logs --since=15m --tail=1000 \
  | grep -Ei 'panic|fatal|segmentation|out of memory|migration.*error' || true
```

Отсутствие совпадений не доказывает отсутствие ошибок; также проверить structured log levels и
container state.

## 5. L0: внешняя инфраструктурная проверка

### 5.1 DNS

```bash
getent ahosts "${BASE_URL#https://}"
getent ahosts "${STORAGE_URL#https://}"
```

Ожидаются IP целевого reverse proxy. Проверить и `A`, и `AAAA`, если IPv6 включён. Старый или
нефильтруемый IPv6-адрес считается ошибкой безопасности.

### 5.2 TLS

```bash
curl --fail --silent --show-error --output /dev/null "$BASE_URL/healthz"
curl --fail --silent --show-error --output /dev/null "$STORAGE_URL/minio/health/live"
```

Проверить сертификат API:

```bash
openssl s_client \
  -connect "${BASE_URL#https://}:443" \
  -servername "${BASE_URL#https://}" \
  -verify_return_error </dev/null 2>/dev/null \
  | openssl x509 -noout -subject -issuer -dates
```

Критерии:

- цепочка доверена;
- SAN содержит нужный hostname;
- сертификат не истекает в ближайшее согласованное окно;
- запрос по HTTP перенаправляется на HTTPS;
- API не доступен через публичный plaintext `:8080`;
- MinIO не формирует HTTP-ссылки.

Проверка redirect:

```bash
curl --silent --show-error --head "http://${BASE_URL#https://}/healthz"
```

Ожидается redirect на `https://...`, а не успешный plaintext API response.

### 5.3 Публичные порты

Запускать только после согласования сетевого сканирования:

```bash
nmap -Pn -p 22,80,443,5432,4222,8222,9000,9001,8080-8086,9081-9086 \
  "${BASE_URL#https://}"
```

Ожидаются только `80/443` и, если разрешён с адреса тестового клиента, `22`. Внутренние порты
должны быть `closed` или `filtered`.

### 5.4 Security headers

```bash
curl --silent --show-error --dump-header - --output /dev/null "$BASE_URL/healthz"
```

Проверить:

- нет утечки версии внутреннего сервиса;
- HTTPS отвечает ожидаемым HSTS, если HSTS уже включён;
- `X-Content-Type-Options` и остальные согласованные headers присутствуют;
- reverse proxy не добавляет permissive CORS ко всем ответам;
- отсутствуют `Set-Cookie` на health endpoint.

## 6. L0: health и readiness

С внешней машины:

```bash
curl --fail --silent --show-error \
  --connect-timeout 5 --max-time 15 \
  "$BASE_URL/healthz" | jq .

curl --fail --silent --show-error \
  --connect-timeout 5 --max-time 15 \
  "$BASE_URL/readyz" | jq .
```

Оба запроса должны вернуть HTTP `200`. Если body не JSON, убрать `jq` и сверить фактический
контракт health-response.

Смысл проверок:

- `/healthz` подтверждает, что gateway process жив;
- `/readyz` gateway проверяет gRPC health `company`, `kb`, `tasks` и другие настроенные зависимости;
- health состояния Compose дополнительно проверяют PostgreSQL/NATS/MinIO и сервисные readyz.

На сервере:

```bash
COMPOSE='docker compose --file deploy/docker-compose.yaml --file deploy/docker-compose.prod.yaml'
$COMPOSE ps
```

Ни один долгоживущий сервис не должен быть `Exited`, `Restarting` или `unhealthy`. Миграционные и
init-контейнеры должны завершиться с exit code `0`; для них `Exited (0)` нормально.

Если нужно проверить внутренний HTTP `readyz`, не открывать порт. Разрешено запустить одноразовый
диагностический curl-контейнер в Docker network, заранее доверяя/закрепив его image:

```bash
for service_port in \
  company:8081 kb:8082 tasks:8083 academy:8084 notifications:8085 files:8086; do
  docker run --rm --network teamos_default curlimages/curl:8.12.1 \
    --fail --silent --show-error --max-time 5 \
    "http://${service_port}/readyz"
done
```

Если production network имеет другое имя, получить его через `docker network ls` и не угадывать.
Не оставлять диагностический контейнер запущенным.

## 7. L0: инфраструктурные зависимости

На сервере использовать тот же набор Compose-файлов:

```bash
COMPOSE='docker compose --file deploy/docker-compose.yaml --file deploy/docker-compose.prod.yaml'
```

### 7.1 PostgreSQL

```bash
$COMPOSE exec -T postgres \
  sh -c 'pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

Проверить базы:

```bash
$COMPOSE exec -T postgres sh -c \
  'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc \
  "SELECT datname FROM pg_database WHERE datname LIKE '\''teamos_%'\'' ORDER BY datname"'
```

Ожидаются `teamos_company`, `teamos_kb`, `teamos_tasks`, `teamos_academy`,
`teamos_notifications`, `teamos_files`.

Проверить состояние миграций каждой базы:

```bash
for db in teamos_company teamos_kb teamos_tasks teamos_academy teamos_notifications teamos_files; do
  printf '%s: ' "$db"
  $COMPOSE exec -T postgres sh -c \
    'psql -U "$POSTGRES_USER" -d "$1" -Atc \
    "SELECT version || '\''/dirty='\'' || dirty FROM schema_migrations"' sh "$db"
done
```

`dirty` должен быть `false`. Номер версии сравнить с последним файлом миграции соответствующего
сервиса; нельзя считать любой номер автоматически правильным.

### 7.2 NATS JetStream

Проверить `nats` и `nats-init` в `$COMPOSE ps --all`. Init должен завершиться успешно, основной
контейнер — быть healthy.

При наличии доверенного `nats-box`:

```bash
docker run --rm --network teamos_default natsio/nats-box:0.19.7 \
  nats --server nats://nats:4222 stream info TEAMOS
```

Проверить:

- stream `TEAMOS` существует;
- subjects включают `teamos.>`;
- storage — file;
- нет неожиданного большого consumer lag;
- размер stream согласуется с лимитами;
- в логах нет постоянных publish/reconnect ошибок.

### 7.3 MinIO

```bash
curl --fail --silent --show-error "$STORAGE_URL/minio/health/live"
curl --fail --silent --show-error "$STORAGE_URL/minio/health/ready"
```

MinIO console `:9001` не должна быть публична. Bucket `teamos-files` должен существовать, но root
credentials нельзя выводить в тестовый лог.

## 8. L1: OpenAPI и базовые ошибки

### 8.1 Неавторизованный доступ

```bash
code=$(curl --silent --show-error --output "$TEST_DIR/error.json" \
  --write-out '%{http_code}' "$BASE_URL/api/v1/auth/me")
test "$code" = 401
jq -e '.error.status == 401 and (.error.message | type == "string")' "$TEST_DIR/error.json"
```

Проверить, что ошибка имеет формат:

```json
{"error":{"message":"...","status":401}}
```

Текст должен быть безопасным и на русском, без SQL, stack trace, адресов сервисов и секретов.

### 8.2 Некорректный UUID

```bash
code=$(curl --silent --show-error --output "$TEST_DIR/error.json" \
  --write-out '%{http_code}' \
  -H 'Authorization: Bearer invalid-test-token' \
  "$BASE_URL/api/v1/kb/articles/not-a-uuid")
```

Из-за невалидного токена ожидается `401`; для проверки UUID использовать действительный test token
после регистрации. Тогда ожидается `400`, а не `500`.

### 8.3 Неизвестные поля и неправильный content type

После получения test token отправить в один безопасный create endpoint неизвестное поле и
проверить `400`. Отдельно отправить JSON без `Content-Type: application/json`. Реализация должна
отклонять некорректный ввод согласно OpenAPI, а не молча сохранять его.

Не выполнять эти проверки на endpoint, который может частично записать данные до валидации.

## 9. L2: основной автоматический e2e

Существующий сценарий проверяет:

```text
register
  -> invite -> accept
  -> SSE connect
  -> KB section/article publish
  -> notification REST + SSE
  -> acknowledgement
  -> academy link-course
  -> article update
  -> event-driven lesson update
  -> gateway health/readiness
```

Он создаёт отдельную компанию и уникальные email на каждом запуске. Удаления компании нет.

Запускать из checkout того же commit:

```bash
cd /path/to/team-os-backend
BASE_URL="$BASE_URL" \
E2E_PASSWORD="$E2E_PASSWORD" \
E2E_TIMEOUT=60 \
tests/e2e/smoke.sh
```

Для production запуск требует разрешения на создание постоянных тестовых данных. Не использовать
пароль по умолчанию из скрипта.

Успех — финальная строка:

```text
E2E smoke успешно завершён: register → invite → accept → publish → ack → link-course → sync → SSE.
```

Если сценарий падает на асинхронном шаге, сначала проверить outbox/NATS/consumer lag. Не увеличивать
timeout бесконечно: доставка за пределами согласованного SLO считается дефектом.

Скрипт не проверяет все endpoints. Следующие разделы закрывают пробелы.

## 10. L2: подготовка общей test-компании вручную

Для расширенных сценариев зарегистрировать отдельную компанию и сохранить response/cookie:

```bash
REGISTER_BODY=$(jq -nc \
  --arg company "Приёмка $RUN_ID" \
  --arg email "$OWNER_EMAIL" \
  --arg password "$E2E_PASSWORD" \
  '{companyName:$company,email:$email,password:$password,firstName:"Тест",lastName:"Владелец"}')

code=$(curl --silent --show-error \
  --output "$TEST_DIR/register.json" \
  --dump-header "$TEST_DIR/register.headers" \
  --cookie-jar "$OWNER_COOKIE_JAR" \
  --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  --data "$REGISTER_BODY" \
  "$BASE_URL/api/v1/auth/register")

test "$code" = 201
OWNER_TOKEN=$(jq -er '.accessToken' "$TEST_DIR/register.json")
OWNER_ID=$(jq -er '.user.id' "$TEST_DIR/register.json")
```

Не использовать `set -x`: он раскроет password и token.

Проверить cookie headers без публикации cookie value:

```bash
grep -i '^set-cookie:' "$TEST_DIR/register.headers" \
  | sed -E 's/(teamos_refresh=)[^;]+/\1<redacted>/'
```

Обязательные атрибуты: `HttpOnly`, `Secure`, `SameSite=Lax`, path `/api/v1/auth`. В JSON не должно
быть `refreshToken`.

Удобная функция только для test shell:

```bash
auth_curl() {
  curl --silent --show-error \
    -H "Authorization: Bearer $OWNER_TOKEN" \
    -H 'Content-Type: application/json' \
    "$@"
}
```

Не печатать команду через shell tracing.

## 11. Gateway и authentication

### 11.1 Current user

```bash
auth_curl "$BASE_URL/api/v1/auth/me" > "$TEST_DIR/me.json"
jq -e --arg id "$OWNER_ID" '.id == $id and .role == "owner"' "$TEST_DIR/me.json"
```

### 11.2 Login

```bash
LOGIN_BODY=$(jq -nc --arg email "$OWNER_EMAIL" --arg password "$E2E_PASSWORD" \
  '{email:$email,password:$password}')

code=$(curl --silent --show-error \
  --output "$TEST_DIR/login.json" \
  --cookie-jar "$OWNER_COOKIE_JAR" \
  --write-out '%{http_code}' \
  -H 'Content-Type: application/json' \
  --data "$LOGIN_BODY" \
  "$BASE_URL/api/v1/auth/login")
test "$code" = 200
LOGIN_TOKEN=$(jq -er '.accessToken' "$TEST_DIR/login.json")
```

Неверный пароль должен вернуть `401` и одинаково безопасный текст для существующего и
несуществующего email, чтобы не облегчать enumeration.

### 11.3 Refresh rotation

```bash
code=$(curl --silent --show-error \
  --output "$TEST_DIR/refresh.json" \
  --cookie "$OWNER_COOKIE_JAR" \
  --cookie-jar "$OWNER_COOKIE_JAR" \
  --write-out '%{http_code}' \
  --request POST \
  "$BASE_URL/api/v1/auth/refresh")
test "$code" = 200
REFRESHED_TOKEN=$(jq -er '.accessToken' "$TEST_DIR/refresh.json")
test "$REFRESHED_TOKEN" != "$LOGIN_TOKEN"
```

Для строгой проверки rotation сохранить старый cookie jar до refresh и убедиться, что повторное
использование старого refresh token отклоняется. Копия содержит секрет и должна быть удалена сразу
после теста.

### 11.4 Logout

Logout выполнять в конце расширенного сценария, иначе cookie-сессия станет недействительной:

```bash
curl --silent --show-error --output /dev/null \
  --cookie "$OWNER_COOKIE_JAR" \
  --request POST "$BASE_URL/api/v1/auth/logout"
```

После logout refresh с тем же jar должен вернуть `401`.

### 11.5 Auth rate limit

Gateway содержит локальный лимит 30 запросов в минуту на `/api/v1/auth/*` по адресу непосредственного
клиента. Проверять `429` только в staging: тест временно блокирует auth-запросы с этого адреса.

Критически проверить конфигурацию reverse proxy: если gateway всегда видит один proxy IP и не
обрабатывает доверенную цепочку client IP, один атакующий может исчерпать общий лимит для всех.
Это отдельный security finding, а не повод отключать limiter.

## 12. Сервис company: компания, оргструктура и приглашения

### 12.1 Компания

```bash
auth_curl "$BASE_URL/api/v1/company" > "$TEST_DIR/company.json"
COMPANY_ID=$(jq -er '.id' "$TEST_DIR/company.json")
jq -e --arg owner "$OWNER_ID" '.ownerId == $owner' "$TEST_DIR/company.json"
```

Выполнить безопасный PATCH имени test-компании и повторный GET. Проверить сохранение и camelCase.

### 12.2 Отдел и должность

```bash
DEPARTMENT_BODY=$(jq -nc --arg name "Отдел $RUN_ID" '{name:$name,parentId:null}')
auth_curl --request POST --data "$DEPARTMENT_BODY" \
  "$BASE_URL/api/v1/org/departments" > "$TEST_DIR/department.json"
DEPARTMENT_ID=$(jq -er '.id' "$TEST_DIR/department.json")

POSITION_BODY=$(jq -nc --arg name "Инженер $RUN_ID" --arg dep "$DEPARTMENT_ID" \
  '{name:$name,departmentId:$dep,level:1,description:"Тестовая должность"}')
auth_curl --request POST --data "$POSITION_BODY" \
  "$BASE_URL/api/v1/org/positions" > "$TEST_DIR/position.json"
POSITION_ID=$(jq -er '.id' "$TEST_DIR/position.json")
```

Проверить:

- GET списков возвращает новые UUID;
- PATCH переименовывает сущность;
- move не создаёт цикл отдела;
- удаление непустого отдела отклоняется русской `400` ошибкой;
- удаление должности снимает её с сотрудников согласно контракту;
- employee не может изменять оргструктуру.

### 12.3 Приглашение и сотрудник

```bash
INVITE_BODY=$(jq -nc --arg email "$MEMBER_EMAIL" --arg position "$POSITION_ID" \
  '{email:$email,role:"employee",positionId:$position}')
auth_curl --request POST --data "$INVITE_BODY" \
  "$BASE_URL/api/v1/org/invites" > "$TEST_DIR/invite.json"
INVITE_ID=$(jq -er '.id' "$TEST_DIR/invite.json")
INVITE_TOKEN=$(jq -er '.token' "$TEST_DIR/invite.json")
```

Точную схему `InviteUserInput` всегда сверять с OpenAPI текущего commit; если поле назначения
называется иначе, тест не должен угадывать.

Проверить публичный GET invite, принять приглашение, сохранить member token/cookie:

```bash
curl --fail --silent --show-error \
  "$BASE_URL/api/v1/auth/invites/$INVITE_TOKEN" > "$TEST_DIR/invite-public.json"

ACCEPT_BODY=$(jq -nc --arg password "$E2E_PASSWORD" \
  '{firstName:"Тест",lastName:"Сотрудник",password:$password}')
curl --fail --silent --show-error \
  --cookie-jar "$MEMBER_COOKIE_JAR" \
  -H 'Content-Type: application/json' \
  --data "$ACCEPT_BODY" \
  "$BASE_URL/api/v1/auth/invites/$INVITE_TOKEN/accept" > "$TEST_DIR/accept.json"

MEMBER_TOKEN=$(jq -er '.accessToken' "$TEST_DIR/accept.json")
MEMBER_ID=$(jq -er '.user.id' "$TEST_DIR/accept.json")
```

Повторное принятие того же token должно быть отклонено. Resend/revoke проверять отдельным вторым
invite, чтобы не ломать основной сценарий.

## 13. Сервис company: графики

Сохранить недельный график test-сотрудника:

```bash
SCHEDULE_BODY=$(jq -nc \
  '{template:{type:"week",days:[1,2,3,4,5],start:"09:00",end:"18:00"}}')
auth_curl --request PUT --data "$SCHEDULE_BODY" \
  "$BASE_URL/api/v1/schedule/$MEMBER_ID" > "$TEST_DIR/schedule.json"
```

Проверить через `GET /api/v1/schedule`, что:

- `userId` совпадает;
- тип `week` и дни сохранились;
- время не изменилось из-за timezone;
- `start < end`;
- employee не может менять чужой график, если роль не разрешает.

Исключение на текущий месяц:

```bash
TEST_DATE=$(date -u +%Y-%m-15)
MONTH=$(date -u +%Y-%m)
EXCEPTIONS_BODY=$(jq -nc --arg user "$MEMBER_ID" --arg date "$TEST_DATE" \
  '[{userId:$user,date:$date,type:"vacation",note:"Приёмочный тест"}]')
auth_curl --request PUT --data "$EXCEPTIONS_BODY" \
  "$BASE_URL/api/v1/schedule/exceptions" > "$TEST_DIR/exceptions.json"
auth_curl "$BASE_URL/api/v1/schedule/exceptions?month=$MONTH" \
  > "$TEST_DIR/exceptions-get.json"
jq -e --arg user "$MEMBER_ID" --arg date "$TEST_DATE" \
  'any(.[]; .userId == $user and .date == $date and .type == "vacation")' \
  "$TEST_DIR/exceptions-get.json"
```

Отдельно в staging проверить cycle template, work exception с обязательными start/end и отклонение
некорректного диапазона времени.

## 14. Сервис company: распределение сделок

Создать группу из владельца и сотрудника:

```bash
GROUP_BODY=$(jq -nc --arg name "Распределение $RUN_ID" \
  --arg owner "$OWNER_ID" --arg member "$MEMBER_ID" \
  '{name:$name,memberIds:[$owner,$member]}')
auth_curl --request POST --data "$GROUP_BODY" \
  "$BASE_URL/api/v1/distribution/groups" > "$TEST_DIR/group.json"
GROUP_ID=$(jq -er '.id' "$TEST_DIR/group.json")
```

Выполнить несколько `POST /api/v1/distribution/groups/{groupId}/simulate` последовательно и
проверить:

- HTTP `201`;
- `groupId` совпадает;
- назначенный `userId` входит в `memberIds`;
- round-robin не выбирает одного пользователя постоянно;
- номера сделок возрастают;
- GET events возвращает новые события первыми;
- disabled member не выбирается после PATCH;
- delete events очищает только события этой группы;
- другая компания не видит группу.

Concurrency-проверку распределения выполнять в staging: параллельные simulate не должны создавать
дублирующий порядок или нарушать лимиты.

## 15. Сервис kb

Основной e2e уже проверяет create section, publish article, acknowledgement, version update и
синхронизацию link-урока. Расширить:

1. GET sections содержит созданный раздел.
2. GET article возвращает TipTap JSON, не HTML.
3. Search находит заголовок/содержимое только доступной статьи.
4. PATCH создаёт новую версию.
5. GET versions возвращает версии в ожидаемом порядке.
6. Rollback по `versionId` восстанавливает title/content и создаёт корректную новую историю.
7. Draft не создаёт publish notification.
8. Published с `requiresAcknowledgement=true` доступна сотруднику согласно access settings.
9. Повторный acknowledge идемпотентен или возвращает документированный ответ.
10. Пользователь без доступа не может получить статью по известному UUID.

Пример валидного rich-text:

```bash
CONTENT=$(jq -nc \
  '{type:"doc",content:[{type:"paragraph",content:[{type:"text",text:"Проверка TipTap"}]}]}')
```

Негативные тесты:

- HTML string вместо TipTap JSON;
- объект без `type: "doc"`;
- слишком большой/deep JSON;
- несуществующий sectionId;
- sectionId другой компании;
- rollback с versionId другой статьи.

Все должны завершаться контролируемой `4xx`, никогда `500`.

## 16. Сервис tasks

### 16.1 Обязательное предварительное условие

В текущем OpenAPI есть `GET /tasks/boards`, но нет endpoint создания доски. Чистая компания после
register может получить пустой список, если доски не созданы импортом/seed/bootstrap-процессом.

```bash
auth_curl "$BASE_URL/api/v1/tasks/boards" > "$TEST_DIR/boards.json"
BOARD_ID=$(jq -er '.[0].id' "$TEST_DIR/boards.json")
```

Если список пуст:

- не вставлять board напрямую SQL в production;
- отметить Tasks write scenario как `BLOCKED: отсутствует test board`;
- проверить, должен ли bootstrap создавать personal board;
- для staging импортировать согласованные fixtures через штатный seed-процесс;
- открыть отдельную задачу на публичный/bootstrap-механизм создания доски, если это требуется
  продуктом.

Пустой список не означает, что сервис упал, но не позволяет полноценно принять write-контракт.

### 16.2 Колонки и задача

При наличии test board:

```bash
COLUMN_A_BODY=$(jq -nc --arg name "Бэклог $RUN_ID" '{name:$name,color:"#808080"}')
auth_curl --request POST --data "$COLUMN_A_BODY" \
  "$BASE_URL/api/v1/tasks/boards/$BOARD_ID/columns" > "$TEST_DIR/column-a.json"
COLUMN_A_ID=$(jq -er '.id' "$TEST_DIR/column-a.json")

COLUMN_B_BODY=$(jq -nc --arg name "Готово $RUN_ID" '{name:$name,color:"#00aa00"}')
auth_curl --request POST --data "$COLUMN_B_BODY" \
  "$BASE_URL/api/v1/tasks/boards/$BOARD_ID/columns" > "$TEST_DIR/column-b.json"
COLUMN_B_ID=$(jq -er '.id' "$TEST_DIR/column-b.json")

TASK_BODY=$(jq -nc --arg board "$BOARD_ID" --arg column "$COLUMN_A_ID" \
  --arg title "Задача $RUN_ID" \
  '{boardId:$board,columnId:$column,title:$title,priority:"high"}')
auth_curl --request POST --data "$TASK_BODY" \
  "$BASE_URL/api/v1/tasks" > "$TEST_DIR/task.json"
TASK_ID=$(jq -er '.id' "$TEST_DIR/task.json")
```

Проверить:

- GET task и list by `boardId`;
- PATCH title, TipTap description, assigneeIds и watcherIds;
- назначение `MEMBER_ID` создаёт notification сотруднику;
- add/get comment сохраняет TipTap JSON и authorId;
- move в `COLUMN_B_ID` с `order:0` сохраняет column/order;
- labels GET не возвращает данные другой компании;
- неправильное сочетание boardId/columnId отклоняется;
- employee не может менять структуру доски, если роль не разрешает;
- параллельные move в staging не создают одинаковый/сломанный порядок;
- completion/recurrence создаёт ожидаемую следующую задачу согласно доменным правилам.

Пример comment:

```bash
COMMENT_CONTENT=$(jq -nc \
  '{type:"doc",content:[{type:"paragraph",content:[{type:"text",text:"Комментарий"}]}]}')
COMMENT_BODY=$(jq -nc --argjson content "$COMMENT_CONTENT" '{content:$content}')
```

## 17. Сервис academy

Основной e2e проверяет link-course и event-driven обновление урока из KB. Для полного round trip
создать обычный курс:

```bash
COURSE_BODY=$(jq -nc --arg title "Курс $RUN_ID" \
  '{title:$title,description:"Приёмка",status:"published",sequential:true,deadlineDays:7}')
auth_curl --request POST --data "$COURSE_BODY" \
  "$BASE_URL/api/v1/academy/courses" > "$TEST_DIR/course.json"
COURSE_ID=$(jq -er '.id' "$TEST_DIR/course.json")

COURSE_SECTION_BODY=$(jq -nc '{title:"Раздел 1"}')
auth_curl --request POST --data "$COURSE_SECTION_BODY" \
  "$BASE_URL/api/v1/academy/courses/$COURSE_ID/sections" \
  > "$TEST_DIR/course-section.json"
COURSE_SECTION_ID=$(jq -er '.id' "$TEST_DIR/course-section.json")

LESSON_CONTENT=$(jq -nc \
  '{type:"doc",content:[{type:"paragraph",content:[{type:"text",text:"Урок"}]}]}')
LESSON_BODY=$(jq -nc --arg course "$COURSE_ID" --arg section "$COURSE_SECTION_ID" \
  --argjson content "$LESSON_CONTENT" \
  '{courseId:$course,sectionId:$section,title:"Урок 1",content:$content}')
auth_curl --request POST --data "$LESSON_BODY" \
  "$BASE_URL/api/v1/academy/lessons" > "$TEST_DIR/lesson.json"
LESSON_ID=$(jq -er '.id' "$TEST_DIR/lesson.json")
```

Проверить GET/PATCH/move курса, раздела и урока.

Создать quiz. UUID question/option генерировать на test client:

```bash
QUESTION_ID=$(cat /proc/sys/kernel/random/uuid)
OPTION_A_ID=$(cat /proc/sys/kernel/random/uuid)
OPTION_B_ID=$(cat /proc/sys/kernel/random/uuid)
QUIZ_BODY=$(jq -nc --arg lesson "$LESSON_ID" --arg q "$QUESTION_ID" \
  --arg a "$OPTION_A_ID" --arg b "$OPTION_B_ID" \
  '{lessonId:$lesson,passingScore:100,maxAttempts:3,questions:[
    {id:$q,type:"single",text:"Выберите правильный ответ",options:[
      {id:$a,text:"Правильно",correct:true},
      {id:$b,text:"Неправильно",correct:false}
    ]}
  ]}')
auth_curl --request PUT --data "$QUIZ_BODY" \
  "$BASE_URL/api/v1/academy/quizzes" > "$TEST_DIR/quiz.json"
```

На macOS вместо `/proc/sys/kernel/random/uuid` использовать `uuidgen | tr 'A-Z' 'a-z'`.

Назначить курс сотруднику:

```bash
ASSIGN_BODY=$(jq -nc --arg course "$COURSE_ID" --arg user "$MEMBER_ID" \
  '{courseId:$course,assigneeType:"user",assigneeId:$user}')
auth_curl --request POST --data "$ASSIGN_BODY" \
  "$BASE_URL/api/v1/academy/assignments" > "$TEST_DIR/assignment.json"
```

Проверить:

- сотрудник получает `course_assigned` notification;
- assignment виден только в своей компании;
- member GET lessons/course работает;
- member отмечает lesson complete с `{courseId:...}`;
- progress содержит lesson ID;
- sequential course не позволяет перескочить обязательный порядок;
- quiz passingScore/maxAttempts валидируются;
- copy-курс не меняется после изменения исходной KB-статьи;
- link-курс меняется после события KB;
- удаление lesson очищает quiz/progress согласно контракту;
- удаление course каскадно удаляет его внутренние сущности, но не KB-статью.

Destructive delete проверять последним и только на test-course.

## 18. Сервис notifications и SSE

### 18.1 REST

С member token:

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer $MEMBER_TOKEN" \
  "$BASE_URL/api/v1/notifications" > "$TEST_DIR/notifications.json"

curl --fail --silent --show-error \
  -H "Authorization: Bearer $MEMBER_TOKEN" \
  "$BASE_URL/api/v1/notifications/unread-count" > "$TEST_DIR/unread.json"
```

Проверить unread count, выбрать test-notification, вызвать `{id}/read`, затем `read-all`. Значение
count должно уменьшаться и не становиться отрицательным. Повторный mark-read должен быть безопасен.

### 18.2 SSE

Подключить поток до действия, создающего notification:

```bash
curl --silent --show-error --no-buffer --max-time 75 \
  -H "Authorization: Bearer $MEMBER_TOKEN" \
  -H 'Accept: text/event-stream' \
  "$BASE_URL/api/v1/notifications/stream" \
  > "$TEST_DIR/events.sse" 2> "$TEST_DIR/events.err" &
SSE_PID=$!
```

После подключения создать publish/assignment/comment event. Ожидать ограниченное время, затем:

```bash
grep -q '^event: notification' "$TEST_DIR/events.sse"
grep -q '^data: {' "$TEST_DIR/events.sse"
kill "$SSE_PID" 2>/dev/null || true
wait "$SSE_PID" 2>/dev/null || true
```

Проверить:

- `Content-Type: text/event-stream`;
- `Cache-Control: no-cache`;
- событие приходит без proxy buffering;
- соединение не обрывается на коротком timeout reverse proxy;
- недействительный token получает `401`, а не открытый stream;
- событие компании A не приходит пользователю компании B;
- REST и SSE не создают дубликаты persisted notification сверх ожидаемой семантики;
- frontend корректно переподключается после потери сети.

## 19. Сервис files и MinIO

Использовать маленький безопасный тестовый файл без персональных данных:

```bash
printf 'TeamOS file smoke %s\n' "$RUN_ID" > "$TEST_DIR/upload.txt"
```

Upload:

```bash
code=$(curl --silent --show-error \
  --output "$TEST_DIR/upload.json" \
  --write-out '%{http_code}' \
  -H "Authorization: Bearer $OWNER_TOKEN" \
  --form 'purpose=attachment' \
  --form "file=@$TEST_DIR/upload.txt;type=text/plain" \
  "$BASE_URL/api/v1/files")
test "$code" = 201
FILE_ID=$(jq -er '.id' "$TEST_DIR/upload.json")
```

Metadata и presigned URL:

```bash
auth_curl "$BASE_URL/api/v1/files/$FILE_ID" > "$TEST_DIR/file.json"
DOWNLOAD_URL=$(jq -er '.downloadUrl' "$TEST_DIR/file.json")
case "$DOWNLOAD_URL" in
  https://storage.example.ru/*) ;;
  *) printf 'Некорректный downloadUrl: %s\n' "$DOWNLOAD_URL" >&2; exit 1 ;;
esac

curl --fail --silent --show-error "$DOWNLOAD_URL" > "$TEST_DIR/download.txt"
cmp "$TEST_DIR/upload.txt" "$TEST_DIR/download.txt"
```

Проверить:

- URL только HTTPS и с production storage hostname;
- content type/name/size совпадают;
- presigned URL ограничен по времени;
- чужая компания не может получить metadata/presigned URL по UUID;
- слишком большой файл получает контролируемую `4xx`, а не обрывает gateway;
- пустой файл, запрещённый purpose и malformed multipart отклоняются;
- имя файла не позволяет path traversal;
- удаление metadata удаляет/делает недоступным object;
- повторный delete возвращает документированный `404/204`, не `500`.

Удалить только созданный test-file:

```bash
code=$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  -H "Authorization: Bearer $OWNER_TOKEN" \
  --request DELETE "$BASE_URL/api/v1/files/$FILE_ID")
test "$code" = 204
```

После удаления старый presigned URL не должен выдавать содержимое. Учесть возможный CDN cache, если
он появится в архитектуре.

## 20. Межсервисные события и outbox

Основные цепочки:

| Источник | Событие/действие | Получатель | Проверяемый эффект |
|---|---|---|---|
| company | пользователь/приглашение | связанные сервисы | tenant/user context доступен |
| kb | публикация статьи | notifications | REST и SSE notification |
| kb | изменение статьи | academy | link-урок обновлён |
| tasks | назначение/комментарий | notifications | notification нужному пользователю |
| academy | назначение курса | notifications | `course_assigned` |

Для каждой цепочки фиксировать время действия и время появления результата. Асинхронный poll:

- интервал 1 секунда;
- общий timeout 30–60 секунд;
- по timeout тест падает;
- не создавать повторно одно и то же действие в каждой итерации.

Проверить unpublished outbox rows. Схема outbox есть у `company`, `kb`, `tasks`, `academy`:

```bash
for db in teamos_company teamos_kb teamos_tasks teamos_academy; do
  printf '%s: ' "$db"
  $COMPOSE exec -T postgres sh -c \
    'psql -U "$POSTGRES_USER" -d "$1" -Atc \
    "SELECT count(*) FROM outbox WHERE published_at IS NULL"' sh "$db"
done
```

Сразу после операции кратковременное ненулевое значение допустимо. Оно должно уменьшиться. Строки
с растущими `attempts`/`last_error` или старым `next_attempt_at` требуют расследования.

Не удалять outbox rows вручную для прохождения теста.

## 21. Tenant isolation

Это обязательный security test. Создать вторую test-компанию B и получить `TENANT_B_TOKEN`. Затем
попробовать с token B получить UUID ресурсов компании A:

- company/user/department/position;
- KB section/article/version/acknowledgement;
- task board/column/task/comment;
- academy course/section/lesson/assignment;
- distribution group/events;
- file metadata/download URL;
- notification.

Ожидается `403` или `404` согласно контракту, но никогда `200`, `204` с изменением чужих данных или
`500`.

Проверять не только GET, но и PATCH/DELETE/move/rollback/acknowledge. Особенно важны UUID, которые
сервис хранит без FK через межсервисную границу.

После попыток повторно прочитать ресурс token A и убедиться, что он не изменился.

Не включать реальные UUID клиентов в отчёт; использовать test-company IDs или редактировать их.

## 22. Проверки ролей

Матрица минимум:

| Действие | Owner | Employee |
|---|---:|---:|
| Читать собственный профиль | Да | Да |
| Менять компанию | Да | Нет |
| Управлять оргструктурой | Да | Нет |
| Приглашать/отзывать пользователей | Да | Нет |
| Читать доступную KB-статью | Да | Да |
| Публиковать/rollback статью | По политике | По политике/нет |
| Менять структуру task board | Да | Нет |
| Выполнять назначенную задачу | Да | Да |
| Управлять курсом | Да | Нет |
| Проходить назначенный урок | Да/необязательно | Да |
| Читать чужие notifications | Нет | Нет |

Фактические ожидания сверять с доменными правилами и OpenAPI. Любой `403` должен иметь русский
ApiError и не раскрывать внутреннюю причину/данные.

## 23. CORS и browser-путь

Разрешённый preflight:

```bash
curl --silent --show-error --dump-header - --output /dev/null \
  --request OPTIONS \
  -H "Origin: $FRONTEND_ORIGIN" \
  -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: authorization,content-type' \
  "$BASE_URL/api/v1/auth/refresh"
```

Проверить exact `Access-Control-Allow-Origin`, credentials и методы/headers.

Запрещённый origin:

```bash
curl --silent --show-error --dump-header - --output /dev/null \
  --request OPTIONS \
  -H 'Origin: https://evil.example' \
  -H 'Access-Control-Request-Method: POST' \
  "$BASE_URL/api/v1/auth/refresh"
```

Ответ не должен разрешать `evil.example`. Нельзя считать curl полной заменой браузеру: выполнить
реальный UI smoke в Chromium/Firefox, проверить login, refresh после reload, logout, upload и SSE.

В browser DevTools убедиться:

- mixed content отсутствует;
- refresh-cookie имеет Secure/HttpOnly/SameSite;
- frontend отправляет credentials;
- CORS errors отсутствуют;
- presigned file URL использует HTTPS;
- access token не появляется в URL или referrer.

## 24. Контракт и форматы данных

Для каждого выбранного endpoint проверять:

- `camelCase` в JSON;
- ID — UUID string;
- date/time соответствуют схеме;
- optional и nullable не смешаны;
- неизвестные поля не появляются;
- rich-text — TipTap JSON `{type:"doc", ...}`, не HTML;
- ошибки — `{ "error": { "message": "...", "status": N } }`;
- пользовательские сообщения — на русском;
- status codes совпадают с OpenAPI;
- `204` не содержит body;
- list endpoint возвращает массив, даже если он пуст;
- download/upload используют документированный content type.

Перед релизом в CI:

```bash
make check-contract
make test
make lint
```

Не запускать сборочные инструменты на production как замену CI.

## 25. Надёжность: контролируемый рестарт

Выполнять в согласованное окно. До рестарта создать test-ресурсы и записать их IDs. Затем на
сервере:

```bash
$COMPOSE restart gateway company kb tasks academy notifications files
$COMPOSE ps
```

Дождаться readiness с ограниченным timeout:

```bash
for attempt in $(seq 1 60); do
  if curl --fail --silent --output /dev/null "$BASE_URL/readyz"; then
    break
  fi
  test "$attempt" -lt 60 || exit 1
  sleep 1
done
```

После рестарта:

- test-данные читаются;
- PostgreSQL/MinIO/NATS volumes сохранены;
- login/refresh работает согласно ожидаемой сессионной семантике;
- outbox допубликовывает событие после временной недоступности NATS;
- SSE клиент переподключается;
- restart count не продолжает расти;
- `/readyz` возвращается в SLO.

Полный `systemctl restart docker` или reboot сервера проверять отдельно и только с разрешения. Он
подтверждает restart policies; текущая production-конфигурация должна задать их всем долгоживущим
сервисам.

## 26. Degraded-mode проверки

Только staging. По одной зависимости за раз:

1. Остановить NATS — write transaction должна сохранить данные и outbox, сервис не должен терять
   событие.
2. Вернуть NATS — outbox должен опубликоваться, consumer обработать событие один раз.
3. Остановить MinIO — upload должен вернуть контролируемую ошибку, остальные домены продолжить
   работу.
4. Остановить конкретный доменный сервис — gateway readiness/его endpoint должны сигнализировать
   проблему без утечки gRPC details.
5. Кратко сделать PostgreSQL недоступным — readyz owning-сервиса становится неуспешным, после
   возврата pool восстанавливается.

После каждого эксперимента полностью восстановить green state до следующего. Не использовать
`docker compose down -v` и не удалять volumes.

## 27. Backup/restore test

Restore никогда не выполнять поверх production.

Порядок L4:

1. Создать известный набор test-данных и файл.
2. Зафиксировать IDs и checksums.
3. Дождаться backup.
4. Поднять изолированный host/network без доступа пользователей.
5. Восстановить все service databases, roles/grants и object storage.
6. Развернуть тот же commit/images.
7. Проверить migrations state.
8. Запустить read-only проверки известных данных.
9. Скачать файл и сравнить checksum.
10. Запустить ограниченный write/read test.
11. Измерить RTO и определить фактический RPO.
12. Уничтожить только изолированное restore-окружение после отчёта.

Если для восстановления нужны секреты, находящиеся только на потерянном production host, DR test
считается проваленным.

## 28. Производительность и нагрузка

В репозитории есть k6-профили:

```bash
make load-kb
make load-tasks
make load-move
```

Не запускать их против production без утверждённого traffic plan. Сначала прочитать
`tests/k6/README.md` и сами scripts: они могут требовать IDs/token и создавать нагрузку/данные.

В staging измерить:

- p50/p95/p99 latency;
- error rate;
- CPU/RAM каждого контейнера;
- PostgreSQL connections/locks/slow queries;
- NATS lag/outbox backlog;
- ordering при конкурентном move;
- SSE connection capacity;
- upload/download throughput;
- поведение при исчерпании pool и лимитов.

Критерии нагрузки должны быть заданы до запуска. «Сервис не упал» — недостаточный критерий.

## 29. Логи и observability после теста

На сервере:

```bash
$COMPOSE logs --since=30m --tail=2000 > "$HOME/teamos-postdeploy.log"
```

Файл лога считать чувствительным. Проверить:

- нет panic/fatal/OOM;
- нет циклических reconnect;
- нет постоянных 5xx;
- нет migration dirty/error;
- нет outbox publish loop;
- нет access/refresh tokens, cookie, password или JWT private key;
- request/trace IDs позволяют связать gateway и сервис;
- timestamps UTC/согласованы;
- error messages пользователю безопасны, внутренняя причина остаётся только в защищённых логах.

После анализа удалить локальную копию или переместить в защищённое хранилище согласно retention.

Проверить alerts: тестовые контролируемые сбои должны создать ожидаемые alerts и закрыться после
восстановления.

## 30. Очистка тестовых данных

Удалять только сущности, для которых контракт предоставляет DELETE, и только по сохранённым
test IDs. Рекомендуемый порядок:

1. test file;
2. test course/lesson/section;
3. distribution events/group;
4. KB article/section, если contract и зависимости позволяют;
5. task entities, если соответствующий DELETE существует.

В текущем контракте нет DELETE для company, user, task и некоторых других сущностей. Не удалять их
напрямую SQL. Test-компанию пометить в реестре тестовых данных и предусмотреть штатную lifecycle
процедуру позже.

Удалить локальные секреты:

```bash
rm -rf "$TEST_DIR"
unset OWNER_TOKEN MEMBER_TOKEN LOGIN_TOKEN REFRESHED_TOKEN E2E_PASSWORD
```

Убедиться, что shell history, CI logs и отчёт не содержат tokens/passwords/cookie.

## 31. Матрица покрытия

Агент заполняет итоговую таблицу:

| Компонент | Проверка | Результат | Evidence без секретов |
|---|---|---|---|
| DNS/TLS | hostname, chain, expiry, redirect | PASS/FAIL | дата/issuer |
| Firewall | только разрешённые порты | PASS/FAIL | список ports |
| Gateway | health/readiness/errors/CORS | PASS/FAIL | status codes |
| Auth | register/login/refresh/logout/cookie | PASS/FAIL | request IDs |
| Company | company/org/invites | PASS/FAIL | test IDs redacted |
| Schedule | week/cycle/exceptions | PASS/FAIL | status codes |
| Distribution | create/simulate/events | PASS/FAIL | counts |
| KB | section/article/version/ack/search | PASS/FAIL | counts |
| Tasks | board/column/task/move/comment | PASS/FAIL/BLOCKED | причина |
| Academy | course/lesson/quiz/assign/progress | PASS/FAIL | counts |
| Notifications | REST/read/SSE | PASS/FAIL | latency |
| Files | upload/presign/download/delete | PASS/FAIL | checksum only |
| PostgreSQL | bases/migrations/dirty | PASS/FAIL | versions |
| NATS/outbox | stream/delivery/backlog | PASS/FAIL | lag/count |
| MinIO | health/HTTPS/object lifecycle | PASS/FAIL | status/checksum |
| Tenant isolation | cross-company GET/write/delete | PASS/FAIL | status codes |
| Restart | recovery/persistence | PASS/FAIL/NOT RUN | recovery time |
| Backup restore | isolated restore | PASS/FAIL/NOT RUN | backup ID/RTO/RPO |
| Logs/alerts | no critical errors/secrets | PASS/FAIL | time window |

Допустимые статусы:

- `PASS` — проверка выполнена, критерий соблюдён;
- `FAIL` — наблюдаемое несоответствие;
- `BLOCKED` — отсутствует обязательное предусловие, указана конкретная причина и владелец;
- `NOT RUN` — проверка сознательно не запускалась из-за риска/нет разрешения.

Нельзя заменять `FAIL` на `BLOCKED` и нельзя объявлять весь deploy успешным при невыполненном P0
security или tenant-isolation тесте.

## 32. Итоговый отчёт агента

Отчёт должен содержать:

- окружение, hostname, commit SHA и время теста UTC;
- версии Docker/Compose;
- перечень выполненных уровней L0–L4;
- матрицу покрытия;
- latency асинхронных событий;
- migration versions;
- restart counts до/после;
- outbox backlog до/после;
- backup ID и результат restore drill, если запускался;
- найденные дефекты с severity и воспроизводимыми шагами;
- оставшиеся test data;
- решение: `GO`, `GO WITH KNOWN RISKS` или `NO-GO`.

В отчёте запрещены:

- access/refresh tokens;
- cookie values;
- password;
- `.env`;
- JWT keys;
- PostgreSQL/MinIO credentials;
- presigned URL целиком;
- персональные данные реальных пользователей;
- неотредактированные confidential logs.

`GO` разрешён только при успешных L0/L1, полном согласованном L2 coverage, tenant isolation,
отсутствии критических ошибок и выполненных P0 требованиях `production-security.md`.
