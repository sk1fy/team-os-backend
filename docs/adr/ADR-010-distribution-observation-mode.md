# ADR-010: изолированный режим наблюдения распределения

Дата: 2026-10-03. Статус: принято для локального РС-10.

## Контекст

Ordinary pause сохраняет принятую queue и первую activation boundary. Поэтому
active=false не является dry-run: очередь может исполниться после resume.
Нужны предварительные планы без CRM команды, business claims и расхода RR.

## Решение

У правила additive `executionMode=live|observe`, default live при active=false,
executionEpoch и nullable liveStartedAt. Новые правила выключены по умолчанию.
Observe использует отдельные durable leased jobs и immutable observations.
Дедупликация — rule/epoch/entry/event. Расчёт выделен из настоящего live worker;
графики/исключения те же, RR читается без создания/резервирования claim.

Смена режима CAS и блокирует любую unsettled живую queue; не отменяет строки
сама. Availability lock → rule FOR UPDATE → проверка busy/used сериализованы
с bounded admission rule FOR SHARE SKIP LOCKED. Observation scheduling также
держит rule share lock: point нельзя изменить поверх pending work. Mode/epoch
проверяются после worker locks, перед RPC и в private validate-decision.

Каждый flip меняет epoch; переход в live фиксирует fresh monotonic floor.
first_activation_at/история сохраняются. Старые наблюдения и исторические входы
не становятся live work: нужны trusted source occurred/received и entry creation
после liveStartedAt. Обычная live pause/resume сохраняет прежнюю очередь/floor.

Чтение observations через owner API требует current company/section/actor и
fresh CRM lead permission; вся запрещённая строка скрывается, unknown source
не превращается в пустой успешный список. Widget actual owner получает отдельный
fresh currentLead, независимо от исторического плана и наличия live queue.

## Последствия

Кандидат наблюдения не резервируется и может повторяться в нескольких планах.
Source precision seconds не доказывает вход после microsecond floor в ту же
секунду. Старый frozen intent не удаляется на Core404: поздний admission всё ещё
возможен. Expired never-admitted требует operator inspection/отдельного negative
admission протокола. Down28 запрещён при использованных observation identities.

Живой legacy cutover, настоящий OAuth/SDK и серверный пилот не подтверждены
локальными fixtures. Процедура и матрица находятся в `amocrm-pro`:
`docs/runbooks/lead-distribution-pilot.md`, `docs/specs/lead-distribution-v1/11-local-acceptance.md`.
