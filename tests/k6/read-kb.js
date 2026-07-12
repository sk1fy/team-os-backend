import http from 'k6/http';
import { check, sleep } from 'k6';
import { accessToken, baseURL, firstOrFail, jsonHeaders } from './lib.js';

export const options = {
  scenarios: {
    kb_read: {
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
    'http_req_duration{operation:list_articles}': ['p(95)<500'],
    'http_req_duration{operation:get_article}': ['p(95)<500'],
  },
};

export function setup() {
  const token = accessToken();
  let articleID = __ENV.ARTICLE_ID;
  if (!articleID) {
    const response = http.get(`${baseURL}/api/v1/kb/articles`, jsonHeaders(token));
    if (response.status !== 200) throw new Error(`Список статей: HTTP ${response.status}`);
    articleID = firstOrFail(response.json(), 'Статьи').id;
  }
  return { token, articleID };
}

export default function (data) {
  const params = jsonHeaders(data.token);
  params.tags = { operation: 'list_articles' };
  const list = http.get(`${baseURL}/api/v1/kb/articles`, params);
  check(list, { 'список статей получен': (r) => r.status === 200 });

  params.tags = { operation: 'get_article' };
  const article = http.get(`${baseURL}/api/v1/kb/articles/${data.articleID}`, params);
  check(article, {
    'статья получена': (r) => r.status === 200,
    'статья содержит TipTap doc': (r) => r.status !== 200 || r.json('content.type') === 'doc',
  });
  sleep(Number(__ENV.PAUSE || 0.1));
}
