# Agent Governance Core

A governance layer for AI agent execution systems. Controls how agents act, under what rules, with what limits, and with what execution flow.

Part of the **Sophia ecosystem**:
- `memory-engine` = operational knowledge and context
- `agent-governance-core` = control, decision, and execution supervision
- `runtime-adapters` = actual tool/side-effect execution

## What it does

- **Task intake** — receive, validate, classify tasks with risk and priority
- **Score-based routing** — decide execution strategy (direct, decompose, escalate) with explainable scoring, hard overrides, and tiebreaker
- **Policy evaluation** — evaluate rules before allowing sensitive actions (allow / deny / constrain / require approval)
- **Approval gates** — explicit HITL approval with single-resolution invariant
- **Deterministic workflows** — state machine with explicit transition table, no hidden states
- **Execution budgets** — timeout and retry budgets with lease management
- **Kill switch** — terminal, irrevocable execution stop
- **Escalation** — escalate to human or senior agent when conditions are met
- **Audit trail** — append-only record of every governance decision and transition
- **Failure telemetry** — structured failure tracking (stage, code, retryable, tool, strategy, role)

## What it does NOT do

- Long-term memory persistence (see `memory-engine`)
- Runtime execution of side effects (see `runtime-adapters`)
- Autonomous planning without governance controls
- Generic BPM engine functionality

## Architecture

Clean/hexagonal architecture with modular monolith bounded contexts.

```
cmd/                         Entry point
internal/
  domain/                    Entities, value objects, business rules
    shared/                  Transversal VOs (IDs, enums, timestamp)
    task/                    Task aggregate — intake, classification, lifecycle
    routing/                 RoutingDecision — score evaluator, overrides, tiebreaker
    policy/                  PolicyDecision — rule evaluation, sensitivity classification
    workflow/                WorkflowRun — deterministic state machine
    execution/               ExecutionLease — budgets, failure telemetry
    approval/                ApprovalRequest — HITL gates
    escalation/              EscalationTrigger — conditions, targets
    audit/                   AuditEntry — append-only, AuditContext builder
  application/               Use cases / orchestration
    intake/                  SubmitTask, ProcessTask (composite)
    routing/                 RouteTask (with decompose subtask creation)
    policyeval/              EvaluatePolicy
    workflowrun/             StartWorkflow, Kill, Pause, Resume, RegisterAttempt
    approvals/               ResolveApproval
    escalation/              TriggerEscalation
    audit/                   RecordAuditEntry (transversal), QueryAuditTrail
  ports/
    inbound/                 GovernanceService, WorkflowControl, ApprovalService, QueryService, EscalationPort
    outbound/                Repositories (8), MemoryContextProvider, GovernanceNotifier, Clock, IDGenerator, AuditRecorder
  adapters/
    inbound/
      http/                  chi v5 REST API (15 endpoints)
      sdk/                   GovernanceFacade (consumer convenience)
    outbound/
      persistence/           PostgreSQL repositories (pgx v5)
      memory/                Degradable memory-engine stub
      events/                In-process callback notifier
  infrastructure/            Config, database pool, clock, ULID generator
  bootstrap/                 Dependency wiring (pure composition)
migrations/postgres/         8 migration files (goose format)
test/
  fixtures/                  Test factories with functional options
  integration/               testcontainers-go with real PostgreSQL
```

## API Endpoints

| Method | Endpoint | Description |
|---|---|---|
| POST | `/api/v1/tasks` | Submit a new task |
| GET | `/api/v1/tasks/{taskID}` | Get task by ID |
| POST | `/api/v1/tasks/{taskID}/route` | Route a task |
| POST | `/api/v1/tasks/{taskID}/evaluate-policy` | Evaluate policy for a task |
| POST | `/api/v1/tasks/{taskID}/start-workflow` | Start workflow for a task |
| POST | `/api/v1/tasks/{taskID}/process` | Full pipeline (submit + route + policy + start) |
| POST | `/api/v1/tasks/{taskID}/escalate` | Trigger escalation |
| GET | `/api/v1/workflows/{workflowID}` | Get workflow status |
| POST | `/api/v1/workflows/{workflowID}/kill` | Kill workflow (terminal) |
| POST | `/api/v1/workflows/{workflowID}/pause` | Pause workflow |
| POST | `/api/v1/workflows/{workflowID}/resume` | Resume workflow |
| POST | `/api/v1/workflows/{workflowID}/attempts` | Register execution attempt |
| GET | `/api/v1/approvals/pending` | List pending approvals |
| POST | `/api/v1/approvals/{approvalID}/resolve` | Resolve approval (approve/deny) |
| GET | `/api/v1/audit` | Query audit trail |

## Tech Stack

- Go 1.26.2
- PostgreSQL 16+
- pgx v5 (direct, no ORM)
- chi v5 (HTTP router)
- ULID (time-sortable IDs, consistent with memory-engine)
- testify + testcontainers-go (testing)
- slog (structured logging)

## Getting Started

### Prerequisites

- Go 1.26.2+
- PostgreSQL 16+
- Docker (for integration tests)

### Configuration

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP server port |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | PostgreSQL user |
| `DB_PASSWORD` | `postgres` | PostgreSQL password |
| `DB_NAME` | `governance` | PostgreSQL database name |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |

### Build and Run

```bash
# Build
make build

# Run (requires PostgreSQL)
make run

# Run tests (unit + application)
make test

# Run integration tests (requires Docker)
make test-integration

# Lint
make lint
```

### Apply Migrations

Migrations are in `migrations/postgres/` in goose format. Apply with your preferred migration tool:

```bash
goose -dir migrations/postgres postgres "postgres://postgres:postgres@localhost:5432/governance?sslmode=disable" up
```

## Testing

| Level | Count | What | Tools |
|---|---|---|---|
| Unit | ~306 | Domain aggregates, VOs, rules, transitions | testify |
| Application | ~130 | Use cases with mocked ports | testify + interface mocks |
| Integration | 52 | Real PostgreSQL roundtrips + full pipeline | testcontainers-go |
| **Total** | **488** | | |

## Design Principles

1. **Governance decides. Runtime executes. Memory informs.**
2. Policy must be explicit — outcomes are allow, deny, constrain, or require approval
3. Routing must be explainable — scores, overrides, and reasons recorded
4. Workflows must be deterministic — explicit transition table, no ad-hoc state changes
5. High-risk actions must be gateable — approval gates with HITL
6. Kill is terminal — no transitions out
7. A task cannot execute without prior routing and policy decisions
8. Memory is consumed through ports, not embedded into domain logic
9. Audit is append-only — every governance decision is traceable

## Documentation

- [Design Spec](docs/superpowers/specs/2026-04-14-agent-governance-core-mvp-phase1-design.md)
- [Implementation Plan](docs/superpowers/plans/2026-04-14-agent-governance-core-mvp-phase1.md)
- [Architecture](docs/architecture.md)
- [Domain Invariants](docs/domain-invariants.md)
- [Rules](docs/rules.md)
- [Workflow Model](docs/workflow-model.md)
- [Policy Model](docs/policy-model.md)
- [Routing Model](docs/routing-model.md)

## License

Proprietary. All rights reserved.
