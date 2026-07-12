import http from 'k6/http';

export const baseURL = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/$/, '');

export function jsonHeaders(token) {
  return {
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    },
  };
}

export function accessToken() {
  if (__ENV.ACCESS_TOKEN) {
    return __ENV.ACCESS_TOKEN;
  }
  const email = __ENV.EMAIL;
  const password = __ENV.PASSWORD;
  if (!email || !password) {
    throw new Error('Задайте ACCESS_TOKEN или пару EMAIL/PASSWORD');
  }
  const response = http.post(
    `${baseURL}/api/v1/auth/login`,
    JSON.stringify({ email, password }),
    { headers: { 'Content-Type': 'application/json' }, tags: { operation: 'login' } },
  );
  if (response.status !== 200) {
    throw new Error(`Вход не выполнен: HTTP ${response.status}, ${response.body}`);
  }
  return response.json('accessToken');
}

export function firstOrFail(items, entity) {
  if (!Array.isArray(items) || items.length === 0) {
    throw new Error(`${entity} не найдены: сначала загрузите seed-данные или передайте ID`);
  }
  return items[0];
}
