import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = 'http://localhost:8081';
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

const latAttempt = new Trend('gov_attempt_ms', true);
const latList    = new Trend('gov_list_breakers_ms', true);

function postJSON(path, body) {
  return http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  });
}

function createWorkflow(tag) {
  const sub = postJSON('/api/v1/tasks', {
    type: 'bugfix',
    title: `breaker-${tag}`,
    scope: 'file',
    priority: 'normal',
  });
  if (sub.status >= 300) { return null; }
  const taskID = sub.json('id') || sub.json('task_id');
  const rt = postJSON(`/api/v1/tasks/${taskID}/route`, {});
  if (rt.status >= 300) { return null; }
  const ev = postJSON(`/api/v1/tasks/${taskID}/evaluate-policy`, { action: 'file_write' });
  if (ev.status >= 300) { return null; }
  const st = postJSON(`/api/v1/tasks/${taskID}/start-workflow`, {});
  if (st.status >= 300) { return null; }
  return st.json('id') || st.json('workflow_run_id');
}

export default function () {
  const wfID = createWorkflow(`${__VU}-${__ITER}`);
  if (!wfID) { return; }

  // Force 3 consecutive failures on the same (tool, role) to trip the breaker.
  for (let i = 0; i < 3; i++) {
    const at = postJSON(`/api/v1/workflows/${wfID}/attempts`, {
      status: 'failure',
      failure_stage: 'runtime',
      failure_code: 'tool/shell_timeout',
      retryable: true,
      tool_name: 'shell',
      agent_role: 'implementer',
    });
    latAttempt.add(at.timings.duration);
    if (at.status >= 400) { break; }
  }

  // Observe the tripped state.
  const ls = http.get(`${BASE}/api/v1/breakers?state=open`);
  latList.add(ls.timings.duration);
  check(ls, { 'list breakers 2xx': (r) => r.status >= 200 && r.status < 300 });

  sleep(0.1);
}
