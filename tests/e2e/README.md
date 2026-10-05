# Backend E2E smoke

Сценарий из §17 работает только через публичный REST/SSE gateway и не зависит от фронтенда:
регистрация → приглашение → принятие → публикация и ознакомление со статьёй → link-курс из БЗ →
изменение статьи → обновление урока → уведомление по REST и SSE.

Требуются запущенный dev-стек, `curl` и `jq`:

```sh
tests/e2e/smoke.sh
```

Переменные: `BASE_URL` (по умолчанию `http://localhost:8080`), `E2E_PASSWORD`, `E2E_TIMEOUT` в
секундах. Каждый запуск создаёт отдельную компанию и уникальные email, поэтому подходит для CI.

## Образ MinIO для CI

В GitHub E2E job используется `deploy/docker-compose.ci.yaml`: он собирает
прежний upstream MinIO `RELEASE.2025-04-22T22-12-26Z` из исходников и задаёт
локальный image только для этого job. Основной dev/production Compose остаётся
без изменений. Причина — Docker Hub перестал отдавать уже закреплённый образ;
та же проблема воспроизводится на исходной main, до изменений распределения.

Источник: [официальный commit MinIO](https://github.com/minio/minio/commit/0d7408fc9969caf07de6a8c3a84f9fbb10a6739e),
соответствующий [release tag](https://github.com/minio/minio/releases/tag/RELEASE.2025-04-22T22-12-26Z).
Архив codeload проверяется по SHA-256
`7eb30a913fea30f18069abf194e1e78e4983b558cc526911ae1c11396a9859a5`
до компиляции. Builder Go 1.25.14 закреплён по multiarch digest и совместим с upstream Go 1.24.0;
модули фиксированы upstream go.sum (`-mod=readonly`). Release metadata и commit
встроены в binary; исходный entrypoint, server command, healthcheck и API сохранены.

Сборка изолирована отдельным шагом перед запуском всего стека. Она не
обновляет MinIO и не меняет поставщика S3. Для воспроизведения можно явно
подключить override к обычному Compose; без него используется прежняя конфигурация:

```sh
docker compose -f deploy/docker-compose.yaml -f deploy/docker-compose.ci.yaml build minio
```
