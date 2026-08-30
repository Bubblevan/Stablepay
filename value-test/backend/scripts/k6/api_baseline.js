import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';

export const errorRate = new Rate('error_rate');

const baseUrl = __ENV.BASE_URL || 'https://ai.wenfu.cn';
const route = __ENV.ROUTE || '/healthz';
const query = __ENV.QUERY || '';
const expectedStatus = Number(__ENV.EXPECTED_STATUS || '200');
const method = (__ENV.METHOD || 'GET').toUpperCase();
const duration = __ENV.DURATION || '60s';
const rate = Number(__ENV.RATE || '20');
const preAllocatedVUs = Number(__ENV.PREALLOCATED_VUS || '20');
const maxVUs = Number(__ENV.MAX_VUS || String(Math.max(preAllocatedVUs, rate * 2)));
const thinkTime = Number(__ENV.THINK_TIME_MS || '0');
const apiKey = __ENV.API_KEY || '';
const insecureSkipTlsVerify = (__ENV.INSECURE_SKIP_TLS_VERIFY || 'false') === 'true';

// ===== 修复1: 将预期状态码注册为 "expected response" =====
// 这样 k6 内置的 http_req_failed 指标会把 402（对 /pay/require）视为成功
const expectedResponseCallback = http.expectedStatuses(expectedStatus);

export const options = {
  insecureSkipTLSVerify: insecureSkipTlsVerify,
  scenarios: {
    baseline: {
      executor: 'constant-arrival-rate',
      rate: rate,
      timeUnit: '1s',
      duration: duration,
      preAllocatedVUs: preAllocatedVUs,
      maxVUs: maxVUs,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
    error_rate: ['rate<0.05'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

function buildUrl() {
  if (!query) {
    return `${baseUrl}${route}`;
  }
  const normalized = query.startsWith('?') ? query : `?${query}`;
  return `${baseUrl}${route}${normalized}`;
}

function buildParams() {
  const headers = {
    'Accept': 'application/json',
  };
  if (apiKey) {
    headers['X-API-Key'] = apiKey;
  }
  return {
    headers,
    // ===== 修复1续: 使用 responseCallback 让 k6 知道哪些状态码算 "成功" =====
    responseCallback: expectedResponseCallback,
    tags: {
      route: route,
      expected_status: String(expectedStatus),
      method: method,
    },
  };
}

// ===== 修复2: 有限调试输出，打印非预期响应 =====
// 使用全局计数器避免日志爆炸（每个 VU 只打印第一次非预期）
let unexpectedLogged = false;

export default function () {
  const url = buildUrl();
  const params = buildParams();

  let res;
  if (method === 'GET') {
    res = http.get(url, params);
  } else {
    res = http.request(method, url, null, params);
  }

  const ok = check(res, {
    'expected status': (r) => r.status === expectedStatus,
  });
  errorRate.add(!ok);

  // 只在非预期响应且尚未记录时打印一次（每个 VU 一次）
  if (!ok && !unexpectedLogged) {
    console.warn(
      `[unexpected] route=${route}` +
      ` expected=${expectedStatus}` +
      ` actual=${res.status}` +
      
      ` retry_after=${res.headers['Retry-After'] || '-'}` +
      ` body=${String(res.body || '').slice(0, 300)}`
    );
    unexpectedLogged = true;
  }

  if (thinkTime > 0) {
    sleep(thinkTime / 1000);
  }
}