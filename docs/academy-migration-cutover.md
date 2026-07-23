# Академия: migration, backfill, cutover и rollback

## 1. Цель и границы

Переход заменяет legacy-модель «курс и progress пользователя» на:

```text
Course → immutable CourseVersion → Assignment/Access/Campaign → Enrollment
```

Миграция выполняется по правилу expand → backfill → verify → cutover → contract. `/api/v1`
меняется только аддитивно. Старые таблицы, колонки и маршруты не удаляются до отдельного
breaking-релиза `/api/v2`.

Этот документ — runbook выпуска, а не свидетельство production-cutover. В текущей ветке backend-код
phases 0–8 подготовлен, но переключение frontend, production-canary и замеры divergence выполняются
отдельно по этому runbook. Полный frontend cutover сейчас дополнительно заблокирован несовместимыми
формами V1/V2-ответов на одних и тех же `/api/v1` путях; детали и обязательный выход через
versioned contract описаны в разделе 7.2.

### 1.1. Статус реализации

| Phase | Готово в кодовой базе | Ещё требуется при выпуске |
|---|---|---|
| 0 | ADR, state diagrams, additive OpenAPI/protobuf, схема миграции | утвердить окно и ответственных |
| 1 | ownership, lifecycle/distribution, object-level policy, audit skeleton | проверить реальные роли и tenant-данные |
| 2 | immutable versions, version 1 backfill, draft/publish и legacy DTO adapter | сверить content hashes на production |
| 3 | version-pinned enrollment, lesson/quiz progress, legacy progress adapter | сверить counts/progress и latency |
| 4 | партнёрские курсы, preview, restrictions, soft delete, независимое копирование | проверить file-copy backlog и уведомления |
| 5 | templates, KB reuse/snapshot и file clone | проверить seed всех компаний |
| 6 | ExternalLearner, персональный доступ, OTP, внешняя сессия и отчёты | production SMTP, rate limits и ротация секретов |
| 7 | promo/candidate campaigns, UTM/referrer, funnel, timeline и scoped reports | retention, dashboards и Academy-specific alerts |
| 8 | additive frontend endpoints, runbooks, reconciliation command, legacy-write guard, deprecation и security/load profiles | versioned frontend DTO contract, production cutover и sign-off |

«Готово в кодовой базе» означает наличие схемы и command/read paths в этой ветке. Это не означает,
что миграции уже применены к production или что ручные gates ниже пройдены.

## 2. Инварианты перехода

1. `company_id` присутствует и проверяется на каждой tenant-строке и query.
2. Published content после cutover неизменяем.
3. Enrollment — единственный источник resume/progress.
4. Assignment, access, campaign и enrollment фиксируют версию.
5. Backfill идемпотентен; большой production backfill должен быть возобновляем по checkpoint.
6. Повторный запуск не создаёт новые версии или дубли enrollment.
7. Backfill не отправляет продуктовые уведомления и не учитывается как новая активация/публикация в аналитике.
8. В пользовательском API нет hard delete.
9. Откат приложения не удаляет expand-схему и уже перенесённые данные.
10. Contract phase начинается только после подтверждённого отсутствия legacy readers/writers.

## 3. Предварительная инвентаризация

До expand снять контрольный снимок без PII в логах:

- число курсов по company/status/visibility;
- число sections, lessons и quizzes на курс;
- число assignments по типу assignee;
- число progress rows, completed lessons и quiz attempts;
- число legacy public courses и фактически использованных external assignments;
- строки без `company_id`, битые ссылки и дубли порядка;
- курсы с неоднозначным partner ownership;
- content, который не проходит TipTap или quiz validation;
- файлы, недоступные academy actor.

Ошибочные данные не исправляются догадкой. Они попадают в quarantine report с безопасным техническим идентификатором; решение фиксируется до cutover.

## 4. Expand

Expand выполняется совместимыми миграциями, которые допускают одновременную работу старой и новой версии сервиса.

### 4.1. Владение и состояния

- добавить ownership, lifecycle и distribution в course root;
- добавить actor/timestamp полей archive/delete;
- создать audit, restriction и outbox-compatible storage;
- backfill ownership по безопасному правилу: legacy course становится company-owned, если достоверная partner metadata отсутствует;
- только после проверки заполнения включить constraints owner type.

### 4.2. Версии

- создать version tables для course metadata, sections, lessons и quizzes;
- добавить draft/latest published pointers;
- добавить unique version number и one-draft constraint;
- исходные summary/content tables пока не менять и не удалять.

### 4.3. Enrollment

- создать enrollments, lesson progress и versioned quiz attempts;
- добавить nullable `course_version_id` в assignments;
- добавить constraints learner identity после backfill;
- сохранить legacy progress tables read-only после cutover.

### 4.4. Последующие агрегаты

- добавить origins и template version tables;
- добавить ExternalLearner, challenges, sessions, personal accesses и campaigns;
- добавить analytics и необходимые report indexes;
- добавить KB partner access/reuse аддитивно в сервисе `kb`.

Новые NOT NULL/unique/check ограничения включаются только после backfill и проверки orphan/mismatch gauges.

## 5. Backfill

В текущей ветке version 1 и employee enrollment backfill выполняют SQL-миграции `000005` и
`000006` внутри migration transaction. Отдельного resumable job для них нет. До production DBA
оценивает объём и длительность блокировок: если они не укладываются в согласованное maintenance
window, inline backfill не запускают, а выносят в отдельный idempotent batch job с checkpoint по
company/primary key. В labels метрик нельзя помещать course/user/email IDs.

### 5.1. Course version 1

Для каждого legacy course:

1. получить transaction/advisory lock курса;
2. проверить, не существует ли уже migration version 1;
3. создать version 1 с тем же `company_id`;
4. скопировать metadata, sections, lessons, quizzes, порядок и TipTap JSON;
5. published legacy course сделать published v1 и выставить latest pointer;
6. draft legacy course сделать draft v1 и выставить draft pointer;
7. вычислить `content_hash` для повторной сверки;
8. использовать уникальную version 1 и `content_hash` как idempotency marker;
9. не публиковать обычное `course.version.published` и не отправлять notifications.

Повторный запуск сравнивает marker и hash, а не создаёт version 2.

### 5.2. Employee progress

Для каждой legacy пары `(user_id, course_id)`:

1. найти version 1 того же tenant;
2. создать или переиспользовать enrollment с `source_type=legacy` и `attempt_number=1`;
3. перенести completed lesson IDs на соответствующие lesson version IDs;
4. перенести quiz attempts без повторной оценки исторических ответов;
5. вычислить current lesson, progress status, started/completed/last activity;
6. вычислить процент по обязательным урокам version 1;
7. сравнить число completed lessons, attempts и финальный статус с legacy;
8. считать backfill успешным только при полном совпадении reconciliation.

Неразрешимый mapping lesson ID останавливает cutover этой company и попадает в quarantine. Он не игнорируется и не уменьшает progress молча.

### 5.3. Assignments

- assignment получает version 1 соответствующего курса;
- target types `user`, `position`, `department` сохраняются;
- новый `external` assignment после включения new-write запрещён;
- существующий `external` переносится только при наличии реальных данных и однозначного получателя, иначе маркируется legacy для ручного решения;
- начатые enrollment автоматически на другую version не переводятся.

### 5.4. Legacy public courses

- direct-ID endpoint временно остаётся read-only;
- через него нельзя создавать новый внешний progress;
- owner получает отдельное действие миграции legacy public course в token campaign;
- старые URL учитываются метрикой до deprecation;
- direct course ID не преобразуется в secret token.

### 5.5. System templates

- для каждой существующей company создать tenant-local immutable copies по `system_template_key`;
- повторный job делает upsert по key/version и не создаёт дубли;
- обработчик `company.created.v1` использует тот же idempotent путь;
- seed version/hash фиксируются в checkpoint.

## 6. Dual-write и dual-read

Раздел фиксирует целевую схему совместимого rollout. Текущая ветка уже переводит новые
progress-команды на enrollment и строит legacy progress DTO из него, но общего automatic shadow
comparator/fallback controller не содержит; эквивалентность подтверждается reconciliation.

### 6.1. Запись

После включения нового command path:

- course content пишется в draft version tables;
- progress и attempts пишутся в enrollment model;
- assignment фиксирует published version;
- старые summary columns при необходимости обновляются только как производная проекция новой транзакции;
- legacy summary никогда не принимается как источник для обратной записи в новую модель;
- ошибка необязательной legacy projection учитывается метрикой и retry job, но не откатывает уже принятую новую бизнес-операцию, если это нарушит outbox consistency.

Для rolling deploy старая версия приложения должна оставаться совместимой с expand schema. Перед запретом legacy writes подтверждается, что старые instances выведены из rotation. После появления repeat enrollment или version >1 включать старую progress-команду снова нельзя: она не выражает новую семантику без потерь.

### 6.2. Чтение

Переключение выполняется отдельно для каждого агрегата:

1. legacy read остаётся пользовательским ответом;
2. shadow read строит эквивалентный DTO из новой модели;
3. нормализованный comparator записывает только тип расхождения и счётчик;
4. после нулевых integrity mismatches новая модель становится primary read;
5. legacy adapter строится из новой модели для старого REST DTO;
6. fallback на legacy разрешён короткое время только для unmigrated tenant и учитывается метрикой;
7. после завершения backfill fallback отключается.

Shadow comparison исключает ожидаемые различия формата и сравнивает бизнес-смысл: course/version/status, completed lesson set, current lesson, attempt totals, процент и даты с согласованной точностью.

## 7. Feature flags

Ниже зафиксирован обязательный порядок переключателей. Имена — целевой rollout-интерфейс, а не
перечень уже доступных env-переменных. В текущем сервисе нет общего server-side flag controller с
company allowlist и автоматической проверкой зависимостей. До его появления release owner обязан
воспроизвести тот же порядок через canary deployment, конфигурацию API client и routing; клиент не
должен самостоятельно выбирать источник данных.

| Флаг | Назначение | Зависимость |
|---|---|---|
| `academy.ownership.enforce` | объектная авторизация и новые lifecycle/distribution поля | ownership backfill завершён |
| `academy.versions.write` | новые draft/publish команды пишут version model | version schema готова |
| `academy.versions.shadow_read` | сравнение legacy course DTO с version adapter | version 1 backfill |
| `academy.versions.primary_read` | чтение курса из version model | shadow divergence = 0 |
| `academy.legacy_summary.project` | временная проекция новых writes в legacy summary | только окно совместимости |
| `academy.enrollments.write` | progress/attempt commands пишут enrollment model | enrollment schema и assignment pinning |
| `academy.enrollments.shadow_read` | сравнение legacy progress с enrollment | progress backfill |
| `academy.enrollments.primary_read` | resume/report из enrollment | integrity divergence = 0 |
| `academy.partner_controls.enabled` | partner CRUD, pause/block/copy | ownership + versions + enrollment |
| `academy.templates.enabled` | templates и instantiate | versions + origins |
| `academy.external_access.enabled` | verification/session/personal access | enrollment primary read |
| `academy.campaigns.enabled` | promo/candidate campaigns и analytics | external access |
| `academy.legacy_public.read_only` | временный direct-ID compatibility read | всегда без progress write |
| `academy.legacy_writes.disabled` | запрет всех legacy mutations | primary reads и reconciled projection |

Порядок включения: ownership → versions write/shadow/primary → enrollments
write/shadow/primary → partner controls → templates → external access → campaigns → единый frontend
→ запрет legacy writes → отключение fallback reads. Primary read нельзя включать до сверки backfill,
а campaigns — до внешнего доступа.

### 7.1. Что автоматизировано, а что проверяется вручную

Автоматизировано в текущей ветке:

- SQL-миграции создают version 1 и enrollment из legacy-данных и останавливаются на нарушении
  tenant/mapping invariants;
- новые progress-команды используют enrollment, а legacy progress DTO строится из enrollment;
- SQL trigger запрещает новые assignments с `assignee_type=external`;
- ownership, immutable publication и lifecycle transitions проверяются backend;
- `make academy-reconcile` выполняет PII-free count/progress gate и завершается с ошибкой при
  расхождении;
- gateway включает fail-closed legacy mutation guard через
  `GATEWAY_ACADEMY_LEGACY_WRITES_READ_ONLY=true` после переключения frontend;
- `make academy-security-smoke` и `make academy-load` покрывают security headers/cookies/CORS/CSRF,
  legacy read-only guard, rate limit и campaign read profile;
- `make check-contract`, `make test`, `make test-race` и `make lint` проверяют кодовую базу.

Ручные release gates до отдельной автоматизации:

- company allowlist/canary и порядок переключения клиентов;
- сохранение результата `make academy-reconcile` и расширенных запросов из раздела 8.1 в release
  ticket;
- shadow comparison и сбор Academy migration metrics, перечисленных ниже;
- подтверждение, что старые application instances выведены из rotation;
- переключение единого frontend Academy и удаление экспериментальных routes;
- retention purge, Academy-specific dashboards и alerts;
- ротация внешних секретов по
  [academy-operations-runbook.md](academy-operations-runbook.md).

### 7.2. Обязательный gate frontend-контракта

`VITE_ACADEMY_V2` нельзя включать в production только потому, что все новые route names появились в
OpenAPI. Legacy и новый frontend используют несколько одинаковых `/api/v1/academy/...` путей, но
ожидают разные JSON DTO. В частности, расходятся формы списка/детали курса, authoring версий и
builder-команд, enrollment player, templates, restrictions и copy. Например, legacy-клиент ждёт
массив курсов, а новый — `PaginatedResult`; сервер не может выбрать правильный ответ без явной
версии контракта.

Поэтому release gate состоит из следующих действий:

1. Определить и добавить `/api/v2/academy` для несовместимых DTO; существующие `/api/v1` response
   bodies не менять.
2. Реализовать V2 adapters для всех совпадающих путей и добавить contract tests из frontend типов.
3. Переключить новый Academy client на `/api/v2`, прогнать старый и новый clients параллельно и
   подтвердить отсутствие fallback/shape errors.
4. Только после этого включить `VITE_ACADEMY_V2`, выдержать canary и включить
   `GATEWAY_ACADEMY_LEGACY_WRITES_READ_ONLY=true`.

Отсутствующие уникальные операции можно и нужно добавлять аддитивно в `/api/v1`; это не решает
конфликт уже существующих response DTO. Silent replacement V1-ответов считается breaking change и
запрещён.

## 8. Divergence metrics и gates

Минимальный целевой набор метрик:

```text
academy_migration_backfill_rows_total{entity,result}
academy_migration_backfill_remaining{entity}
academy_migration_quarantine_rows{entity,reason}
academy_courses_without_version_total
academy_courses_with_multiple_drafts_total
academy_assignments_without_version_total
academy_course_content_hash_mismatch_total{content_type}
academy_enrollment_count_mismatch_total{learner_type}
academy_enrollment_progress_mismatch_total{field}
academy_quiz_attempt_count_mismatch_total
academy_dual_write_projection_failures_total{target}
academy_shadow_read_mismatch_total{aggregate,field}
academy_shadow_read_fallback_total{aggregate}
academy_legacy_write_requests_total{operation}
academy_legacy_public_reads_total
academy_migration_job_lag_seconds{job}
```

Эти Academy migration metrics пока не зарегистрированы как отдельный Prometheus-набор. До их
реализации release owner сохраняет результаты SQL-сверки и canary-наблюдения в release ticket.
Метрики не должны содержать UUID, email и другие high-cardinality/PII labels. Для расследования
используются `request_id` и защищённый quarantine report.

Gates cutover:

- `courses_without_version`, `multiple_drafts`, `assignments_without_version` равны нулю;
- quarantine пуст или каждый остаток имеет согласованное исключение, блокирующее только известную company;
- content hash mismatch равен нулю;
- enrollment, completed lesson и attempt totals совпадают;
- shadow business mismatches равны нулю на полном canary-окне;
- projection failures обработаны retry и равны нулю;
- p95 нового read path не ухудшает согласованный SLO;
- нет старых application instances и legacy write requests.

Алерты:

- рост divergence после достижения нуля;
- stalled backfill/checkpoint;
- projection retry exhaustion;
- legacy write после `legacy_writes.disabled`;
- fallback после завершённого cutover;
- outbox или file-copy saga lag.

### 8.1. Count/progress reconciliation

Запросы выполняются read-only пользователем отдельно для каждой production-копии. Вывод содержит
tenant UUID и считается внутренним артефактом. Сначала сохранить totals «до», затем применить
миграции, выполнить запросы ниже дважды и приложить результат к release ticket.

При остановленном legacy и новом write-трафике выполнить автоматический PII-free gate сразу после
миграции (он возвращает ненулевой exit code при mismatch):

```sh
ACADEMY_DB_URL='postgres://…' make academy-reconcile
```

После возобновления новых enrollment-команд legacy progress закономерно становится stale, поэтому
mutable progress equality повторно не используется как production health check. Ниже приведены
расширенные диагностические запросы с техническими UUID для локализации расхождения во время
write-freeze; их результат хранится только как защищённый внутренний артефакт.

Курс без ровно одной version 1:

```sql
SELECT c.company_id, c.id AS course_id, count(v.id) AS version_one_count
FROM courses AS c
LEFT JOIN course_versions AS v
  ON v.company_id = c.company_id
 AND v.course_id = c.id
 AND v.number = 1
GROUP BY c.company_id, c.id
HAVING count(v.id) <> 1;
```

Непривязанное назначение или ссылка на другую компанию/курс:

```sql
SELECT a.company_id, a.id AS assignment_id
FROM assignments AS a
LEFT JOIN course_versions AS v
  ON v.company_id = a.company_id
 AND v.course_id = a.course_id
 AND v.id = a.course_version_id
WHERE v.id IS NULL;
```

Каждая legacy progress row должна иметь ровно одно исходное прохождение version 1. Готовые
enrollment, созданные назначением без progress, в этой сверке намеренно не считаются ошибкой:

```sql
SELECT p.company_id, p.user_id, p.course_id, count(e.id) AS enrollment_count
FROM progress AS p
LEFT JOIN course_versions AS v
  ON v.company_id = p.company_id
 AND v.course_id = p.course_id
 AND v.number = 1
LEFT JOIN course_enrollments AS e
  ON e.company_id = p.company_id
 AND e.course_id = p.course_id
 AND e.course_version_id = v.id
 AND e.learner_type = 'user'
 AND e.user_id = p.user_id
 AND e.attempt_number = 1
 AND e.source_type IN ('assignment', 'legacy')
GROUP BY p.company_id, p.user_id, p.course_id
HAVING count(e.id) <> 1;
```

Симметричная разница completed lessons должна быть пустой:

```sql
WITH mapped AS (
    SELECT DISTINCT ON (p.company_id, p.user_id, p.course_id)
           p.company_id, p.user_id, p.course_id, p.completed_lesson_ids,
           e.id AS enrollment_id
    FROM progress AS p
    JOIN course_versions AS v
      ON v.company_id = p.company_id
     AND v.course_id = p.course_id
     AND v.number = 1
    JOIN course_enrollments AS e
      ON e.company_id = p.company_id
     AND e.course_id = p.course_id
     AND e.course_version_id = v.id
     AND e.learner_type = 'user'
     AND e.user_id = p.user_id
     AND e.attempt_number = 1
     AND e.source_type IN ('assignment', 'legacy')
    ORDER BY p.company_id, p.user_id, p.course_id, e.id
), legacy_completed AS (
    SELECT company_id, user_id, course_id, unnest(completed_lesson_ids) AS lesson_id
    FROM mapped
), enrollment_completed AS (
    SELECT m.company_id, m.user_id, m.course_id, lp.lesson_version_id AS lesson_id
    FROM mapped AS m
    JOIN enrollment_lesson_progress AS lp
      ON lp.company_id = m.company_id
     AND lp.enrollment_id = m.enrollment_id
     AND lp.status = 'completed'
)
(SELECT 'missing_in_enrollment' AS mismatch, * FROM legacy_completed
 EXCEPT
 SELECT 'missing_in_enrollment', * FROM enrollment_completed)
UNION ALL
(SELECT 'missing_in_legacy' AS mismatch, * FROM enrollment_completed
 EXCEPT
 SELECT 'missing_in_legacy', * FROM legacy_completed);
```

Исторические quiz attempts после expand остаются в той же таблице, но должны быть pinned:

```sql
SELECT qa.company_id, count(*) AS unmapped_attempts
FROM quiz_attempts AS qa
WHERE qa.enrollment_id IS NULL
   OR qa.quiz_version_id IS NULL
   OR NOT EXISTS (
       SELECT 1
       FROM course_enrollments AS e
       JOIN course_version_quizzes AS q
         ON q.company_id = e.company_id
        AND q.course_version_id = e.course_version_id
       WHERE e.company_id = qa.company_id
         AND e.id = qa.enrollment_id
         AND q.id = qa.quiz_version_id
   )
GROUP BY qa.company_id;
```

Все пять запросов должны вернуть ноль строк. Дополнительно сравниваются totals по company:
`courses`, version 1, `progress`, mapped employee enrollments, completed lessons и quiz attempts.
Расхождение нельзя «исправлять» удалением строк; оно переводит company в quarantine до разбора.

## 9. Порядок rollout

1. Назначить release owner, incident commander, окно и tested rollback build; сделать backup и
   проверить restore metadata.
2. Развернуть expand schema и совместимый backend без переключения frontend; перед backfill
   остановить все Academy legacy/new writes на согласованное окно.
3. Зафиксировать totals и выполнить reconciliation из раздела 8.1 под write-freeze. Сама миграция выполняет
   version/enrollment backfill транзакционно; отдельный resumable job в текущей ветке отсутствует.
4. Проверить ownership, tenant constraints, version 1, assignment pinning, progress и attempts.
5. Направить внутреннюю canary company на новый backend path; затем расширять company allowlist
   только после полного окна без integrity mismatch.
6. Переключить version reads/writes, затем enrollment reads/writes и server-side resume.
7. Включить partner controls, templates, external access и campaigns строго по зависимостям.
8. Проверить SMTP, file clone/outbox, deadline worker и campaign reports. Analytics retention и
   Academy-specific alerts должны быть приняты по operations runbook.
9. Переключить единый frontend Academy и его API client. Старые Academy Grok/Opus routes пока не
   удалять: сначала снять факт отсутствия трафика.
10. Вывести все старые backend instances из rotation. Убедиться, что новые внешние assignments уже
    блокирует DB trigger и что progress DTO читается из enrollment.
11. Закрыть оставшиеся legacy mutation routes, установив
    `GATEWAY_ACADEMY_LEGACY_WRITES_READ_ONLY=true`, не меняя `/api/v1` response schema. Подтвердить
    ответы `410`, `make academy-security-smoke` и отсутствие legacy write-трафика.
12. После возобновления трафика повторять только structural/orphan gates и сверку новых
    enrollment-reports; legacy mutable progress equality больше не является валидной. Выдержать
    полный canary/release цикл, затем отключить legacy fallback reads. Возврат legacy writer после
    version >1 или repeat enrollment запрещён.
13. Оставить `GET /api/v1/public/academy/courses/{id}` только как compatibility read. Новый внешний
    трафик использует token routes `/api/v1/public/academy/access/{token}`.
14. Объявить окно deprecation direct-ID endpoint потребителям, проверить опубликованные
    `Deprecation`/`Sunset`/`Link`/`Warning` headers, измерить обращения и дождаться нуля. Operation
    уже помечена `deprecated: true` в OpenAPI; перенос объявленной даты требует согласования.
15. Удаление experimental frontend routes, legacy таблиц/полей и direct-ID endpoint выполнять
    отдельными изменениями. Breaking contract cleanup допустим только в `/api/v2`.

### 9.1. Gate остановки legacy writes

Остановка подтверждена только когда одновременно выполнено следующее:

- в rotation нет build до Academy expand;
- новый внешний assignment отклоняется, а персональный/кампанийный доступ создаётся token-командой;
- legacy progress endpoints возвращают adapter из enrollment и не меняют таблицу `progress`;
- за согласованное окно нет прямых INSERT/UPDATE в legacy content/progress tables от application
  role; до появления метрики это проверяется DB audit/logging;
- write-freeze reconciliation был нулевым, а post-cutover structural/orphan gates остаются нулевыми;
- frontend не вызывает experimental mutation clients;
- rollback build умеет читать новую schema и не включает legacy writer.

Нельзя использовать revoke прав на таблицы как первый способ остановки: фоновые workers и
compatibility adapters ещё могут читать legacy rows. Сначала инвентаризировать фактические SQL
обращения, затем ограничивать DB role отдельным проверенным изменением.

## 10. Rollback

### 10.1. До новых writes

Убрать canary routing и вернуть совместимый предыдущий build/read adapter. Expand schema остаётся на
месте. Down migration допустима только если новые таблицы пусты и это подтверждено проверкой;
штатно она не требуется.

### 10.2. После backfill, до cutover

Если inline migration завершилась ошибкой, её transaction должна откатиться целиком: исправить
причину на копии, повторить rehearsal и только затем запускать снова. Если для большого production
набора был отдельно реализован batch job, остановить его на checkpoint и сохранить позицию.
Backfilled rows вручную не удалять. Исправление quarantine выполняется отдельной идемпотентной
командой.

### 10.3. После включения новых writes

Откат read path возможен только если legacy projection синхронизирована и divergence равен нулю. Новую запись и outbox нельзя удалять. Если новая операция не представима в legacy-модели, например version >1 или repeat enrollment, откат к legacy writer запрещён; применяется forward fix при сохранении новой модели источником истины.

### 10.4. После отключения legacy writes

Повторно включать legacy writes нельзя без отдельного reconciliation и решения инцидента. Безопасный rollback приложения использует предыдущий совместимый build, который читает новую модель или её legacy adapter. Database contract/hard drop не выполняется в том же релизе, поэтому expand-структура остаётся доступной.

### 10.5. Доменные переходы

- Product delete необратим и не отменяется rollback релиза.
- Published version не размораживается для редактирования.
- Restore курса не размораживает enrollment и campaigns.
- Уже отправленные события не компенсируются удалением; компенсация оформляется новой idempotent domain command/event.
- File copy использует retry/compensation saga и очищает только неиспользуемые копии, созданные неуспешной saga.

## 11. Проверка завершения

Перед каждым расширением allowlist выполняются:

- migration/backfill integration tests на реальном PostgreSQL;
- повторный migration rehearsal на свежей restore-копии и двойной запуск reconciliation;
- concurrent publish/activation/progress tests;
- tenant isolation и IDOR tests;
- OpenAPI/buf compatibility checks для соответствующего contract PR;
- `make gen`, `make test`, `make test-race`, `make lint`, `make check-contract`;
- сверка доступных generic dashboards, ручных Academy gates, quarantine и rollback rehearsal.

Contract phase допускается только после полного cutover frontend, нулевых legacy writes/reads за согласованное окно и отдельного решения о `/api/v2`.
