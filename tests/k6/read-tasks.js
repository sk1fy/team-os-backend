import http from 'k6/http';
import { check, sleep } from 'k6';
import { accessToken, baseURL, firstOrFail, jsonHeaders } from './lib.js';

export const options = {
  scenarios: {
    tasks_read: {
      executor: 'constant-arrival-rate',
      rate: Number(__ENV.RATE || 20),
      timeUnit: '1s',
      duration: __ENV.DURATION || '2m',
      preAllocatedVUs: Number(__ENV.VUS || 20),
      maxVUs: Number(__ENV.MAX_VUS || 100),
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{operation:list_tasks}': ['p(95)<500'],
    'http_req_duration{operation:get_task}': ['p(95)<500'],
  },
};

export function setup() {
  const token = accessToken();
  let boardID = __ENV.BOARD_ID;
  if (!boardID) {
    const response = http.get(`${baseURL}/api/v1/tasks/boards`, jsonHeaders(token));
    if (response.status !== 200) throw new Error(`Список досок: HTTP ${response.status}`);
    boardID = firstOrFail(response.json(), 'Доски').id;
  }
  const response = http.get(`${baseURL}/api/v1/tasks?boardId=${boardID}`, jsonHeaders(token));
  if (response.status !== 200) throw new Error(`Список задач: HTTP ${response.status}`);
  const taskID = __ENV.TASK_ID || firstOrFail(response.json(), 'Задачи').id;
  return { token, boardID, taskID };
}

export default function (data) {
  const params = jsonHeaders(data.token);
  params.tags = { operation: 'list_tasks' };
  const list = http.get(`${baseURL}/api/v1/tasks?boardId=${data.boardID}`, params);
  check(list, { 'список задач получен': (r) => r.status === 200 });

  params.tags = { operation: 'get_task' };
  const task = http.get(`${baseURL}/api/v1/tasks/${data.taskID}`, params);
  check(task, { 'задача получена': (r) => r.status === 200 });
  sleep(Number(__ENV.PAUSE || 0.1));
}
