# Каталог доменных событий TeamOS

Каталог фиксирует публичные события между сервисами. Схемы находятся рядом в
`contracts/events/*.proto`; transport — NATS JetStream. На wire используется
JSON envelope (`eventId`, `occurredAt`, `companyId`, `actorId`, `payload`), а
`payload` кодируется по ProtoJSON-представлению соответствующего `*Payload`
из `.proto`. Такой envelope позволяет общему outbox/идемпотентному consumer не
знать конкретный домен, сохраняя protobuf-схемы источником правды полей.

## Общие правила

- Subject: `teamos.<service>.<entity>.<action>.v1`. Версия является частью
  контракта. Несовместимое изменение создаёт `.v2`; издатель временно публикует
  обе версии, пока существуют потребители v1.
- Каждое сообщение содержит `EventMetadata`: уникальный UUID `event_id`, UTC
  `occurred_at`, UUID `company_id`, опциональные `actor_id` и `trace_id`.
- `event_id` — ключ идемпотентности. Durable-потребитель обязан записать его в
  `processed_events` в той же транзакции, что и свою реакцию, до `Ack`.
- Событие попадает в outbox в той же транзакции, что и изменение агрегата.
  Outbox-релейер публикует его с NATS header `Nats-Msg-Id = event_id`.
- Порядок гарантируется только внутри одного агрегата. Ключ агрегата передаётся
  в payload (`user_id`, `article_id`, `task_id`, `course_id` и т. п.).
- После исчерпания retry сообщение публикуется в
  `teamos.dlq.<исходный-subject>` вместе с исходными headers и текстом ошибки.
- Идентификаторы в protobuf хранятся как строки, но на границе издателя
  валидируются как UUID. Rich text передаётся только TipTap JSON.

## Subjects v1

| Subject | Protobuf schema (`payload`) | Издатель | Потребители и реакция |
|---|---|---|---|
| `teamos.org.user.created.v1` | `teamos.events.v1.OrgUserCreatedEvent` (`org.proto`) | `company` | `notifications`: приветствие активному сотруднику; `academy`: пересчитать назначения на должность/отдел. |
| `teamos.org.user.updated.v1` | `teamos.events.v1.OrgUserUpdatedEvent` (`org.proto`) | `company` | `academy`: пересчитать назначения при изменении `positionIds`/статуса; остальные потребители обновляют локальные проекции пользователя. |
| `teamos.org.user.deactivated.v1` | `teamos.events.v1.OrgUserDeactivatedEvent` (`org.proto`) | `company` | `academy`: исключить из будущих орг-назначений; `notifications`: прекратить создание новых уведомлений пользователю. |
| `teamos.org.position.deleted.v1` | `teamos.events.v1.OrgPositionDeletedEvent` (`org.proto`) | `company` | `kb`: очистить position-ссылки в access settings; `academy`: очистить назначения на должность. Снятие должности с пользователей выполняется синхронно внутри `company`. |
| `teamos.org.invite.created.v1` | `teamos.events.v1.OrgInviteCreatedEvent` (`org.proto`) | `company` | `notifications`/почтовый шлюз: отправить русскоязычное письмо, если задан `email`; приглашение по ссылке без email только сохраняется. |
| `teamos.kb.article.published.v1` | `teamos.events.v1.KbArticlePublishedEvent` (`kb.proto`) | `kb` | `notifications`: `article_published` доступным пользователям и дополнительно `article_ack_required`, если `requires_acknowledgement=true`. |
| `teamos.kb.article.updated.v1` | `teamos.events.v1.KbArticleUpdatedEvent` (`kb.proto`) | `kb` | `academy`: обновить TipTap `content` и непереименованный заголовок link-уроков с этим `article_id`. |
| `teamos.kb.article.deleted.v1` | `teamos.events.v1.KbArticleDeletedEvent` (`kb.proto`) | `kb` | `academy`: перевести link-уроки в режим `copy`, сохранив уже реплицированный контент. |
| `teamos.tasks.task.assigned.v1` | `teamos.events.v1.TasksTaskAssignedEvent` (`tasks.proto`) | `tasks` | `notifications`: создать `task_assigned` исполнителям, исключив автора. Если задана только должность, список пользователей разрешается через `company.ResolvePositionUsers`. |
| `teamos.tasks.comment.added.v1` | `teamos.events.v1.TasksCommentAddedEvent` (`tasks.proto`) | `tasks` | `notifications`: создать `task_comment` автору задачи, исполнителям и наблюдателям, исключив автора комментария и устранив дубли. |
| `teamos.tasks.task.due_soon.v1` | `teamos.events.v1.TasksTaskDueSoonEvent` (`tasks.proto`) | `tasks` worker | `notifications`: создать однократное `task_due` получателям за 24 часа до срока. |
| `teamos.academy.course.assigned.v1` | `teamos.events.v1.AcademyCourseAssignedEvent` (`academy.proto`) | `academy` | `notifications`: создать `course_assigned`; user — напрямую, position/department — через resolve RPC `company`, external — по каналу приглашения. |
| `teamos.academy.course.due_soon.v1` | `teamos.events.v1.AcademyCourseDueSoonEvent` (`academy.proto`) | `academy` worker | `notifications`: создать однократное `course_due` за три дня до срока. |
| `teamos.academy.course.deleted.v1` | `teamos.events.v1.AcademyCourseDeletedEvent` (`academy.proto`) | `academy` | `company`: удалить `course_id` из `positions.required_course_ids`. |
| `teamos.tasks.mention.created.v1` | `teamos.events.v1.MentionCreatedEvent` (`mention.proto`) | `tasks` | `notifications`: создать `mention` упомянутому пользователю со ссылкой на задачу/комментарий. |
| `teamos.kb.mention.created.v1` | `teamos.events.v1.MentionCreatedEvent` (`mention.proto`) | `kb` | `notifications`: создать `mention` упомянутому пользователю со ссылкой на статью. |

Строка `teamos.*.mention` из архитектурного плана является сокращением для
двух канонических subjects `*.mention.created.v1` выше; wildcard-subject не
публикуется.

## Владение и совместимость

- Издатель владеет схемой, каталогом и outbox-записью; потребитель не импортирует
  доменные Go-типы издателя, только сгенерированный event contract.
- Поля protobuf не удаляются и не перенумеровываются. При удалении поле и номер
  объявляются `reserved`; новые поля добавляются как optional/repeated либо с
  безопасным default.
- `buf lint` выполняется для текущего дерева, `buf breaking --against ...` —
  относительно `main`. Изменение реакции потребителя без изменения wire schema
  обновляет этот каталог, но не требует новой версии subject.
