# Промпт серверному агенту: завершение R09/R10 после merge

Ниже текст для pi на тестовом сервере. Он самодостаточен: ClickUp агенту не нужен.
Запускать после review/merge трёх PR этой проверки. Не развёртывать незамёрженные
ветки. Если обновлённый main не содержит перечисленных исправлений, остановить
мутации и сообщить несовпадение. Не утверждать принятие до получения evidence.

---

Заверши тестовую приёмку Task Control R09/R10 на Devcorps2.26.66.187.
Разрешён только тестовый аккаунт amoCRM31391206 amotehrkrs, интеграция
`task-control-widget`, installation `5a2c8731-ca70-4aeb-95d2-137509749f5c`.
Distribution/amocrm-pro-service — отдельная интеграция; её настройки не менять.
Действуй через штатные CLI/API, не делай ручных SQL-правок бизнес-состояния.
Секреты, JWT, webhook keys, полный env и payload с ПДн не выводить в отчёты.

Контекст и прежние артефакты:
- /root/reports/task-control-r09-r10-server-20261019.md
- /root/reports/task-control-r09-browser-smoke-20261020.md
- /root/reports/task-control-r09-r10-readonly-prep-20261020.md
- /root/task-control-rc-20261019-public443/
- /root/backups/task-control-r10-20261019T183246Z/
- /root/task-control-drills/

1. Сначала preflight и backup. Зафиксируй актуальные dirty state/SHA трёх checkout,
получи main через fast-forward без reset/clean, запиши полные SHA и CI. Core main
должен содержать empty-events reconcile, widgets —1.2.4 с повторными напоминаниями,
backend — уникальные candidate tags и image tar archives. Прочитай актуальные
runbooks/Core lifecycle и backend R09/R10 contracts перед командами. Сними
защищённые backup БД Core/company и конфигов, проверь восстановление в отдельные
БД с текущими ключами. Зафиксируй installation/generation/binding/epoch,
publication/draft/runtime revision, исходные правила/пользовательские настройки.
Никаких down миграций, ротации ключей, новой OAuth-интеграции или reinstall.

2. Создай новый candidate в новом каталоге через backend
`scripts/task-control-release.py --build-images`, указав Core/widgets checkout,
обе origins `https://2-26-66-187.sslip.io`, действительный OAuth UUID именно
Task Control. Версию ZIP брать из manifest (ожидается1.2.4), не переименовывать
старый1.2.3. Убедись в достаточном свободном месте под восемь image archives.
Сохрани candidate.json, ZIP/tar checksums, revision/arch всех образов. Ни один
старый tag не переиспользовать; никаких rebuild после фиксации manifest.

3. Исправь artifact mismatch реальным rollout. Сейчас running IDs прежнего
manifest не доступны в image store: прежние tags были перезаписаны. Не выдавай
пересборку того же SHA или docker commit/export за прежний образ. Сначала выбери
и проверь отдельный доступный rollback bundle (точные image IDs/tars+конфиги+
восстанавливаемый backup). Если пригодного bundle нет, подготовь/проверь baseline
на изолированной восстановленной копии; не называй её исходными running images.
До перехода на рабочем тестовом стенде докажи безопасный rollback. Разверни
именно IDs нового candidate, используя его Compose overrides без --build.
После rollout health и `task-control-deployment-audit.py` обязаны подтвердить
artifactMatch=true для четырёх работающих контейнеров и нужного ZIP. Отдельно
проверь миграции30/40 и runtime tools. Registry-публикацию не имитировать:
используй настроенный разрешённый registry и проверь digests либо явно укажи
невыполненный registry gate; не менять registry credentials/права.

4. Проверь nginx main/public snippet. Старые четыре маршрута уже заменены;
не откатывай их. Новые telemetry/source/v2/ACK должны обслуживаться gateway,
корректный amoCRM Origin получает CORS, чужой Origin403; unsigned auth401,
внутренние routes через443 недоступны,8443 не открывать публике. Backup snippet:
/root/backups/nginx-task-control-public-20261019T210659Z.conf.

5. Исправь webhook штатно после обновления worker. Подтверждено:
webhook_events=[] и webhook_settings=[], reconcile раньше отправлял POST с
settings=[], job завершался400 validation. Старое тело ошибки не сохранено,
поэтому точный ответ amoCRM ретроспективно не доказан. Новый код трактует пустой
список как отсутствие подписки и снимает только принадлежащие installation hooks.
Запусти одну штатную административную команду webhook.reconcile для указанной
installation, проверь job/result и checked_at. Ожидается unregistered без
last_error, не active с выдуманными событиями. Если события обязательны для
другого включённого сервиса, они должны сохраниться. При новой ошибке сохрани
безопасные validation code/path без значений и остановись для локального fix/PR;
не делай произвольный retry, SQL clearing или переавторизацию.

6. Установка runtime выполняется в amoCRM пользователем/браузерным агентом.
Сообщи точный путь ZIP1.2.4 внутри нового candidate, SHA-256 и четыре hashes
script.js/lib/deployment.js/lib/teamos.js/lib/teamos_controller.js. Файл ZIP на
диске не доказывает установку. Descriptor подтверждать штатной CLI только после
проверки реально загруженных файлов на текущей installation/generation.
Не создавай новую installation/generation для обхода этого шага.

7. Выполни полную R09-матрицу из docs/integrations/task-control-r09-acceptance.md
через реальные Core/gateway/company boundary. Для TeamOS используй существующего
разрешённого test admin, не выпускать произвольный JWT и не менять права SQL.
Для browser proof согласуй действия с браузерным агентом; серверные synthetic
JWT/loopback не считать живой аутентификацией. Проверь references/refresh,
source request→preview→verification→apply, validate/publish/history/restore,
повтор requestId, stale/replay, tenant/actor isolation, concurrent CAS, outage/
reconnect/revoke/standalone и возврат исходного intent без потери draft/history.
Сохраняй ожидаемый и фактический HTTP/result + обезличенные revisions/checksums.
Не менять чужие tenant данные для негативных проверок.

Сейчас binding в managed schema1. Живой v2/dynamic/ACK нельзя объявить пройденным
по v1 telemetry. Активация schema2 необратима в рамках текущего binding: не
понижать её SQL и не обещать восстановление schema1. На этом тестовом аккаунте
разрешена штатная миграция через supported mode intent/activate/draft/validate/
publish обратно в managed с сохранением исходного v1 intent как legacyExplicit.
Перед активацией экспортируй исходный intent и проверь equivalent projection;
для dynamic smoke используй отдельное тестовое правило и восстанови прежнее
поведение через штатную публикацию/restore. Результат остаётся schema2 с
эквивалентными исходными правилами. Если текущий API не позволяет этот порядок
без потери правил или прав, остановись и сообщи точный blocker, не делай SQL reset.
Измерь recompute runtimeRevision без смены intent publication; подтверди настоящий
v2 bootstrap→received/applied ACK→application status из установленного виджета,
отклонение stale/foreign ACK и пустую projection после удаления test mapping.

8. Browser regression только на тестовых39230235/39246629. Уже подтверждено
в Brave на1.2.3: owner10912522, чужая task26589597 на198392 не снимает запрет;
owner task26589599 снимает его; её завершение снова блокирует выход. Обе задачи
завершены, это API-подтверждено. Не переоткрывай их. Новую проверку1.2.4 провести
с явно помеченными test tasks: чужая, ответственному, просроченная незавершённая,
завершённая; смена owner, две вкладки, повторный create click/helper и отсутствие
дублирующего POST. После теста завершить только созданные test tasks. Guard
проверяет уход из карточки, а не смену этапа. На списке закрыть reminder, остаться
на странице>30s: он должен вернуться, пока у владельца нет задачи; чужая задача
не исключает сделку. После owner task окно не возвращается; hidden tab не опрашивает.
Браузерную проверку выполняет пользователь/браузерный агент, сервер не заменяет её
Node-тестами. При потребности в действии сообщи конкретный UI шаг.

9. R10: на изолированной восстановленной копии выполни full-stack rollout и
binary rollback точных архивированных образов, проверь не только health, но и
runtime/bootstrap/ACK и данные до/после restore. Down30/40 не применять при наличии
evidence. Проверить load image tar→исходный image ID; old R06 health на схеме30/40
не выдавать за доказательство v2 compatibility. Затем верни стенд на новый
проверенный candidate и повтори artifact audit/health/runtime checks.

10. Итоговый отчёт /root/reports/task-control-r09-r10-final-YYYYMMDD.md:
каждый gate PASS/FAIL/BLOCKED с конкретным артефактом, SHA/images/ZIP/schema/
generation/epoch/publication/runtimeRevision и фактическим ACK; restore исходных
правил/mode, завершение test tasks. Acceptance/release не выставлять true до
полной матрицы. Никаких ClickUp-статусов от имени серверного агента. Верни мне
компактный итог и пути evidence; необходимые новые code fixes делаются локально
через PR, без незакоммиченных правок на сервере.
