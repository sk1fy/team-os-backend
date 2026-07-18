# TeamOS Backend

Бэкенд TeamOS на Go-микросервисах: персистентность, реальная авторизация, фоновые процессы и
уведомления для фронтенда [team-os](https://github.com/sk1fy/team-os), заменяющие его мок-API.

## Архитектура

Шесть доменных сервисов + gateway (BFF). Каждый сервис владеет своей БД и деплоится независимо;
события идут через NATS JetStream по паттерну transactional outbox.

| Сервис | Порт | Назначение |
|---|---|---|
| `gateway` | 8080 | маршрутизация, JWT, CORS, rate limiting, SSE-прокси |
| `company` | 8081 | авторизация, пользователи, оргструктура, графики, распределение сделок |
| `kb` | 8082 | база знаний |
| `tasks` | 8083 | таск-трекер |
| `academy` | 8084 | курсы |
| `notifications` | 8085 | уведомления + SSE |
| `files` | 8086 | файлы (MinIO/S3) |

Подробности: [teamos-go-microservices-plan.md](teamos-go-microservices-plan.md) (полный дизайн),
[docs/adr/](docs/adr/) (ключевые решения), [AGENTS.md](AGENTS.md) (правила работы с кодом).

## Быстрый старт

Требуются Docker (compose v2) и Go 1.25+. Для генерации кода — `buf`, `oapi-codegen`, `sqlc`.

```sh
make up       # поднять весь стек (Postgres, NATS, MinIO, сервисы); .env с dev-ключами создаётся сам
make migrate  # применить миграции
make e2e      # смоук-сценарий через gateway
make down     # остановить
```

API доступен на `http://localhost:8080`. Все команды — `make help`.

## Разработка

- REST-контракт: [contracts/openapi/teamos.yaml](contracts/openapi/teamos.yaml) — источник правды;
  хендлеры gateway генерируются из него. Эволюция контракта — только аддитивная.
- После правки контрактов или SQL-запросов: `make gen`.
- Тесты и линт: `make test`, `make test-race`, `make lint`.
- Сверка контракта с фронтендом: `make check-contract` (переменная `FRONTEND_DIR` указывает на
  репозиторий фронтенда).
- Проверка сетевых и security-инвариантов production Compose: `make check-production-compose`.
- Один сервис локально: `make dev SERVICE=company`.

## Эксплуатация

- [deploy.md](deploy.md) — пошаговое production-развёртывание.
- [production-security.md](production-security.md) — требования безопасности; production-стек
  запускается с override `deploy/docker-compose.prod.yaml`, закрывающим внешние порты.
- [test-services.md](test-services.md) — post-deploy runbook.
- `make observability-up` — Prometheus, Grafana, Loki, Tempo.
