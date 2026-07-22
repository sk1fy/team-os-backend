# Академия: внешний доступ, privacy, SLO и эксплуатация

## 1. Назначение и текущая готовность

Runbook относится к персональным ссылкам, promo/candidate campaigns, внешней email-верификации,
campaign analytics и совместимому public course-by-ID. Он дополняет
[academy-migration-cutover.md](academy-migration-cutover.md),
[production-security.md](../production-security.md) и [test-services.md](../test-services.md).

В кодовой базе есть hashed access/session/code tokens, AES-256-GCM envelope для OTP-письма,
first-party visitor cookie, UTM/referrer normalization, version-pinned enrollment и campaign
reports. Следующие механизмы не автоматизированы и являются release blockers до отдельной работы:

- keyring с одновременной поддержкой старого и нового token/email key;
- автоматическая retention/purge job для campaign analytics;
- Academy-specific Prometheus metrics, dashboard и alert rules;
- production-sized load baseline для добавленного Academy k6 profile;
- единый server-side feature-flag controller;
- production traffic counter и подтверждённое окно отключения public course-by-ID.

## 2. Секреты внешнего контура

| Назначение | Academy | Notifications | Фактическое поведение |
|---|---|---|---|
| HMAC внешних access/session tokens, OTP codes и IP rate-limit hash | `ACADEMY_EXTERNAL_TOKEN_SECRET` | — | один активный secret, минимум 32 символа |
| AES-256-GCM envelope OTP delivery | `ACADEMY_EXTERNAL_EMAIL_KEY` | `NOTIFICATIONS_EXTERNAL_EMAIL_KEY` | один общий 32-byte base64 key |
| Идентификатор AES key | `ACADEMY_EXTERNAL_EMAIL_KEY_ID` | `NOTIFICATIONS_EXTERNAL_EMAIL_KEY_ID` | producer и consumer должны совпадать |

Production secrets не хранятся в `.env`, git, image layer, логах или release ticket. Их выдаёт
secret manager; backup/DR-копия должна быть отдельно зашифрована и проверена restore-тестом.
Development defaults из Compose в production запрещены.

### 2.1. Ротация `ACADEMY_EXTERNAL_TOKEN_SECRET`

Это HMAC secret, а не encryption key: в БД нет исходных tokens, поэтому существующие hashes нельзя
пересчитать. Текущая реализация не поддерживает previous secret. Смена значения немедленно делает
недействительными существующие personal/campaign URLs, external sessions и незавершённые OTP
challenges. Enrollment и progress при этом сохраняются.

Штатная zero-downtime rotation до реализации keyring запрещена. Для аварийной ротации:

1. Объявить maintenance внешнего контура и остановить создание/активацию ссылок и запросы OTP.
2. Сохранить список активных personal accesses/campaigns по ID без полных tokens и email в ticket.
3. Дождаться завершения текущих requests; зафиксировать outbox/consumer lag.
4. Сменить secret во всех Academy instances одним coordinated rollout, не оставляя mixed pool.
5. Инвалидировать незавершённые challenges и external sessions операционной командой. Прямое
   редактирование БД без отдельного reviewed procedure запрещено.
6. После переключения вызвать штатные rotate/reissue commands для активных personal accesses и
   campaigns; безопасно передать новые URLs владельцам. Старые URLs не восстанавливаются.
7. Выполнить smoke: старый token → 404/controlled error, новый token → landing, OTP, activation,
   resume; проверить отсутствие token/code/email в логах.
8. Снять maintenance и наблюдать invalid-token/verification failures.

Для неаварийной rotation сначала реализовать versioned keyring: запись только новым key ID, чтение
старым и новым, миграционное окно, затем удаление previous key после истечения sessions/challenges и
перевыпуска links.

### 2.2. Ротация OTP email AES key

Academy шифрует `recipientEmail + verificationCode`, Notifications расшифровывает envelope с
проверкой `keyId`. Оба сервиса сейчас держат только один key; rolling update с несовпадающими
значениями приведёт к недоставленным кодам.

Без keyring ротация выполняется в коротком maintenance window:

1. Остановить новые verification requests.
2. Дождаться нулевого Academy outbox lag по
   `teamos.academy.external_email_verification.requested.v1` и завершения/истечения уже принятых
   deliveries. Проверить `email_deliveries`, не извлекая message body или recipient.
3. Сгенерировать новый 32-byte random key, новый уникальный key ID и положить их в secret manager.
4. Остановить Academy и Notifications external email consumers/producers или вывести весь старый
   pool из rotation.
5. Одновременно обновить обе пары env, запустить Notifications и Academy, проверить readiness.
6. Создать один synthetic challenge, подтвердить ровно одну доставку и отсутствие plaintext в NATS,
   БД и логах.
7. Только после smoke удалить старый secret из runtime storage; сохранить его согласно DR policy.

Если старый queue нельзя полностью drain, ротацию отложить или сначала добавить multi-key
decryptor. Потеря старого AES key делает оставшиеся envelopes нерасшифровываемыми; их нужно
инвалидировать и запросить новый код, не повторять ciphertext вручную.

## 3. Campaign analytics: privacy и retention

### 3.1. Что хранится

- Gateway создаёт first-party random 256-bit HttpOnly visitor cookie сроком до 365 дней и передаёт
  только `SHA-256(cookie)`; значение помечается key ID `gateway-visitor-sha256-v1`.
- UTM fields обрезаются; referrer принимается только для HTTP(S), без userinfo, query и fragment.
- Raw IP не хранится в `analytics_events`. Для verification rate limiting Academy получает IP через
  внутренний gRPC metadata hop и сохраняет keyed HMAC в challenge, не исходный адрес.
- Tokens и OTP codes хранятся только как HMAC; полный token может быть показан только один раз в
  ответе create/rotate.
- `external_learners.email/normalized_email` и expected email персонального доступа сейчас хранятся
  в Academy DB как PII; `ACADEMY_EXTERNAL_EMAIL_KEY` шифрует только delivery envelope, а не эти
  таблицы. Защита at rest обеспечивается шифрованием диска/backup и ограничением DB access. В
  Notifications сохраняется fingerprint получателя, не email/body/code.
- Campaign event может ссылаться на ExternalLearner/enrollment. Это pseudonymous analytics, а не
  анонимные данные; доступ к reports остаётся tenant- и owner-scoped.

Нельзя добавлять email, имя, raw IP, user-agent, access token, OTP, ответы quiz или TipTap body в
analytics metadata, UTM labels, traces и логи. UTM/referrer являются недоверенным вводом и не должны
попадать в Prometheus labels.

### 3.2. Retention policy и её автоматизация

Текущая schema сохраняет events и daily funnel вместе с tombstone курса/кампании; scheduled purge
worker отсутствует. Поэтому production rollout требует утверждённого владельцем данных и юристом
retention decision. Базовая целевая политика, если локальные требования не задают более короткий
срок:

| Данные | Целевой срок | После срока |
|---|---:|---|
| request/IP hashes для rate limiting | 24 часа | удалить hash; raw IP никогда не сохранять |
| raw `analytics_events`, visitor hash, UTM/referrer | 90 дней | агрегировать, затем удалить raw event |
| `external_campaign_funnel_daily` | 24 месяца | удалить или оставить только обезличенный monthly aggregate |
| campaign/access history и enrollment result | по договорной/кадровой policy компании | anonymize/purge отдельным audited процессом |

До появления idempotent retention worker удаление вручную не является штатной операцией. Worker
должен сначала пересобрать daily aggregate, зафиксировать checkpoint, удалять bounded batches в
порядке foreign keys, публиковать только low-cardinality counters и проходить restore/retry tests.
Legal hold отключает purge только для явно перечисленных tenant/objects и обязательно audit-ится.

Ежемесячная ручная проверка до автоматизации:

```sql
SELECT min(occurred_at) AS oldest_event,
       max(occurred_at) AS newest_event,
       count(*) AS event_count
FROM analytics_events;

SELECT min(bucket_date) AS oldest_bucket,
       max(bucket_date) AS newest_bucket,
       count(*) AS bucket_count
FROM external_campaign_funnel_daily;
```

Результат содержит только totals/dates. Если oldest row вышла за policy, открыть security issue; не
выполнять ad-hoc `DELETE` до появления проверенного purge path.

## 4. SLO и alerts

Ниже initial production objectives. Release owner фиксирует окончательные значения до load test.

| SLI | Initial SLO |
|---|---|
| Доступность internal Academy и external landing/player | 99,9% за 30 дней, без ожидаемых 4xx |
| p95 HTTP/gRPC read path | ≤ 1 с за 10-минутное окно |
| p95 activation/progress mutation | ≤ 1 с без времени email provider |
| OTP: outbox event → provider accepted | p95 ≤ 60 с |
| Expiration materialization lag | ≤ 5 мин; synchronous access check остаётся обязательным |
| Campaign daily aggregate lag | ≤ 15 мин |
| Cross-tenant exposure, plaintext secret leak, immutable content mutation | 0 событий |

Существующий observability stack уже покрывает generic service down, HTTP 5xx >1%, p95 >1 с,
outbox lag, DLQ и consumer lag. Academy-specific counters/rules из product plan ещё нужно добавить.

Обязательные alerts до full rollout:

- `critical`: Academy unavailable >2 мин, cross-tenant/IDOR security signal, DLQ с Academy event;
- `warning`: external activation 5xx >1% за 5 мин, p95 >1 с за 10 мин;
- `warning`: OTP delivery failure spike или outbox-to-delivery >60 с;
- `warning`: expiration worker lag >5 мин или funnel aggregate lag >15 мин;
- `warning`: invalid/rotated token spike, verification rate-limit spike;
- `critical`: legacy write после cutover или migration divergence после достижения нуля;
- `warning`: raw analytics старше retention policy.

Labels ограничиваются service, route template, result/reason enum, owner/source type. UUID, email,
token prefix, UTM/referrer и IP hash в labels запрещены.

## 5. Security и load sign-off

Перед canary выполнить обычные проверки репозитория и integration tests на реальном PostgreSQL:

```sh
make gen
make test
make test-race
make lint
make check-contract
make check-production-compose
```

Security suite должна подтвердить 401/403/404 anti-enumeration, tenant isolation, partner report
scope, rotated/revoked token, cookie/CSRF isolation, rate limiting, отсутствие correct quiz answers,
read-only expired/frozen content и отсутствие secrets/PII в logs/events.

Репозиторий содержит безопасный read-only профиль `make academy-load` для campaign
landing и, при переданной внешней сессии, outline/progress/result reads. До production load sign-off
его дополняют staging-сценариями OTP с synthetic mailbox, idempotent activation, concurrent lesson
complete/quiz submit, campaign report и aggregation lag. Запускать только в staging с отдельной
company, заранее заданными RPS/duration/pass thresholds и cleanup plan. Минимум снять p50/p95/p99,
5xx/429, DB pool/locks, CPU/RAM, outbox/consumer/worker lag; факт «сервис не упал» не является gate.

## 6. Deprecation public course-by-ID

`GET /api/v1/public/academy/courses/{id}` остаётся compatibility read-only. Он не создаёт внешний
progress и не должен рекламироваться в новых клиентах. Новый flow начинается с secret token:
`GET /api/v1/public/academy/access/{token}` → verification → activation → enrollment player.

Порядок deprecation:

1. Переключить все известные clients и ссылки на token flow.
2. Добавить low-cardinality request counter для direct-ID route; не использовать course ID label.
3. Поддерживать operation `deprecated: true` в OpenAPI и стандартные `Deprecation`, `Sunset`,
   `Link`, `Warning` headers без изменения response body; дату проверять перед каждым релизом.
4. Выдержать объявленное окно и полный релизный цикл с нулём обращений.
5. В `/api/v1` оставить endpoint до окончания compatibility policy. Удаление route/schema, legacy
   tables/columns и experimental API — только отдельный breaking `/api/v2` release.

Rollback до завершения окна возвращает compatibility read, но никогда не включает legacy external
progress writer. После появления version >1/repeat enrollment возврат старой модели записи приведёт
к потере семантики и запрещён.
