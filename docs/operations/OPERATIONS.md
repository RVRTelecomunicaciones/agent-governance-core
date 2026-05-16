# Operations Runbook — agent-governance-core

After reading this runbook you will be able to deploy the service, verify it is healthy, correlate logs and metrics, and diagnose the most common failure modes.

---

## Deploy

The service ships as a Helm chart at `helm/agent-governance-core/`.

### Required values

Three secrets have `required` guards in `helm/agent-governance-core/templates/secret.yaml` and will cause `helm install` to fail if absent:

| Value path | Env var injected | Description |
|---|---|---|
| `secrets.dbHost` | `DB_HOST` | PostgreSQL hostname |
| `secrets.dbUser` | `DB_USER` | PostgreSQL user |
| `secrets.dbPassword` | `DB_PASSWORD` | PostgreSQL password |

`secrets.dbName` and `secrets.dbPort` have no `required` guard but should always be set explicitly; defaults (`governance` / `5432`) will not exist in a fresh cluster.

### Install

```bash
helm install agent-governance-core ./helm/agent-governance-core \
  --namespace governance \
  --create-namespace \
  --set secrets.dbHost=<PG_HOST> \
  --set secrets.dbUser=<PG_USER> \
  --set secrets.dbPassword=<PG_PASSWORD> \
  --set secrets.dbName=<PG_DATABASE> \
  --set secrets.dbSslMode=require \
  --set image.tag=<VERSION>
```

### Upgrade

```bash
helm upgrade agent-governance-core ./helm/agent-governance-core \
  --namespace governance \
  --reuse-values \
  --set image.tag=<VERSION>
```

### Database migrations

Migrations are Goose-format SQL files under `migrations/postgres/`. The chart does not run them automatically by default (`migrate.enabled: false`). Run them out-of-band before deploying a new version:

```bash
# Example: run via psql from a bastion or init job
psql "$DATABASE_URL" -f migrations/postgres/001_create_tasks.sql
# ... repeat for each unapplied migration in sequence
```

There are currently 12 migrations (001–012). Migration 012 (`012_unify_id_columns_to_char26.sql`) alters 24 ULID columns across 10 tables from `VARCHAR(26)` to `CHAR(26)` — required for cross-repo consistency with `sophia-orchestator`. Run it on any database upgraded from a pre-012 schema.

---

## Env vars

All configuration is read from environment variables at startup. The Helm chart injects `config.*` values as plain env vars and `secrets.*` values via a Kubernetes Secret mounted with `envFrom`.

### Required (service will not start correctly without these)

| Env var | Controls | Example |
|---|---|---|
| `DB_HOST` | PostgreSQL host | `postgres.internal` |
| `DB_USER` | PostgreSQL user | `governance` |
| `DB_PASSWORD` | PostgreSQL password | `s3cr3t` |
| `DB_NAME` | PostgreSQL database name | `governance` |

### Common tuning vars

| Env var | Default | Controls |
|---|---|---|
| `DB_SSLMODE` | `disable` (chart default: `require`) | PostgreSQL SSL mode. Always `require` in staging/prod. |
| `LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error`. |
| `OTEL_ENABLED` | `false` | Enables the OTel SDK (traces + metrics via OTLP). Requires a Collector endpoint. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | OTLP endpoint consumed by the OTel SDK when `OTEL_ENABLED=true`. Example: `http://otel-collector:4318`. |
| `ADAPTIVE_ROUTING_ENABLED` | `false` | Enables dynamic strategy re-classification from past failure stats. Keep `false` unless explicitly tested. |

Sampling is controlled via standard OTel env vars when `OTEL_ENABLED=true`:

| Env var | Description | Example |
|---|---|---|
| `OTEL_TRACES_SAMPLER` | Sampler name | `parentbased_traceidratio` |
| `OTEL_TRACES_SAMPLER_ARG` | Ratio for ratio-based samplers | `0.1` (10%) |

The service defaults to 10% trace sampling when these vars are not set and `OTEL_ENABLED=true`. See `docs/observability/slos.md` for OTel overhead measurements.

---

## Healthchecks

Both probes are mounted outside `/api/v1` — they require no auth and are independent of business logic.

### Liveness — `GET /health`

Returns `200 OK` immediately with no I/O. Kubernetes uses this to determine whether to restart the pod. It intentionally does not check the database; a transient DB failure should not trigger a restart.

```json
{"status": "ok"}
```

Probe config (from `values.yaml`): `initialDelaySeconds: 10`, `periodSeconds: 10`.

### Readiness — `GET /ready`

Pings the database with a 2-second timeout.

| Response | Body | Meaning |
|---|---|---|
| `200 OK` | `{"status":"ready","checks":{"db":"ok"}}` | DB reachable — pod accepts traffic |
| `503 Service Unavailable` | `{"status":"degraded","checks":{"db":"<error>"}}` | DB ping failed or timed out — pod removed from Service endpoints |

Probe config: `initialDelaySeconds: 5`, `periodSeconds: 5`.

If `/ready` returns 503 persistently, the pod will be excluded from load balancing. Check database connectivity before investigating the application.

---

## Logs and metrics

### Log format

Logs are structured JSON emitted to stdout via Go's `log/slog`. Every request log includes a `trace_id` and `span_id` field injected by the W3C Traceparent middleware (`internal/adapters/inbound/http/middleware`).

```json
{
  "time": "2026-05-15T10:00:00Z",
  "level": "INFO",
  "msg": "...",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7"
}
```

Filter by `trace_id` to correlate all log lines for a single request across the governance pipeline.

### Metrics

Metrics are emitted via OTel and require `OTEL_ENABLED=true`. There is no standalone Prometheus `/metrics` endpoint — metrics are exported via OTLP to a Collector.

Key instruments (meter name: `governance`):

| Metric | Type | Useful for |
|---|---|---|
| `governance.tasks.submitted` | Counter | Task intake rate by `type`, `scope`, `risk_level` |
| `governance.routing.duration_ms` | Histogram | Routing latency; labelled by `strategy` and `overridden` |
| `governance.policy.duration_ms` | Histogram | Policy evaluation latency by `outcome` |
| `governance.policy.outcomes` | Counter | Allow/deny/escalate distribution |
| `governance.workflow.duration_ms` | Histogram | End-to-end workflow execution time |
| `governance.workflow.transitions` | Counter | State machine activity |
| `governance.approval.wait_ms` | Histogram | Time tasks spend waiting for human approval |
| `governance.execution.failures` | Counter | Failures with `failure_stage` label |
| `governance.circuit_breaker.trips` | Counter | CLOSED→OPEN transitions |
| `governance.memory.degraded` | Counter | Memory context misses (non-fatal) |

Alert rules are defined in `test/observability/grafana/provisioning/alerting/governance-rules.yaml`. SLO targets are documented in `docs/observability/slos.md`.

### Audit trail

Every governance decision (routing, policy, workflow transition, approval, escalation, circuit breaker trip) is written as an immutable row to the `audit_entries` table in PostgreSQL. The schema is append-only by design. Query via `GET /api/v1/audit` with filter params, or directly:

```sql
SELECT actor_id, action, target_id, created_at, context
FROM audit_entries
ORDER BY created_at DESC
LIMIT 50;
```

---

## Troubleshooting

### 1. Pod stuck in `0/1 Ready` — readiness probe failing

**Symptom:** `kubectl get pods` shows `0/1 READY`. `GET /ready` returns 503 with `{"db": "..."}`.

**Cause:** The pod cannot reach PostgreSQL. Most common causes: wrong `DB_HOST`, `DB_PASSWORD`, missing secret, or `DB_SSLMODE=require` with a server that does not have SSL enabled.

**Fix:**
```bash
kubectl exec -it <pod> -- env | grep DB_
# Verify values match the actual PostgreSQL instance.
# Test connectivity:
kubectl exec -it <pod> -- nc -zv $DB_HOST $DB_PORT
```

---

### 2. Migrations not applied — service returns 500 on all business endpoints

**Symptom:** Any call to `/api/v1/tasks` or similar returns 500. Logs contain a PostgreSQL error referencing a missing table (e.g., `relation "tasks" does not exist`).

**Cause:** Migrations were not run before deploying a new image. The Helm chart does not apply migrations automatically.

**Fix:** Run the SQL migration files in sequence against the target database. Start from the first unapplied migration. If upgrading from a schema prior to migration 012, ensure `012_unify_id_columns_to_char26.sql` is applied — it alters 24 ULID columns across 10 tables and is required for cross-repo compatibility.

---

### 3. `GET /governance/v1/decisions/phase` returns 503

**Symptom:** The orchestrator receives 503 on phase decision endpoints.

**Cause:** The `WithPhaseDecisions` wiring was not completed during bootstrap. This surfaces as a nil `phaseDecisions` field in the HTTP server. In a correctly wired binary this should not happen, but it can occur if a custom entrypoint skips `Wire()`.

**Fix:** Confirm the binary uses `bootstrap.Wire()`. The `WithPhaseDecisions` call is part of that wiring path. Do not construct `httpAdapter.NewServer` manually without wiring this service.

---

### 4. All tasks end up `quarantined`

**Symptom:** `governance.workflow.transitions{to_state="quarantined"}` is elevated. Tasks do not complete.

**Cause:** Retry budget exhaustion. The workflow state machine moves a task to `quarantined` after it exceeds its configured retry budget — this is intentional, not a bug. The underlying cause is typically repeated execution failures logged under `governance.execution.failures`.

**Fix:** Query audit entries for the affected task IDs to see the `failure_stage` label on each failure. Check the runtime/executor side for the root cause. Do not increase retry budgets without understanding the underlying failure.

```sql
SELECT action, context, created_at
FROM audit_entries
WHERE target_id = '<task_id>'
ORDER BY created_at;
```

---

### 5. High `governance.memory.degraded` counter

**Symptom:** `governance.memory.degraded` counter is non-zero and rising. Routing decisions may be lower quality (less context-enriched) but the service continues operating.

**Cause:** The outbound memory context provider (`StubMemoryContextProvider` in current deployments) is returning degraded/empty context. This is a non-fatal degradation path by design — routing and policy evaluation proceed without memory context.

**Fix:** This is expected in environments where `memory-engine` is not deployed or not reachable. If memory enrichment is required, deploy `memory-engine` and wire a real `MemoryContextProvider` implementation. The stub is the current default.

---

## References

- Helm chart: `helm/agent-governance-core/`
- Migrations: `migrations/postgres/`
- Config loader: `internal/infrastructure/config/config.go`
- HTTP router and probes: `internal/adapters/inbound/http/router.go`, `health_handler.go`
- OTel setup: `internal/infrastructure/observability/otel.go`
- Bootstrap wiring: `internal/bootstrap/wire.go`
- SLOs and alert thresholds: `docs/observability/slos.md`
- Domain invariants: `docs/domain-invariants.md`
- Architecture: `docs/architecture.md`
