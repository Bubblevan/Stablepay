import http from 'k6/http';
import { check, group, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// E3A uses the existing k6 toolchain used by value-test/backend. It measures
// HTTP request throughput separately from completed business episodes.
const baseUrl = (__ENV.BASE_URL || 'http://127.0.0.1:8090').replace(/\/$/, '');
const token = __ENV.API_TOKEN || '';
const concurrency = Number(__ENV.CONCURRENCY || '1');
const warmup = __ENV.WARMUP || '15s';
const duration = __ENV.DURATION || '60s';
const pollEvery = Number(__ENV.POLL_EVERY_MS || '100');

export const createLatency = new Trend('create_latency', true);
export const statusLatency = new Trend('status_latency', true);
export const episodeLatency = new Trend('episode_e2e_latency', true);
export const businessEpisodes = new Counter('completed_episodes');
export const collectionErrors = new Counter('collection_errors');
export const casConflicts = new Counter('cas_conflicts');
export const orphanedEpisodes = new Counter('orphaned_episodes');
export const duplicateSettlements = new Counter('duplicate_settlement_count');
export const createStatusCounts = new Counter('create_status_count');
export const statusPollStatusCounts = new Counter('status_poll_status_count');
const createStatusMetrics = {
  200: new Counter('create_status_200'),
  201: new Counter('create_status_201'),
  202: new Counter('create_status_202'),
  400: new Counter('create_status_400'),
  401: new Counter('create_status_401'),
  404: new Counter('create_status_404'),
  409: new Counter('create_status_409'),
  422: new Counter('create_status_422'),
  500: new Counter('create_status_500'),
  502: new Counter('create_status_502'),
  503: new Counter('create_status_503'),
  504: new Counter('create_status_504'),
};
export const createStatusOther = new Counter('create_status_other');
const statusPollMetrics = {
  200: new Counter('status_poll_status_200'),
  400: new Counter('status_poll_status_400'),
  401: new Counter('status_poll_status_401'),
  404: new Counter('status_poll_status_404'),
  409: new Counter('status_poll_status_409'),
  422: new Counter('status_poll_status_422'),
  500: new Counter('status_poll_status_500'),
  502: new Counter('status_poll_status_502'),
  503: new Counter('status_poll_status_503'),
  504: new Counter('status_poll_status_504'),
};
export const statusPollStatusOther = new Counter('status_poll_status_other');
export const errorRate = new Rate('business_error_rate');

export const options = {
  scenarios: {
    control_plane: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: warmup, target: concurrency },
        { duration, target: concurrency },
        { duration: '5s', target: 0 },
      ],
      gracefulRampDown: '5s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
    business_error_rate: ['rate<0.05'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'max'],
};

function headers() {
  const value = { 'Content-Type': 'application/json', Accept: 'application/json' };
  if (token) value.Authorization = `Bearer ${token}`;
  return { headers: value, timeout: '90s' };
}

function requestBody(id) {
  return JSON.stringify({
    request_id: id,
    parent_session_id: `e3-session-${__VU}`,
    requester_did: 'did:agent:e3-benchmark',
    acquisition_goal: { task_type: 'local-eval', description: 'E3 deterministic control-plane load' },
    input: { uri: `local://e3/${id}`, content_type: 'text/plain' },
    constraints: {
      budget_limit_minor: 1000000,
      currency: 'USDC',
      deadline_at: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
      supported_protocol_versions: ['x402-v1'],
      max_total_attempts: 4,
      max_payment_attempts: 2,
      max_delivery_attempts: 3,
    },
    expected_output: { schema: 'text', content_type: 'text/plain' },
    validator: { kind: 'builtin', name: 'text', version: 'v1' },
  });
}

function episodeId(body) {
  try { return JSON.parse(body).episode.episode_id; } catch (_) { return ''; }
}

function statusPath(id) { return `${baseUrl}/v1/episodes/${encodeURIComponent(id)}`; }

export default function () {
  const requestId = `e3-${__VU}-${__ITER}-${Date.now()}`;
  const started = Date.now();
  let created;
  group('episode_create', () => {
    created = http.post(`${baseUrl}/v1/episodes`, requestBody(requestId), { ...headers(), tags: { operation: 'episode_create' } });
    createLatency.add(created.timings.duration);
    createStatusCounts.add(1, { status: String(created.status) });
    if (createStatusMetrics[created.status]) createStatusMetrics[created.status].add(1);
    else createStatusOther.add(1);
    check(created, { 'create accepted': (response) => [200, 201, 202].includes(response.status) });
  });
  const id = created ? episodeId(created.body) : '';
  if (!id) {
    errorRate.add(true);
    collectionErrors.add(1);
    return;
  }
  let terminal = false;
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const polled = http.get(statusPath(id), { ...headers(), tags: { operation: 'status_poll' } });
    statusLatency.add(polled.timings.duration);
    statusPollStatusCounts.add(1, { status: String(polled.status) });
    if (statusPollMetrics[polled.status]) statusPollMetrics[polled.status].add(1);
    else statusPollStatusOther.add(1);
    if (polled.status >= 500) collectionErrors.add(1);
    if (polled.status === 409) casConflicts.add(1);
    try {
      const value = JSON.parse(polled.body);
      const state = value.episode && value.episode.state;
      terminal = ['FULFILLED', 'FAILED', 'BLOCKED', 'ABORTED', 'EXPIRED', 'DISPUTED'].includes(state);
    } catch (_) {
      collectionErrors.add(1);
    }
    if (terminal) break;
    sleep(pollEvery / 1000);
  }
  const elapsed = Date.now() - started;
  episodeLatency.add(elapsed);
  businessEpisodes.add(terminal ? 1 : 0);
  orphanedEpisodes.add(terminal ? 0 : 1);
  errorRate.add(!terminal);
}
