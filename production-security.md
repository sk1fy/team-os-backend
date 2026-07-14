# Безопасность и надёжность TeamOS Backend в production

Этот документ задаёт целевое состояние безопасности и эксплуатации TeamOS Backend при размещении
всего стека на одном сервере. Это не разовая проверка: требования должны применяться при первом
развёртывании, каждом релизе и регулярном обслуживании.

Пошаговый запуск описан в [deploy.md](deploy.md).

## 1. Модель угроз и границы

Защищаем:

- персональные и корпоративные данные в PostgreSQL;
- JWT signing key и refresh-сессии;
- файлы в MinIO;
- события NATS JetStream;
- доступ к Docker daemon и Linux-хосту;
- целостность исходного кода, образов и миграций;
- доступность API и возможность восстановления после сбоя.

Основные угрозы:

- сканирование и эксплуатация открытых внутренних портов;
- кража SSH-, JWT-, PostgreSQL- или MinIO-секретов;
- компрометация контейнера и продвижение к хосту или соседним сервисам;
- SQL injection, нарушение tenant isolation и IDOR;
- злоупотребление login/registration/upload/SSE эндпоинтами;
- потеря сервера, диска, Docker volume или ошибочная миграция;
- supply-chain атака через зависимости или изменяемые Docker tags;
- утечка секретов через Git, логи, process list или диагностику;
- исчерпание диска логами, JetStream, PostgreSQL или файлами;
- ошибочная конфигурация CORS, cookie или TLS.

Один сервер остаётся единой точкой отказа. Hardening снижает вероятность инцидента, но не превращает
одиночный сервер в high availability. Для строгого SLA понадобятся внешний managed PostgreSQL или
реплика, внешнее object storage, несколько application nodes и балансировщик.

## 2. Текущие риски репозитория

В `deploy/docker-compose.yaml` сейчас:

- PostgreSQL публикуется на `0.0.0.0:5432`;
- NATS и monitoring публикуются на `0.0.0.0:4222/8222`;
- MinIO API и console публикуются на `0.0.0.0:9000/9001`;
- HTTP-порты доменных сервисов публикуются на `0.0.0.0:8081-8086`;
- gateway публикуется на `0.0.0.0:8080` без встроенного TLS;
- все логические базы используют одного PostgreSQL-пользователя;
- секреты передаются контейнерам через environment variables;
- `FILES_S3_SECURE` жёстко задан как `false`;
- application-сервисы не везде имеют явный `restart` policy;
- resource limits и log rotation не заданы;
- Docker image tags сторонних компонентов не закреплены digest;
- production reverse proxy и backup automation отсутствуют.

Пока эти пункты не устранены, стек допустим для изолированной проверки, но не для публичного
production.

## 3. Приоритеты внедрения

### P0 — обязательно до первого пользователя

- закрыть все внутренние порты;
- включить HTTPS;
- заменить все development credentials;
- установить `GATEWAY_COOKIE_SECURE=true`;
- ограничить CORS точным frontend-origin;
- обеспечить HTTPS-ссылки MinIO;
- настроить внешний зашифрованный backup PostgreSQL и MinIO;
- проверить восстановление;
- ограничить SSH и доступ к Docker;
- включить обновления безопасности ОС;
- настроить monitoring диска, памяти, health и срока TLS-сертификата.

### P1 — выполнить до стабильной эксплуатации

- отдельный PostgreSQL role для каждого сервиса;
- file-based или внешний secret manager;
- лимиты CPU/RAM/PID и rotation контейнерных логов;
- read-only filesystem и удаление Linux capabilities там, где поддерживается;
- rate limits на gateway/reverse proxy;
- audit и alerting по authentication-событиям;
- проверка образов и зависимостей на уязвимости;
- документированная ротация JWT, DB, MinIO и deploy keys;
- регулярный restore drill.

### P2 — по мере роста нагрузки и требований SLA

- managed PostgreSQL с PITR и репликой;
- внешнее S3-compatible object storage с versioning;
- отдельные application nodes;
- высокодоступный reverse proxy/load balancer;
- Vault/KMS/облачный secret manager;
- централизованный immutable audit log;
- WAF/DDoS protection;
- disaster recovery в другом регионе/провайдере.

## 4. Сетевая изоляция

### 4.1 Публичные порты

На внешнем интерфейсе разрешены только:

| Порт | Назначение | Кто может подключаться |
|---|---|---|
| `22/tcp` | SSH | доверенные IP или VPN |
| `80/tcp` | ACME/redirect | Internet |
| `443/tcp` | HTTPS | Internet |

Не должны быть публичны:

```text
5432             PostgreSQL
4222, 8222       NATS и monitoring
9000, 9001       MinIO API и console напрямую
8080-8086        gateway и HTTP сервисов без TLS
9081-9086        внутренний gRPC
3000, 3100, 3200 observability, если профиль включён
9090             Prometheus
```

MinIO API публикуется пользователям только через `storage.example.ru:443`. Console MinIO должна
быть недоступна из интернета; для администрирования использовать SSH tunnel или VPN.

### 4.2 Production Compose override

Создать `deploy/docker-compose.prod.yaml`. Для полного замещения `ports` использовать `!override`,
который требует Docker Compose 2.24.4+:

```yaml
services:
  postgres:
    ports: !override []

  nats:
    ports: !override []

  minio:
    ports: !override
      - "127.0.0.1:9000:9000"

  company:
    ports: !override []
    restart: unless-stopped

  kb:
    ports: !override []
    restart: unless-stopped

  tasks:
    ports: !override []
    restart: unless-stopped

  academy:
    ports: !override []
    restart: unless-stopped

  notifications:
    ports: !override []
    restart: unless-stopped

  files:
    ports: !override []
    restart: unless-stopped
    environment:
      FILES_S3_SECURE: "true"

  gateway:
    ports: !override
      - "127.0.0.1:8080:8080"
    restart: unless-stopped
```

Проверять нужно именно объединённую модель:

```bash
docker compose \
  --file deploy/docker-compose.yaml \
  --file deploy/docker-compose.prod.yaml \
  config
```

Без `!override` Compose может объединить списки и оставить старые публичные ports. Проверка
отрендеренного результата обязательна.

### 4.3 Docker и firewall

Docker управляет собственными iptables/nftables rules. Нельзя считать UFW единственной защитой
опубликованных Docker ports. Основная защита — не публиковать порт либо явно bind к `127.0.0.1`.

Дополнительно:

- firewall провайдера/security group разрешает только `22`, `80`, `443`;
- host firewall повторяет те же ограничения;
- IPv6 фильтруется так же, как IPv4;
- исходящие соединения ограничиваются после инвентаризации внешних зависимостей;
- admin interfaces доступны только через VPN/SSH tunnel.

После каждого релиза проверять с внешней машины:

```bash
nmap -Pn -p 22,80,443,5432,4222,8222,9000,9001,8080-8086,9081-9086 SERVER_IP
```

Ожидаемо публично открыты только согласованные `22`, `80`, `443`.

## 5. TLS, reverse proxy и HTTP-защита

### 5.1 TLS

- TLS 1.2 минимум, предпочтительно TLS 1.3;
- автоматическое получение и обновление сертификата;
- HTTP всегда перенаправляется на HTTPS;
- monitoring срока сертификата;
- сертификат и private key доступны только reverse proxy;
- включить HSTS после проверки всех поддоменов и HTTPS; начинать с небольшого `max-age`, затем
  увеличивать;
- не включать HSTS preload без отдельного осознанного решения.

### 5.2 Reverse proxy

Для API:

- сохранять исходный `Host`;
- передавать `X-Forwarded-For` и `X-Forwarded-Proto`;
- не доверять входящим `X-Forwarded-*` от интернета — proxy должен перезаписать их;
- установить request body limit в соответствии с контрактом;
- настроить timeout так, чтобы SSE оставался живым;
- отключить proxy buffering для SSE endpoint, если используется Nginx;
- не логировать `Authorization`, `Cookie`, `Set-Cookie` и тела login/refresh запросов;
- ограничить методы и размеры заголовков разумными значениями.

Для upload максимальный размер reverse proxy должен соответствовать `FILES_MAX_SIZE_BYTES` с
небольшим запасом. Сейчас значение по умолчанию — 25 МиБ (`26214400`). Нельзя разрешать
неограниченный request body.

### 5.3 Security headers

Frontend обычно отвечает за CSP, но reverse proxy может добавить базовые заголовки:

- `Strict-Transport-Security` после периода проверки;
- `X-Content-Type-Options: nosniff`;
- `Referrer-Policy: strict-origin-when-cross-origin`;
- подходящий `Permissions-Policy`;
- CSP, согласованный с реальными frontend-ресурсами.

Не добавлять `Access-Control-Allow-Origin` на reverse proxy независимо от gateway: должно быть одно
место, владеющее CORS-политикой.

## 6. CORS, cookie и JWT

### 6.1 CORS

Production-конфигурация:

```dotenv
GATEWAY_CORS_ORIGINS=https://app.example.ru
```

Требования:

- только точные доверенные origins;
- никакого `*` при credentialed requests;
- не отражать произвольный `Origin`;
- development origins не оставлять в production;
- проверять preflight и обычные запросы;
- при preview deployments использовать явный allowlist, а не wildcard домена.

### 6.2 Refresh-cookie

```dotenv
GATEWAY_COOKIE_SECURE=true
```

В коде cookie уже устанавливается с `HttpOnly`, `Secure` согласно конфигурации и `SameSite=Lax`.
Проверить браузером, что:

- cookie никогда не передаётся по HTTP;
- cookie недоступна JavaScript;
- path ограничен `/api/v1/auth`;
- logout удаляет cookie;
- refresh rotation инвалидирует предыдущую сессию;
- frontend и API размещены так, чтобы SameSite-политика соответствовала UX.

### 6.3 JWT signing key

- Ed25519 private key доступен только `company`;
- gateway и остальные сервисы получают только public key;
- private key не попадает в Docker image, Git, логи, backup без шифрования;
- access token остаётся короткоживущим, сейчас 15 минут;
- ротация signing key должна поддерживать переходный период или принудительный перелогин;
- при компрометации private key отозвать refresh-сессии и выпустить новую пару;
- issuer и audience должны совпадать во всех сервисах и быть стабильными.

Для мягкой ротации одного публичного ключа в будущем потребуется поддержка key ID (`kid`) и набора
публичных ключей/JWKS. Пока её нет, ротация потребует согласованного рестарта и может инвалидировать
активные access tokens.

## 7. Управление секретами

### 7.1 Минимально допустимо

Если пока используется `.env`:

- файл не хранится в Git;
- права `0600`;
- владелец — deployment user;
- секреты создаются отдельно для каждого окружения;
- production secrets не копируются в staging/dev;
- `.env` не выводится в agent logs и CI artifacts;
- backup `.env` зашифрован отдельным ключом;
- shell history не содержит секретов;
- доступ к `docker inspect` считается привилегированным, потому что environment виден там.

### 7.2 Целевое состояние

Перейти на один из вариантов:

- Vault;
- cloud secret manager + KMS;
- SOPS с age/KMS и расшифровкой только на сервере;
- file-based secrets, смонтированные read-only;
- Docker secrets, если deployment переведён на Swarm.

Приложения сейчас читают значения из environment variables. Для настоящих file-based secrets нужно
добавить поддержку переменных вида `*_FILE` или загрузку secret-файла в конфигурационном слое.
Нельзя считать обычный Compose `secrets:` эквивалентом шифрованного secret manager без анализа
того, где хранится исходный файл.

### 7.3 Ротация

Документировать владельца, период и процедуру для:

- SSH deploy key;
- PostgreSQL roles;
- MinIO access key;
- Ed25519 JWT key;
- backup encryption key;
- credentials внешнего object storage;
- TLS account/ключей, если ими не управляет ACME автоматически.

Ротацию сначала репетировать в staging. После ротации проверять login, refresh, gRPC связи, upload,
download и backup.

## 8. PostgreSQL

### 8.1 Изоляция сервисов

Сейчас базы разделены логически, но используется общий `POSTGRES_USER`. Целевое состояние:

| Сервис | База | Role |
|---|---|---|
| company | `teamos_company` | `teamos_company` |
| kb | `teamos_kb` | `teamos_kb` |
| tasks | `teamos_tasks` | `teamos_tasks` |
| academy | `teamos_academy` | `teamos_academy` |
| notifications | `teamos_notifications` | `teamos_notifications` |
| files | `teamos_files` | `teamos_files` |

Каждая role получает права только на свою базу и схему. У сервисных roles не должно быть
`SUPERUSER`, `CREATEDB`, `CREATEROLE` или доступа к чужим базам. Миграционный пользователь может
иметь расширенные права, но runtime-пользователь — только DML и необходимые sequence privileges.

Не создавать FK и JOIN между базами сервисов. Межсервисные UUID остаются без FK.

### 8.2 Доступ

- PostgreSQL не публикуется в интернет;
- локальное администрирование — через `docker compose exec`, Unix access или SSH tunnel;
- удалённое администрирование — только через VPN/bastion и TLS;
- `pg_hba.conf` разрешает только необходимые сети и roles;
- логировать неуспешные подключения без логирования паролей;
- установить connection limits и следить за pool saturation.

### 8.3 Настройка и обслуживание

- включить мониторинг размера баз, connections, locks, dead tuples и slow queries;
- настроить memory параметры под фактическую RAM, а не копировать случайный `postgresql.conf`;
- контролировать autovacuum;
- установить statement timeout для пользовательских запросов после нагрузочного теста;
- проверять миграции на блокировки и длительность;
- обновлять PostgreSQL minor release планово;
- major upgrade сначала репетировать на копии production backup.

## 9. MinIO и файлы

- MinIO API доступен извне только через HTTPS-домен;
- console закрыта от интернета;
- заменить root credentials;
- сервису `files` в перспективе выдать отдельного пользователя с доступом только к нужному bucket,
  не использовать root credentials;
- `FILES_S3_SECURE=true`;
- `FILES_S3_PUBLIC_ENDPOINT` указывает на публичный HTTPS host без `http://`;
- максимальный размер файла ограничен в gateway/reverse proxy и сервисе;
- имена, MIME type и расширения не считаются доверенными;
- скачивание происходит через короткоживущие presigned URL;
- включить versioning/retention в объектном хранилище, если оно поддерживается;
- backup bucket хранится вне сервера;
- рассмотреть malware scanning для загружаемых пользователями файлов.

MinIO root password из `.env.example` запрещён в production.

## 10. NATS JetStream

- порты `4222` и `8222` не публикуются;
- monitoring endpoint доступен только внутренней сети/monitoring agent;
- при выходе за пределы одного Docker host включить NATS authentication и TLS;
- выдавать сервисам subject-level permissions;
- следить за размером stream, consumer lag, redeliveries и failed publishes;
- настроить лимиты хранения исходя из диска и RPO;
- тестировать восстановление состояния вместе с PostgreSQL, учитывая transactional outbox и
  идемпотентность consumers.

Текущий JetStream использует один узел и `replicas=1`, поэтому не выдерживает потерю сервера.

## 11. Docker host hardening

### 11.1 Доступ к daemon

- не публиковать Docker API на TCP `2375`;
- не монтировать `/var/run/docker.sock` в application-контейнеры;
- доступ к группе `docker` давать как root-equivalent privilege;
- для удалённого управления использовать SSH-based Docker context или защищённый CI runner;
- не запускать недоверенные образы;
- Docker host по возможности не использовать для посторонних workloads.

### 11.2 Контейнеры

Для каждого сервиса проверить возможность:

```yaml
read_only: true
security_opt:
  - no-new-privileges:true
cap_drop:
  - ALL
tmpfs:
  - /tmp:size=64m,mode=1777
pids_limit: 256
```

Не добавлять эти настройки вслепую: сначала протестировать каждый образ. PostgreSQL, NATS и MinIO
нужны writable volumes; application-сервисам может понадобиться только `/tmp`.

Проверить Dockerfile каждого сервиса:

- процесс работает не от root;
- multi-stage build не переносит build tools в runtime;
- в image нет `.env`, Git metadata и ключей;
- base image минимален и регулярно обновляется;
- binary и CA certificates — только необходимые runtime artifacts.

### 11.3 Ресурсы и логи

Задать лимиты после измерения профиля нагрузки:

- memory limit/reservation;
- CPU limit;
- PID limit;
- restart policy;
- healthcheck;
- graceful shutdown timeout;
- log rotation.

Минимальная rotation для Docker json logs:

```yaml
x-logging: &default-logging
  driver: json-file
  options:
    max-size: "20m"
    max-file: "5"
```

Применить anchor к долгоживущим сервисам. Централизованная отправка логов не отменяет локальный
лимит.

## 12. Linux host hardening

- только поддерживаемая LTS-система;
- автоматическая установка security updates или регулярное согласованное patch window;
- минимальный набор пакетов;
- SSH только по ключам;
- `PermitRootLogin no` после проверки административного пользователя;
- `PasswordAuthentication no` после проверки key-based login;
- ограничение SSH по IP/VPN;
- MFA на уровне VPN/bastion/provider console;
- fail2ban может быть дополнительным слоем, но не заменяет allowlist;
- AppArmor/SELinux не отключать без причины;
- синхронизация времени обязательна для JWT и TLS;
- encrypted disk/volume использовать, если это поддерживает провайдер;
- provider console и API защищены MFA и отдельными учётными записями;
- мониторить неожиданные login, sudo и изменения firewall.

Перед изменением SSH всегда держать вторую открытую сессию и проверить новый вход, чтобы не
заблокировать доступ к серверу.

## 13. Supply-chain и релизы

- protected main branch;
- обязательный review;
- CI выполняет `make test`, `make lint`, `make check-contract`;
- dependency и image vulnerability scanning;
- secret scanning;
- release строится из чистого commit SHA;
- production разворачивает tag/SHA, а не незафиксированную ветку;
- сторонние image versions закрепляются хотя бы точным version tag, для строгой воспроизводимости —
  digest;
- обновления образов происходят контролируемо, а не через бесконтрольный `latest`;
- генерируемые файлы создаются в CI и проверяются на соответствие контрактам;
- артефакты или images подписываются, если вводится registry-based deployment;
- SBOM сохраняется вместе с релизом.

Перед `docker compose build --pull` понимать, какие base images могут измениться. Для полностью
воспроизводимого релиза собирать image в CI, сканировать и разворачивать по digest.

## 14. Application security

### 14.1 Авторизация и multi-tenancy

Для каждого storage query и application method проверять:

- `companyId` берётся из доверенных JWT claims/context, а не только из body/path;
- запрос ограничен `company_id`;
- UUID существующей сущности другой компании не раскрывает данные;
- межсервисный вызов передаёт tenant context;
- role/position/department permissions проверяются сервером;
- ошибки не раскрывают факт существования чужой сущности, когда это важно.

Добавлять negative integration tests: пользователь компании A не может читать или изменять данные
компании B.

### 14.2 Ввод и rich text

- валидировать размер и структуру всех входов;
- rich-text принимается только как TipTap JSON;
- не принимать или не рендерить HTML без отдельной sanitization;
- ограничивать глубину и размер JSON;
- UUID валидировать до storage layer;
- ошибки пользователю остаются на русском и не содержат stack trace, SQL или секретов.

### 14.3 Rate limiting и abuse protection

Особо ограничить:

- login;
- register;
- password reset/invite acceptance;
- token refresh;
- upload;
- дорогие search/export операции;
- создание SSE connections.

Лимиты должны учитывать доверенную цепочку proxy и реальный client IP. Нельзя доверять
произвольному `X-Forwarded-For`. Для распределённого deployment in-memory rate limiter потребуется
заменить общим хранилищем или edge limiter.

### 14.4 Логи и приватность

Никогда не логировать:

- пароли;
- access/refresh tokens;
- `Authorization`;
- `Cookie` и `Set-Cookie`;
- Ed25519 private key;
- PostgreSQL/MinIO credentials;
- полные тела authentication requests;
- содержимое пользовательских файлов.

Структурированные логи должны содержать request/trace ID, service, route, status, duration и
безопасный tenant/user identifier. Определить retention и доступ к логам как к персональным данным.

## 15. Backup

### 15.1 Цели

До запуска определить:

- RPO — допустимая потеря данных по времени;
- RTO — допустимое время восстановления;
- retention, например daily/weekly/monthly;
- владельца процесса восстановления;
- отдельное географическое/провайдерское место хранения.

Для небольшого production разумный начальный ориентир: ежедневный полный backup плюс более частые
инкрементальные/PITR-механизмы, если RPO меньше суток. Конкретная политика определяется бизнесом.

### 15.2 Что сохранять

- все шесть PostgreSQL databases;
- PostgreSQL roles и grants;
- MinIO bucket и metadata/versioning;
- NATS JetStream, если события нельзя безопасно восстановить из outbox;
- зашифрованную production-конфигурацию;
- reverse proxy config;
- commit SHA, image digests и migration versions;
- инструкции и ключи восстановления, хранящиеся отдельно.

### 15.3 Правило 3-2-1

- минимум три копии данных;
- минимум на двух типах/системах хранения;
- минимум одна копия вне production-сервера;
- желательно одна immutable/object-lock копия.

Backup на том же диске или только snapshot того же VPS не является достаточным disaster recovery.

### 15.4 Безопасность backup

- шифрование до отправки или server-side encryption с отдельным KMS key;
- credentials backup-процесса имеют write-only/минимальные права, где возможно;
- checksum и размер проверяются;
- lifecycle/retention защищают от бесконечного роста;
- удаление backup требует отдельного privileged identity;
- alert, если очередной backup не создан или слишком мал;
- секрет расшифровки не хранится только на том же сервере.

### 15.5 Проверка восстановления

Backup считается существующим только после успешного restore test. Не реже одного раза в квартал,
а после изменения схемы backup — сразу:

1. Поднять изолированное окружение.
2. Восстановить PostgreSQL и bucket.
3. Запустить ту же версию приложения.
4. Выполнить integrity и smoke checks.
5. Измерить фактический RTO.
6. Зафиксировать результат и исправить runbook.

Никогда не проверять restore поверх production databases.

## 16. Надёжность и отказоустойчивость

### 16.1 Один сервер

На одном сервере обязательны:

- provider snapshot как дополнительный, но не единственный backup;
- внешний backup;
- disk space alerts;
- автоматический запуск Docker после reboot;
- restart policies для всех долгоживущих контейнеров;
- health/readiness monitoring извне;
- проверенный server rebuild runbook;
- DNS TTL, позволяющий аварийное переключение;
- запас свободного диска минимум 20–30%.

### 16.2 Что выносить первым

При росте или повышении требований SLA:

1. PostgreSQL — managed service с automated backups/PITR.
2. MinIO — managed S3-compatible storage.
3. Reverse proxy — внешний load balancer/CDN.
4. Stateless application-сервисы — два и более nodes.
5. NATS — кластер из трёх узлов, если события критичны.

Вынос сервиса выполняется изменением его `*_DB_URL`, gRPC/service discovery адресов и переносом
данных. Границы отдельных баз уже облегчают этот процесс.

### 16.3 graceful shutdown

Compose задаёт `stop_grace_period` для сервисов. При обновлении:

- сначала перестать направлять новые запросы;
- дождаться завершения текущих запросов/outbox операций;
- отправить SIGTERM;
- не применять SIGKILL до истечения grace period;
- проверить, что SSE-клиенты переподключаются.

Обычный single-host Compose не обеспечивает zero-downtime rolling update. Если это требование
появится, нужен второй application node или оркестратор.

## 17. Monitoring и alerting

Мониторить с внешней точки:

- `https://api.example.ru/healthz`;
- `https://api.example.ru/readyz`;
- TLS expiry;
- latency и 5xx rate;
- доступность upload/download;
- SSE connection/reconnect rate.

Мониторить внутри:

- CPU, RAM, load average;
- disk usage и inode usage;
- container restart count/OOM kills;
- PostgreSQL connections, locks, replication/PITR status, DB size;
- NATS stream size, consumer lag и publish errors;
- MinIO capacity и request errors;
- outbox backlog;
- authentication failure spikes;
- backup age и restore-test age.

Начальные alerts:

- диск > 70% warning, > 85% critical;
- inode > 80%;
- container restarting;
- `/readyz` неуспешен несколько проверок подряд;
- TLS истекает менее чем через 21 день;
- backup старше ожидаемого интервала;
- резкий рост 401/403/429/5xx;
- NATS consumer lag или outbox backlog растёт;
- OOM kill или swap thrashing.

Observability UI (Grafana, Prometheus и др.) не публиковать напрямую. Доступ — VPN, SSO-protected
proxy или SSH tunnel.

## 18. Обновления и миграции

- применяются только проверенные release commits;
- перед релизом backup и проверка restore path;
- миграции следуют expand → migrate → contract;
- destructive migration не совмещается с application rollout;
- не редактировать применённую миграцию задним числом;
- оценивать lock и table rewrite на production объёме;
- `migrate down` не является автоматическим rollback;
- после релиза проверять readiness, error rate, latency, outbox и DB locks;
- иметь previous image/commit, но учитывать совместимость его кода с новой схемой.

Для больших таблиц миграция должна быть resumable или выполняться отдельным backfill job с
наблюдаемым прогрессом.

## 19. Реагирование на инциденты

Минимальный incident runbook:

1. Зафиксировать время, симптомы и затронутые компоненты.
2. Не уничтожать контейнеры, volumes и логи до сбора фактов.
3. Ограничить доступ или вывести скомпрометированный узел из сети.
4. Сохранить audit evidence с контролем доступа.
5. Ротировать затронутые секреты в правильном порядке.
6. При компрометации JWT private key отозвать refresh-сессии и выпустить новую пару.
7. Проверить целостность Git commit/images/configuration.
8. Восстановить из доверенного источника, а не «починить» неизвестно изменённый хост.
9. Уведомить ответственных и выполнить юридические требования по утечкам данных.
10. Провести postmortem и добавить предотвращающие проверки.

Не публиковать чувствительные логи и дампы в обычных чатах или issue tracker.

## 20. Регулярный график

### Ежедневно автоматически

- health/readiness checks;
- backup и проверка его свежести;
- disk/resource monitoring;
- certificate monitoring;
- alert на container restarts и 5xx.

### Еженедельно

- review security updates;
- review auth anomalies;
- проверка роста PostgreSQL/NATS/MinIO;
- проверка неуспешных backup jobs;
- vulnerability scan актуальных images.

### Ежемесячно

- patch window ОС и Docker;
- проверка firewall и публичных портов;
- review пользователей с SSH/Docker/provider access;
- проверка сроков секретов и ключей;
- тест восстановления выбранной базы/файла.

### Ежеквартально

- полный disaster recovery exercise;
- review threat model;
- ротация секретов согласно политике;
- проверка tenant-isolation тестов;
- пересмотр capacity и SLA.

## 21. Production readiness checklist

Перед открытием доступа пользователям все P0-пункты должны быть отмечены:

- [ ] Развёрнут конкретный release tag/commit SHA.
- [ ] `.env` не находится в Git и имеет права `0600`.
- [ ] Development passwords полностью заменены.
- [ ] JWT private key уникален для production.
- [ ] Публичны только `22`, `80`, `443`.
- [ ] PostgreSQL, NATS, MinIO console и сервисные порты недоступны извне.
- [ ] Gateway доступен только через HTTPS reverse proxy.
- [ ] `GATEWAY_COOKIE_SECURE=true`.
- [ ] CORS содержит только доверенные production origins.
- [ ] `FILES_S3_SECURE=true` реально присутствует внутри контейнера.
- [ ] Upload/download работает через HTTPS.
- [ ] TLS автоматически обновляется и контролируется alert.
- [ ] SSH ограничен ключами и доверенной сетью/VPN.
- [ ] Docker API не опубликован.
- [ ] Application images не запускаются privileged.
- [ ] Настроена rotation логов.
- [ ] Настроены resource/disk alerts.
- [ ] Backup PostgreSQL и MinIO уходит вне сервера.
- [ ] Выполнен успешный restore test.
- [ ] Документированы RPO/RTO и владелец восстановления.
- [ ] Проверены `/healthz` и `/readyz` снаружи.
- [ ] Проверены login, refresh, logout, tenant isolation и файлы.
- [ ] Подготовлен и проверен rollback/forward-fix plan.

## 22. Ссылки на первичные руководства

- [Установка Docker Engine на Ubuntu](https://docs.docker.com/engine/install/ubuntu/)
- [Установка Docker Compose plugin](https://docs.docker.com/compose/install/linux/)
- [Объединение Compose-файлов и `!override`](https://docs.docker.com/reference/compose-file/merge/)
- [Docker networking и firewall](https://docs.docker.com/engine/network/packet-filtering-firewalls/)
- [Безопасность Docker Engine](https://docs.docker.com/engine/security/)
- [Защита доступа к Docker daemon](https://docs.docker.com/engine/security/protect-access/)
- [Production-рекомендации Docker Compose](https://docs.docker.com/compose/how-tos/production/)
- [Automatic HTTPS в Caddy](https://caddyserver.com/docs/automatic-https)
