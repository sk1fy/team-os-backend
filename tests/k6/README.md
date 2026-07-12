# Нагрузочные профили k6

Все профили обращаются только к gateway. Авторизация задаётся через `ACCESS_TOKEN` либо пару
`EMAIL`/`PASSWORD`; адрес по умолчанию — `http://localhost:8080`.

```sh
k6 run -e EMAIL=owner@example.test -e PASSWORD='password123' tests/k6/read-kb.js
k6 run -e ACCESS_TOKEN="$TOKEN" -e RATE=50 -e DURATION=5m tests/k6/read-tasks.js
k6 run -e ACCESS_TOKEN="$TOKEN" -e VUS=20 -e DURATION=2m tests/k6/move-task.js
```

Для чтения можно явно передать `ARTICLE_ID`, `BOARD_ID`, `TASK_ID`. Для move-профиля можно передать
`BOARD_ID` и список `TASK_IDS` через запятую; иначе профиль выберет первую доску и создаст отдельную
задачу на каждый VU. Тестовые задачи намеренно не удаляются: API удаления задач в контракте нет.
