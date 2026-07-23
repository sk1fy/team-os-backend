import http from 'k6/http';
import { check, sleep } from 'k6';

// Required: PUBLIC_ACADEMY_TOKEN. Optional read flow:
// EXTERNAL_SESSION_TOKEN + ENROLLMENT_ID (+ LESSON_VERSION_ID).
// Load shape: BASE_URL, RATE, DURATION, VUS, MAX_VUS, PAUSE, UTM_SOURCE,
// REFERRER. Secrets are read only from the process environment.

const baseURL = (__ENV.BASE_URL || 'http://localhost:8080').replace(/\/$/, '');
const publicToken = __ENV.PUBLIC_ACADEMY_TOKEN || '';
const sessionToken = __ENV.EXTERNAL_SESSION_TOKEN || '';
const enrollmentID = __ENV.ENROLLMENT_ID || '';
const lessonVersionID = __ENV.LESSON_VERSION_ID || '';
const utmSource = __ENV.UTM_SOURCE || 'k6-academy-public';
const pauseSeconds = positiveNumber('PAUSE', 0.1, true);

export const options = {
  scenarios: {
    academy_public_campaign: {
      executor: 'constant-arrival-rate',
      rate: positiveNumber('RATE', 10),
      timeUnit: '1s',
      duration: __ENV.DURATION || '2m',
      preAllocatedVUs: positiveNumber('VUS', 10),
      maxVUs: positiveNumber('MAX_VUS', 50),
      gracefulStop: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    checks: ['rate>0.99'],
    'http_req_duration{operation:academy_campaign_landing}': ['p(95)<500'],
    'http_req_duration{operation:academy_external_enrollment}': ['p(95)<500'],
    'http_req_duration{operation:academy_external_outline}': ['p(95)<500'],
    'http_req_duration{operation:academy_external_lesson}': ['p(95)<750'],
    'http_req_duration{operation:academy_external_results}': ['p(95)<500'],
  },
};

export function setup() {
  if (!publicToken) {
    throw new Error('Задайте PUBLIC_ACADEMY_TOKEN; профиль никогда не содержит реальный токен');
  }
  if ((sessionToken && !enrollmentID) || (!sessionToken && enrollmentID)) {
    throw new Error('EXTERNAL_SESSION_TOKEN и ENROLLMENT_ID задаются только вместе');
  }
  if (lessonVersionID && (!sessionToken || !enrollmentID)) {
    throw new Error('LESSON_VERSION_ID требует EXTERNAL_SESSION_TOKEN и ENROLLMENT_ID');
  }

  const ready = http.get(`${baseURL}/readyz`, { tags: { operation: 'academy_setup_ready' } });
  if (ready.status !== 200) {
    throw new Error(`Gateway или зависимости Academy не готовы: HTTP ${ready.status}`);
  }

  const landing = http.get(landingURL(), { tags: { operation: 'academy_setup_landing' } });
  if (landing.status !== 200) {
    throw new Error(`Публичная кампания недоступна: HTTP ${landing.status}`);
  }
  const kind = landing.json('kind');
  if (kind !== 'partner_promo_campaign' && kind !== 'company_candidate_campaign') {
    throw new Error(`Токен не относится к кампании: kind=${String(kind)}`);
  }

  return {
    externalReadEnabled: Boolean(sessionToken && enrollmentID),
  };
}

export default function (data) {
  const landing = http.get(landingURL(), {
    headers: referrerHeaders(),
    tags: { operation: 'academy_campaign_landing' },
  });
  check(landing, {
    'landing кампании доступен': (response) => response.status === 200,
    'landing запрещено индексировать': hasNoIndex,
    'landing запрещено кэшировать': hasNoStore,
  });

  if (data.externalReadEnabled) {
    const params = externalSessionParams();
    params.tags = { operation: 'academy_external_enrollment' };
    const enrollmentURL = http.url`${baseURL}/api/v1/public/academy/enrollments/${encodeURIComponent(enrollmentID)}`;
    const enrollment = http.get(enrollmentURL, params);
    check(enrollment, {
      'внешнее прохождение доступно своей сессии': (response) => response.status === 200,
      'прохождение запрещено кэшировать': hasNoStore,
    });

    params.tags = { operation: 'academy_external_outline' };
    const outlineURL = http.url`${baseURL}/api/v1/public/academy/enrollments/${encodeURIComponent(enrollmentID)}/outline`;
    const outline = http.get(outlineURL, params);
    check(outline, {
      'outline доступен своей сессии': (response) => response.status === 200,
      'outline запрещено индексировать': hasNoIndex,
    });

    if (lessonVersionID) {
      params.tags = { operation: 'academy_external_lesson' };
      const lessonURL = http.url`${baseURL}/api/v1/public/academy/enrollments/${encodeURIComponent(enrollmentID)}/lessons/${encodeURIComponent(lessonVersionID)}`;
      const lesson = http.get(lessonURL, params);
      check(lesson, {
        'доступный урок возвращён': (response) => response.status === 200,
        'урок запрещено кэшировать': hasNoStore,
      });
    }

    params.tags = { operation: 'academy_external_results' };
    const resultsURL = http.url`${baseURL}/api/v1/public/academy/enrollments/${encodeURIComponent(enrollmentID)}/results`;
    const results = http.get(resultsURL, params);
    check(results, {
      'свои результаты доступны': (response) => response.status === 200,
      'результаты запрещено кэшировать': hasNoStore,
    });
  }

  sleep(pauseSeconds);
}

function landingURL() {
  const token = encodeURIComponent(publicToken);
  const source = encodeURIComponent(utmSource);
  // http.url preserves the real request URL while replacing interpolated
  // values in the k6 metric name, so secret tokens never become metric tags.
  return http.url`${baseURL}/api/v1/public/academy/access/${token}?utm_source=${source}&utm_medium=load_test`;
}

function externalSessionParams() {
  return {
    headers: {
      Cookie: `teamos_academy_external=${sessionToken}`,
    },
  };
}

function referrerHeaders() {
  if (!__ENV.REFERRER) return {};
  return { Referer: __ENV.REFERRER };
}

function hasNoStore(response) {
  const value = String(response.headers['Cache-Control'] || '').toLowerCase();
  return value.includes('private') && value.includes('no-store');
}

function hasNoIndex(response) {
  const value = String(response.headers['X-Robots-Tag'] || '').toLowerCase();
  return value.includes('noindex') && value.includes('nofollow');
}

function positiveNumber(name, fallback, allowZero = false) {
  const value = Number(__ENV[name] || fallback);
  if (!Number.isFinite(value) || (allowZero ? value < 0 : value <= 0)) {
    throw new Error(`${name} должен быть ${allowZero ? 'неотрицательным' : 'положительным'} числом`);
  }
  return value;
}
