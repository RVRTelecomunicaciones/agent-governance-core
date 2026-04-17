import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = __ENV.BASE_URL || 'http://localhost:8081';
const INTENSITY = __ENV.K6_INTENSITY || 'smoke';

const profiles = {
  smoke: { vus: 5,  duration: '1m'  },
  load:  { vus: 25, duration: '5m'  },
  soak:  { vus: 25, duration: '30m' },
};
const profile = profiles[INTENSITY];
if (!profile) { throw new Error(`unknown intensity: ${INTENSITY}`); }

export const options = {
  vus: profile.vus,
  duration: profile.duration,
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

const latAttempt  = new Trend('gov_attempt_ms', true);

function postJSON(path, body, name) {
  return http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: name || path },
  });
}

function createWorkflow(iter) {
  const sub = postJSON('/api/v1/tasks', {
    type: 'bugfix',
    title: `dlq-${__VU}-${iter}`,
    scope: 'file',
    priority: 'normal',
  }, 'POST /api/v1/tasks');
  if (sub.status >= 300) { return null; }
  const taskID = sub.json('id') || sub.json('task_id');
  const rt = postJSON(`/api/v1/tasks/${taskID}/route`, {}, 'POST /api/v1/tasks/:id/route');
  if (rt.status >= 300) { return null; }
  const ev = postJSON(`/api/v1/tasks/${taskID}/evaluate-policy`, { action: 'file_write' }, 'POST /api/v1/tasks/:id/evaluate-policy');
  if (ev.status >= 300) { return null; }
  const st = postJSON(`/api/v1/tasks/${taskID}/start-workflow`, {}, 'POST /api/v1/tasks/:id/start-workflow');
  if (st.status >= 300) { return null; }
  return st.json('id') || st.json('workflow_run_id');
}

export default function () {
  const wfID = createWorkflow(__ITER);
  if (!wfID) { return; }

  // Register retryable failures until the retry budget is exhausted
  // and the workflow is quarantined. The service enforces the budget;
  // we just keep pushing failures and let the server decide when to
  // stop accepting.
  for (let i = 0; i < 10; i++) {
    const at = postJSON(`/api/v1/workflows/${wfID}/attempts`, {
      status: 'failure',
      failure_stage: 'runtime',
      failure_code: 'tool/shell_timeout',
      retryable: true,
      tool_name: 'shell',
      agent_role: 'implementer',
    }, 'POST /api/v1/workflows/:id/attempts');
    latAttempt.add(at.timings.duration);
    // Stop once server refuses further attempts (workflow terminal).
    if (at.status >= 400) { break; }
    check(at, { 'attempt accepted': (r) => r.status >= 200 && r.status < 300 });
  }

  sleep(0.1);
}
