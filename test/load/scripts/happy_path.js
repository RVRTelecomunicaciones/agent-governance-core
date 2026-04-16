import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE = 'http://localhost:8081';
const INTENSITY = __ENV.K6_INTENSITY || 'smoke';

const profiles = {
  smoke: { vus: 10, duration: '1m' },
  load:  { vus: 50, duration: '5m' },
  soak:  { vus: 50, duration: '30m' },
};
const profile = profiles[INTENSITY];
if (!profile) { throw new Error(`unknown intensity: ${INTENSITY}`); }

export const options = {
  vus: profile.vus,
  duration: profile.duration,
  // No thresholds — DISCOVERY, not validation.
  summaryTrendStats: ['avg', 'min', 'med', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

const latSubmit   = new Trend('gov_submit_ms',   true);
const latRoute    = new Trend('gov_route_ms',    true);
const latEval     = new Trend('gov_eval_ms',     true);
const latStart    = new Trend('gov_start_ms',    true);
const latAttempt  = new Trend('gov_attempt_ms',  true);

function postJSON(path, body, name) {
  return http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
    tags: { name: name || path },
  });
}

export default function () {
  // 1. Submit
  const sub = postJSON('/api/v1/tasks', {
    type: 'bugfix',
    title: `load-${__VU}-${__ITER}`,
    scope: 'file',
    priority: 'normal',
  }, 'POST /api/v1/tasks');
  latSubmit.add(sub.timings.duration);
  check(sub, { 'submit 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (sub.status >= 300) { return; }
  const taskID = sub.json('id') || sub.json('task_id');

  // 2. Route
  const rt = postJSON(`/api/v1/tasks/${taskID}/route`, {}, 'POST /api/v1/tasks/:id/route');
  latRoute.add(rt.timings.duration);
  check(rt, { 'route 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (rt.status >= 300) { return; }

  // 3. Evaluate policy
  const ev = postJSON(`/api/v1/tasks/${taskID}/evaluate-policy`, { action: 'file_write' }, 'POST /api/v1/tasks/:id/evaluate-policy');
  latEval.add(ev.timings.duration);
  check(ev, { 'eval 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (ev.status >= 300) { return; }

  // 4. Start workflow
  const st = postJSON(`/api/v1/tasks/${taskID}/start-workflow`, {}, 'POST /api/v1/tasks/:id/start-workflow');
  latStart.add(st.timings.duration);
  check(st, { 'start 2xx': (r) => r.status >= 200 && r.status < 300 });
  if (st.status >= 300) { return; }
  const wfID = st.json('id') || st.json('workflow_run_id');

  // 5. Register 1 success attempt → workflow completes (terminal).
  //    Multiple success attempts are rejected by the state machine
  //    (ErrTerminalWorkflow) — see register_attempt.go.
  const at = postJSON(`/api/v1/workflows/${wfID}/attempts`, {
    status: 'success',
    tool_name: 'shell',
    agent_role: 'implementer',
  }, 'POST /api/v1/workflows/:id/attempts');
  latAttempt.add(at.timings.duration);
  check(at, { 'attempt 2xx': (r) => r.status >= 200 && r.status < 300 });

  sleep(0.1);
}
