# РС-08: private API для виджета amoCRM

Core проверяет SDK JWT и передаёт только серверный scope/user в подписанный
`POST /internal/v1/distribution/widget-runtime`; browser не выбирает компанию
или employee actor. Endpoint требует отдельный существующий механизм grant
с capability `widget-runtime`; по умолчанию доступа нет. `widget-access` остаётся
отдельной проверкой раздела/связи. Текущий actor получается из verified mapping
точной active binding с acknowledged mapping revision и активной компании.

Открытый Core контракт и эксплуатационная инструкция:
`amocrm-pro/api/distribution-widget-openapi.yaml`,
`amocrm-pro/docs/runbooks/lead-distribution-widget.md`.
Публичный REST TeamOS не меняется: новые private POST не проходят через gateway.
Бизнес-правила, конфигурация групп, очередь и действия переиспользуют РС-06/07;
widget не является вторым распределителем.

Private envelope: полный `corebridge.Scope`, `userId`, `principalExpiresAt`
(не позднее expiry проверенного SDK JWT и максимум через 15 минут), `kind,id,leadId,limit,offset,write,requestId,payload`.
Scope должен совпасть с HMAC company/installation; binding/account/revision
проверяются приложением. Контекст ограничен expiry (до 15 минут от admission),
а авторизация повторно проверяется после ожидания общего availability lock.
Поле proof expiry не входит в семантический hash повторов.

Чтение ограничено точной binding: rules/lead entries, безопасные refs
verified employee `{id,name}`, settings, availability и redacted history.
К lead/history/action добавляется точный leadId и текущая CRM проверка в Core;
подробный DTO переиспользует permission-aware ReadLead этапа 07.
Группа, используемая другой binding, недоступна для изменения.

Запись — только текущему TeamOS owner/admin; Core отдельно требует active CRM
admin. Updates используют текущую revision и общий validation/dirty wake,
действия — существующую requestId/expectedUpdatedAt логику. Новое правило
использует серверные bindingId/revision и существующую группу.

Migration **27_distribution_widget_requests**: durable pending перед эффектом,
completed/rejected после ответа. Gap ledger/effect намеренно консервативен:
после возможного эффекта pending никогда не выполняется повторно автоматически,
возвращает 202 outcome_unknown/retryAllowed=false. Это не транзакционный
exactly-once wrapper. После неопределённости нужны перечитывание shared state,
сравнение пользовательского draft и явное решение. Rollback миграции с receipts
отклоняется, чтобы не терять доказательства неизвестного результата.

Локальные сценарии не заменяют РС-08.3.2 (установленный ZIP, OAuth и настоящий
тестовый аккаунт amoCRM). Production grants/cutover/deployment здесь не выполняются.

Парный локальный профиль Core↔TeamOS дополнен сценарием РС-08: production
JWT/CORS/nonce/signature middleware, shared rules/read/write, одна revision после
повтора с новым synthetic JWT, отрицательные проверки browser companyId и текущей
роли. Работают настоящие сервисные handlers и отдельные PostgreSQL; JWT signed
fixture secret и amoCRM fixture не являются установкой ZIP в amoCRM.
