# Agent Governance Core

A reusable governance layer for AI agent execution systems.

## What it does

- Task intake and lifecycle management
- Routing decisions (strategy selection, agent assignment)
- Deterministic workflow state machines
- Policy evaluation (allow / deny / constrain / require-approval)
- Approval gates
- Timeout and retry budgets
- Kill switches
- Escalation rules
- Append-only audit trail

## What it does NOT do

- Long-term memory persistence (see `memory-engine`)
- Runtime execution (see runtime adapters)
- Git/PR operations (see adapter layer)

## Architecture

Clean / hexagonal architecture. See [docs/architecture.md](docs/architecture.md).

```
cmd/                    → Entry points
internal/
  domain/               → Entities, value objects, business rules
  application/          → Use cases, orchestration
  ports/                → Inbound/outbound interfaces
  adapters/             → HTTP, SDK, persistence, memory, runtime
  infrastructure/       → Config, logging, database, observability
```

## Getting started

```bash
# Build
make build

# Run tests
make test

# Run linter
make lint
```

## Documentation

- [Architecture](docs/architecture.md)
- [Rules](docs/rules.md)
- [Domain Invariants](docs/domain-invariants.md)
- [Workflow Model](docs/workflow-model.md)
- [Policy Model](docs/policy-model.md)
- [Routing Model](docs/routing-model.md)

## Tech stack

- Go
- PostgreSQL
- Clean / hexagonal architecture
- HTTP + SDK first
