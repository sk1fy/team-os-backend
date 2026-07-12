import http from 'k6/http';
import { check, sleep } from 'k6';
import { accessToken, baseURL, firstOrFail, jsonHeaders } from './lib.js';

const vus = Number(__ENV.VUS || 10);

export const options = {
  scenarios: {
    concurrent_move: {
      executor: 'constant-vus',
      vus,
      duration: __ENV.DURATION || '1m',
      gracefulStop: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'checks{scenario:concurrent_move}': ['rate>0.99'],
    'http_req_duration{operation:move_task}': ['p(95)<750'],
  },
};

export function setup() {
  const token = accessToken();
  let boardID = __ENV.BOARD_ID;
  if (!boardID) {
    const boards = http.get(`${baseURL}/api/v1/tasks/boards`, jsonHeaders(token));
    if (boards.status !== 200) throw new Error(`Список досок: HTTP ${boards.status}`);
    boardID = firstOrFail(boards.json(), 'Доски').id;
  }
  const columnsResponse = http.get(`${baseURL}/api/v1/tasks/boards/${boardID}/columns`, jsonHeaders(token));
  if (columnsResponse.status !== 200) throw new Error(`Список колонок: HTTP ${columnsResponse.status}`);
  const columns = columnsResponse.json();
  if (!Array.isArray(columns) || columns.length < 2) {
    throw new Error('Для конкурентного перемещения на доске нужны минимум две колонки');
  }

  let taskIDs = (__ENV.TASK_IDS || '').split(',').filter(Boolean);
  while (taskIDs.length < vus) {
    const response = http.post(
      `${baseURL}/api/v1/tasks`,
      JSON.stringify({ boardId: boardID, columnId: columns[0].id, title: `k6 конкурентная задача ${taskIDs.length + 1}` }),
      jsonHeaders(token),
    );
    if (response.status !== 201) throw new Error(`Создание задачи: HTTP ${response.status}, ${response.body}`);
    taskIDs.push(response.json('id'));
  }
  return { token, taskIDs, columnIDs: [columns[0].id, columns[1].id] };
}

export default function (data) {
  const taskID = data.taskIDs[(__VU - 1) % data.taskIDs.length];
  const destination = data.columnIDs[(__ITER + __VU) % data.columnIDs.length];
  const params = jsonHeaders(data.token);
  params.tags = { operation: 'move_task' };
  const response = http.post(
    `${baseURL}/api/v1/tasks/${taskID}/move`,
    JSON.stringify({ columnId: destination, order: (__ITER + __VU) % 5 }),
    params,
  );
  check(response, {
    'перемещение выполнено без конфликта': (r) => r.status === 200,
    'задача оказалась в целевой колонке': (r) => r.status !== 200 || r.json('columnId') === destination,
  });
  sleep(Number(__ENV.PAUSE || 0.05));
}
