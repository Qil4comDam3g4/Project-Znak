import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 20 }, // Разогрев до 20 пользователей
    { duration: '1m', target: 20 },  // Держим нагрузку
    { duration: '30s', target: 0 },  // Плавное завершение
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'], // 95% запросов должны быть быстрее 500ms
    http_req_failed: ['rate<0.01'],    // Менее 1% ошибок
  },
};

const BASE_URL = 'http://localhost:8080';

export default function () {
  // Тест эндпоинта health check
  const healthCheck = http.get(`${BASE_URL}/health`);
  check(healthCheck, {
    'health check status is 200': (r) => r.status === 200,
    'health check response has status': (r) => r.json('status') === 'ok',
  });

  // Тест создания запроса КИЗ
  const kizPayload = {
    telegram_id: 123456789,
    gtins: ['046023800404'],
    inn: '7707083893'
  };

  const kizRequest = http.post(`${BASE_URL}/api/v1/kizs`, JSON.stringify(kizPayload), {
    headers: { 'Content-Type': 'application/json' },
  });

  check(kizRequest, {
    'kiz request status is 200 or 201': (r) => r.status === 200 || r.status === 201,
  });

  sleep(1);
} 