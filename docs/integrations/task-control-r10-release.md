# КТ-01.R10: локальный release candidate и эксплуатация

16.10.2026. По указанию пользователя выполняются только локальные проверки.
R09 live/browser acceptance отложена. **Пилот/production не выпускались**, новый UI
TeamOS/админки в поставку не входит. Подготовленный candidate не означает одобрение rollout.

Основная операторская инструкция — Core `docs/runbooks/task-control-mvp.md`:
Core → company/gateway → runtime 1.2.3, schema 30/40, env и key rotation,
backup/restore, схема v1/v2, import/standalone, диагностика и stop conditions.
Актуальные API/DTO/examples/permissions/pagination и полная матрица —
[R09](task-control-r09-acceptance.md), [R07](task-control-r07-diagnostics.md),
[R08](task-control-r08-semantics.md), [R11](task-control-r11-dynamic.md).

## Candidate

Из корня backend, после коммитов во всех трёх checkout:

```sh
python3 scripts/task-control-release.py --output /tmp/task-control-rc --build-images
```

Соседние пути задаются --core/--widgets. Для реального кандидата отдельно передать
--core-origin/--backend-origin и --oauth-client-uuid. Это публичная конфигурация,
не секреты. Скрипт ничего не устанавливает, не публикует образы и не меняет OAuth.
По умолчанию origins example.invalid и UUID неизвестен: такой managed ZIP запрещено
использовать для живой проверки. Стандартный standalone архив не содержит origins.

Candidate фиксирует точные commit SHA, 8 local image IDs и OCI revision/architecture,
RepoDigests из локального daemon при наличии, schema 30/40, runtime 1.2.3 и SHA-256 двух ZIP. Каждый ZIP
собирается дважды с одним SOURCE_DATE_EPOCH и сравнивается побитово. Для повторения
на другой машине нужны те же исходники/config, Python и zlib; toolchain записан.
Наличие RepoDigests в daemon не подтверждает push; registryVerified=false.
Локальный Docker image ID не является registry manifest digest. После push отдельно
зафиксировать repo@sha256 для нужной архитектуры и проверки CI финальных SHA.
`releaseApproved=false` сохраняется явно; блокирующие release gates не скрываются.
После сборки создаются core.compose.json и backend.compose.json: добавить их
**последними** к существующим overlays и использовать up --no-build. Они закрепляют
точный локальный image ID, включая tools; build не выполняется
повторно под плавающим тегом. Локальные ID работают только на этом Docker engine.
Для сервера нужны опубликованные и повторно проверенные registry digests.

## Реальные локальные drills

Core:

```sh
python3 scripts/task-control-backup-drill.py --output /tmp/r10-core-drill
python3 scripts/task-control-proxy-smoke.py
```

Первый запускает свой временный PostgreSQL 17; миграции 1–28, установка/grant и
подтверждённый 1.2.1 → миграции 29–30 → штатный descriptor CLI 1.2.3.
Проверяет сохранение generation, отказ down30 с evidence 1.2.3, настоящий pg_dump -Fc,
pg_restore в другую БД, hashes **всех 47 таблиц** и неизменность после повторного
migrate up. Dump синтетический, доступ 0600, каталог 0700; контейнер удаляется.
Ни внешний DB URL, ни production credentials скрипт не принимает.

Второй запускает изолированный nginx 1.28 с настоящим staging include и stub upstreams.
Проверены синтаксис и **16 routes**: v2/bootstrap, v1/v2 ACK, telemetry, connection,
sessions/source bridge, auth/disconnect; лишние paths, private/admin снаружи отвергаются.
Проверка включена в Core CI. CORS, настоящие auth/JWT, TLS и серверный nginx этим
smoke не подтверждаются. Раньше staging include содержал только MVP routes.

Company, из services/company (для Colima дополнительно задать DOCKER_HOST и
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE; Docker CLI должен обращаться к тому же engine):

```sh
TASK_CONTROL_RELEASE_DRILL=1 GOWORK=off go test -race -tags integration \
  -count=1 -v -timeout=4m -run TestTaskControlDynamicModeConfirmationEpochAndRollback \
  ./internal/application
```

Прогон включает настоящий PostgreSQL 16, миграции 1–40, динамическую публикацию,
переход managed→standalone с полным экспортом/confirmDynamicLoss, stale source fence,
отзыв прежней runtime session и явный rollback intent. Дополнительный release subtest
находит **только** disposable testcontainers PostgreSQL по label и mapped port,
делает pg_dump/pg_restore в другую БД, сверяет все **23 task-control таблицы**,
создаёт application с новым pool как после рестарта и выполняет API rollback.
Новая публикация сохраняет checksum intent; draft/revision и исходная история не теряются.
Received/applied evidence также сохранено; down40 с этими событиями проверенно отказал.
Временная БД удаляется вместе с test container. Без TASK_CONTROL_RELEASE_DRILL=1
обычный тест не запускает этот дополнительный dump/restore; CI не выдаётся за этот drill.

Это локальный application/migration rollout и restart, **не Docker-развёртывание
полного стека и не откат работающих production бинарников**. Внешний Core/CRM в
application fixtures синтетический. Backup test доказывает восстановление точки dump,
а не сохранение изменений после backup. Production PITR/WAL требует отдельной проверки.

## Ограничения rollback

- Основной безопасный путь — API rollback правил: новая immutable publication на
  текущих источниках, история и draft сохраняются. Недоступные explicit references
  запрещают восстановление; их нельзя обходить редактированием БД.
- V2 mode-intents возвращает server completed + full export receipt. Это не доказывает
  сохранение amoCRM: существующий widget.taskControlMigration.prepareStandalone
  проверяет receipt/current standalone, сохраняет backup старых настроек, создаёт
  отдельный каталог, перечитывает checksum и готовит runtime_config для основной Save.
  Одного выключения checkbox или actor-проекции недостаточно.
- Старый v1 UI остаётся совместимым при binding schema=1. Активация v2 явная;
  после неё old-editor write получает requires_v2. Обязательного UI-релиза нет.
- Откат бинарников только к отдельно проверенной паре image digest, понимающей
  schema 30/40 и выбранный runtime protocol. В этом этапе такой cross-version binary
  rollout не проверялся. Старый v1 runtime при активном v2 binding не подходит.
- Аддитивные DB migrations остаются. Не выполнять destructive down, не уменьшать
  generation/epoch/revision, не релабелить runtime descriptor. Core down30 и company
  down40 имеют guards retained evidence. Не обходить их ради старого образа.

## Что остаётся до живого выпуска

R09 live + поддерживаемые браузеры; согласованный тестовый аккаунт/карточки;
проверенные origins/OAuth UUID, установка конкретного ZIP; registry digests и CI
финальных SHA; совместимость текущего MVP UI на выбранной схеме; rollout и binary
rollback на разрешённом стенде, защищённый backup/restore реальных данных с актуальными
ключами и reconciliation OAuth. Эти пункты отделены от выполненных локальных drills.

## Передача после серверного аудита 19.10.2026

Read-only аудит обнаружил прежний R06 на тестовом сервере: Core `759c4e3`
(schema 25), backend `39cf57a` (company schema 33). Отдельный Task Control
`task-control-widget` включён в managed; `amocrm-pro-service` обслуживает
Distribution. Bootstrap 200 подтверждает доступность старой цепочки, а не
версию 1.2.3 и не завершение R09. Упоминание 501 в аудите — чтение старого кода,
не полученный HTTP-ответ. В текущем main эти обработчики уже реализованы.

После согласованного merge серверный агент обновляет три checkout **с main**,
фиксирует новые полные SHA и CI, создаёт новый candidate по инструкции выше.
SHA предыдущего RC не переносить на новую сборку. Для company/gateway обычный
Compose и CI теперь передают `BUILD_REVISION`: OCI revision и `--version` бинарника
должны совпадать. Без аргумента остаётся явное `unknown`; это не evidence.
Запуск `company --version` / `gateway --version` не требует env, БД или токенов.

До запуска сборки через обычный Compose:

```sh
export BUILD_REVISION="$(git rev-parse HEAD)"
docker compose -f deploy/docker-compose.yaml build company gateway
```

При candidate-сборке аргумент передаёт `task-control-release.py`. Не запускать
повторный build поверх закреплённых image IDs. После загрузки **тех же** образов
и согласованного обновления можно выполнить read-only сравнение артефактов:

```sh
python3 scripts/task-control-deployment-audit.py \
  --candidate /secure/task-control-rc/candidate.json \
  --container core/api=amocrm-pro-api-1 \
  --container core/worker=amocrm-pro-worker-1 \
  --container backend/company=teamos-company-1 \
  --container backend/gateway=teamos-gateway-1 \
  --widget-zip /secure/task-control-rc/task-control-1.2.3-managed.zip \
  --output /secure/task-control-rc/deployment-audit.json
```

Имена контейнеров сверить с целевым Compose. Команда читает только image ID,
OCI revision, architecture, running и ZIP; не выгружает Env, не создаёт sessions
и не меняет сервер. Возвращает ненулевой код при старом/неизвестном SHA, другом
image ID/архитектуре, остановленном контейнере, ином ZIP/версии, example.invalid
или неизвестном OAuth UUID. Для registry-поставки нужно сохранить исходный image
ID и отдельно подтвердить repo digest; пересборка с тем же SHA не эквивалентна
кандидату. Файл ZIP на диске не доказывает установку в amoCRM.

`artifactMatch=true` **не закрывает** schema, installation/generation, webhook,
freshness, ACK, R09 live/browser matrix и R10 rollout/restore/binary rollback.
Поля `acceptancePassed=false`, `releaseApproved=false` сохранены намеренно.
`webhook_status=error` требует штатной диагностики/reconcile; expired OAuth state
требует новой штатной OAuth-попытки, а не ослабления state-проверки. Истёкший
runtime snapshot не становится свежим от обновления бинарника: проверить новый
bootstrap и application evidence. R09/R10 остаются in progress до реальных
проверок, перечисленных выше; эти PR сами по себе не являются приёмкой.


## Восстановление поставки после проверки20.10.2026

Повторная сборка одного SHA не обязана дать тот же image ID. Раньше tag содержал
только SHA и новая сборка могла перезаписать ссылку на старый работающий образ.
Теперь candidate имеет уникальный `candidateId`, tags включают SHA и candidateId;
каждый из восьми образов сразу сохраняется `docker image save` **по image ID** в
`core-*.tar` / `backend-*.tar` с SHA-256 в `images.*.archive`. Под этот вывод надо
заранее выделить место под восемь архивов. Версия ZIP читается из manifest виджета,
поэтому следующий пакет1.2.4 не будет ошибочно назван1.2.3.

Это не восстанавливает уже утраченные image IDs: manifest нельзя вручную
переписать под старые контейнеры. После merge нужен новый отдельный candidate,
rollout его точных IDs и сравнение с работающими контейнерами. Для восстановления
проверить SHA-256 tar, выполнить `docker image load --input ...`, затем убедиться,
что `docker image inspect <recorded-ID>` возвращает исходный ID/revision/arch.
Нельзя повторно build поверх закреплённого кандидата. Registry digest остаётся
отдельным gate: локальный tar/ID не является подтверждением публикации registry.

Deployment audit теперь проверяет также JSON origins внутри `lib/deployment.js`
ZIP, а не только поля candidate и SHA архива. Код JS из ZIP не исполняется.
CLI test с временными Git checkout и fake Docker проверяет две сборки того же
SHA: tags не пересекаются, старые tags сохраняются, восемь архивов соответствуют
зафиксированным ID/checksum, Compose overrides используют эти IDs. Это тест
оркестратора, не реальная сборка/restore Docker-образов.

Порядок серверной приёмки: [готовый промпт](../runbooks/task-control-r09-r10-server-prompt.md).
Установка нового ZIP1.2.4 и server rollout после merge обязательны; локальные тесты
и browser smoke1.2.3 не закрывают эти gates.


Для1.2.4 Core должен применить новую миграцию31 до подтверждения descriptor;
Core CLI/status/admin-read и backend allowlists поддерживают1.2.4. Целевой candidate
теперь указывает Core31/company40. Down31 блокируется при descriptor1.2.4.
Прошлый health/drill на30/40 не является приёмкой новой сборки. После merge
сначала Core migration/API/worker/tools и backend, затем ZIP и только после
проверки загруженных файлов — descriptor1.2.4 на текущей generation.
