# Создание компании и вход из стороннего сервиса

Этот контракт позволяет доверенному стороннему сервису создать в TeamOS одну компанию,
сразу назначить владельца и администратора, а затем открывать TeamOS для этих пользователей
без передачи их паролей стороннему сервису.

## Границы ответственности

Сторонний сервис:

- авторизует своего сотрудника и получает актуальный список сотрудников;
- позволяет выбрать двух разных сотрудников: владельца и администратора;
- указывает, кто из них является инициатором текущего входа;
- вызывает только два служебных endpoint TeamOS;
- перенаправляет браузер на URL, полученный от TeamOS.

TeamOS:

- атомарно создаёт компанию, владельца, администратора и их внешние идентичности;
- хранит пароли только в `company`-сервисе как Argon2id-хэши;
- выпускает одноразовые bootstrap- и SSO-токены и хранит только их SHA-256-хэши;
- не активирует компанию, пока пароль не установили оба выбранных пользователя;
- выдаёт первой стороне ссылку активации второй стороны;
- после онбординга создаёт обычную TeamOS-сессию при каждом подтверждённом входе из стороннего сервиса.

## Служебная авторизация

Для вызовов `/api/v1/provisioning/*` используется отдельный секрет:

```http
Authorization: Service <credential>
```

Один credential привязан к одному `provider`. В текущей конфигурации это `rakurs`.
Секрет должен содержать не менее 32 символов, передаваться только по HTTPS и не попадать
в браузер или amoCRM-виджет.

Переменные gateway:

```dotenv
GATEWAY_PROVISIONING_SERVICE_TOKEN=<случайный секрет длиной 32+>
GATEWAY_PROVISIONING_SERVICE_PROVIDER=rakurs
```

Gateway обращается к `company` по отдельному внутреннему ключу. Внешний сервис этот ключ
не знает:

```dotenv
GATEWAY_COMPANY_SERVICE_TOKEN=<другой случайный секрет длиной 32+>
COMPANY_GATEWAY_SERVICE_TOKEN=<то же значение>
```

## 1. Создание компании

Перед показом сценария создания сторонний backend может проверить amoCRM Account ID:

```http
GET /api/v1/provisioning/companies/status?externalAccountId=31355990
Authorization: Service <credential>
```

Для существующей компании TeamOS вернёт:

```json
{
  "exists": true,
  "companyId": "6f366854-322d-43c8-894a-d4ce61ef4593",
  "companyStatus": "active"
}
```

Если компания ещё не создана, ответ будет `{ "exists": false }`. Провайдер берётся
из служебной учётной записи; для `rakurs` `externalAccountId` — это amoCRM Account ID.
Ответ всегда помечается `Cache-Control: private, no-store`.

```http
POST /api/v1/provisioning/companies
Authorization: Service <credential>
Idempotency-Key: create-rakurs-account-31355990
Content-Type: application/json
```

```json
{
  "provider": "rakurs",
  "externalAccountId": "31355990",
  "companyName": "ООО Ромашка",
  "initiatorExternalUserId": "101",
  "owner": {
    "externalUserId": "101",
    "email": "owner@example.com",
    "firstName": "Иван",
    "lastName": "Иванов"
  },
  "admin": {
    "externalUserId": "102",
    "email": "admin@example.com",
    "firstName": "Анна",
    "lastName": "Петрова"
  }
}
```

Требования:

- `owner` и `admin` — разные внешние пользователи с разными email;
- `initiatorExternalUserId` совпадает с `owner.externalUserId` или `admin.externalUserId`;
- внешний аккаунт уникален в рамках провайдера;
- один `Idempotency-Key` нельзя использовать с другим телом запроса.

Ответ `201 Created` для новой компании или `200 OK` для безопасного повтора:

```json
{
  "companyId": "6f366854-322d-43c8-894a-d4ce61ef4593",
  "companyStatus": "onboarding",
  "created": true,
  "initiatorRole": "owner",
  "continueUrl": "https://company.example/onboarding?token=<одноразовый-токен>",
  "expiresAt": "2026-08-08T10:00:00Z"
}
```

При повторе TeamOS не создаёт вторую компанию. Он перевыпускает ссылку инициатора и
отзывает прежнюю, поэтому сторонний сервис всегда должен использовать последний
`continueUrl` из успешного ответа.

## 2. Установка пароля инициатором

Браузер открывает `continueUrl`. Страница TeamOS должна сразу считать параметр `token` и
удалить query-строку через `history.replaceState`, чтобы токен не оставался в истории и
не передавался дальше как referrer.

Получение данных экрана:

```http
GET /api/v1/auth/bootstrap/<token>
```

Завершение активации:

```http
POST /api/v1/auth/bootstrap/<token>/complete
Content-Type: application/json
```

```json
{
  "password": "пароль-длиной-не-менее-8-символов"
}
```

TeamOS устанавливает refresh-cookie и возвращает access-токен, текущего пользователя и
состояние онбординга. После активации первого пользователя ответ содержит ссылку второго:

```json
{
  "accessToken": "<jwt>",
  "user": {},
  "onboarding": {
    "companyId": "6f366854-322d-43c8-894a-d4ce61ef4593",
    "companyStatus": "onboarding",
    "completed": false,
    "pendingUser": {
      "userId": "0e490525-90fd-4295-9c7c-52b54f281c2a",
      "email": "admin@example.com",
      "firstName": "Анна",
      "lastName": "Петрова",
      "role": "admin",
      "status": "invited"
    },
    "activationUrl": "https://company.example/onboarding?token=<новый-токен>",
    "expiresAt": "2026-08-08T10:05:00Z"
  }
}
```

Первый пользователь передаёт `activationUrl` второму. Вторая сторона открывает ссылку и
создаёт свой пароль тем же способом. После этого `companyStatus` становится `active`, а
`completed` — `true`.

Если ссылку нужно показать повторно, активированный владелец или администратор использует:

```http
GET  /api/v1/company/onboarding
POST /api/v1/company/onboarding/activation
Authorization: Bearer <TeamOS access token>
```

POST перевыпускает одноразовую ссылку и отзывает предыдущую.

## 3. Повторный вход из стороннего сервиса

После проверки сотрудника сторонний backend каждый раз запрашивает новую ссылку:

```http
POST /api/v1/provisioning/sessions
Authorization: Service <credential>
Content-Type: application/json
```

```json
{
  "provider": "rakurs",
  "externalAccountId": "31355990",
  "externalUserId": "101"
}
```

Ответ:

```json
{
  "redirectUrl": "https://company.example/sso?token=<одноразовый-токен>",
  "expiresAt": "2026-08-07T10:01:00Z"
}
```

Если пользователь ещё не установил пароль, TeamOS вернёт `/onboarding?token=...` вместо
`/sso?token=...`. Стороннему сервису не нужно ветвить логику: он открывает полученный URL.

Страница `/sso` считывает токен из query-параметра, очищает URL и обменивает токен на TeamOS-сессию:

```http
POST /api/v1/auth/sso/exchange
Content-Type: application/json
```

```json
{
  "token": "<одноразовый-токен>"
}
```

SSO-токен действует одну минуту, погашается атомарно и не может быть использован повторно.

## Два поддерживаемых порядка

Владелец начинает:

1. `initiatorExternalUserId` равен ID владельца.
2. Владелец создаёт пароль.
3. TeamOS отдаёт владельцу ссылку администратора.
4. Администратор создаёт пароль, компания активируется.

Администратор начинает:

1. `initiatorExternalUserId` равен ID администратора.
2. Администратор создаёт пароль.
3. TeamOS отдаёт администратору ссылку владельца.
4. Владелец создаёт пароль, компания активируется.

Роль владельца определяется полем `owner`, а не порядком входа.

## Основные ошибки

Ошибки возвращаются в общем формате TeamOS:

```json
{
  "error": {
    "message": "Ссылка активации уже использована",
    "status": 409,
    "code": "BOOTSTRAP_CONSUMED"
  }
}
```

Стабильные коды нового потока:

- `SERVICE_AUTH_INVALID` — неверный служебный секрет;
- `SERVICE_PROVIDER_FORBIDDEN` — credential не имеет доступа к указанному провайдеру;
- `PROVISIONING_CONFLICT` — внешний аккаунт или ключ идемпотентности конфликтует с запросом;
- `BOOTSTRAP_INVALID`, `BOOTSTRAP_EXPIRED`, `BOOTSTRAP_CONSUMED` — состояние ссылки активации;
- `SSO_INVALID`, `SSO_EXPIRED`, `SSO_CONSUMED` — состояние SSO-ссылки;
- `INTEGRATION_FROZEN` — интеграция или компания временно заморожена;
- `EXTERNAL_USER_DEACTIVATED` — внешний пользователь деактивирован;
- `ONBOARDING_COMPLETED` — компания уже завершила первоначальную настройку.

Для всех ответов с одноразовыми токенами TeamOS устанавливает `Cache-Control: private, no-store`.
