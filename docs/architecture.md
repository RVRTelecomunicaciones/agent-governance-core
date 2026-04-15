# Architecture

## Overview

Agent Governance Core follows clean / hexagonal architecture.

```
┌─────────────────────────────────────────────┐
│                  Adapters                   │
│  ┌─────────────┐       ┌─────────────────┐  │
│  │  HTTP / SDK  │       │  Persistence    │  │
│  │  (inbound)   │       │  Memory/Runtime │  │
│  │              │       │  (outbound)     │  │
│  └──────┬───────┘       └───────┬─────────┘  │
│         │                       │            │
│  ┌──────▼───────────────────────▼─────────┐  │
│  │              Ports                      │  │
│  │   Inbound interfaces  │  Outbound       │  │
│  └──────┬───────────────────────┬─────────┘  │
│         │                       │            │
│  ┌──────▼───────────────────────▼─────────┐  │
│  │           Application                   │  │
│  │  Use cases / orchestration              │  │
│  └──────────────────┬─────────────────────┘  │
│                     │                        │
│  ┌──────────────────▼─────────────────────┐  │
│  │             Domain                      │  │
│  │  Entities, value objects, rules         │  │
│  └────────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

## Dependency rule

Dependencies point inward. Domain depends on nothing. Application depends on domain. Ports define contracts. Adapters implement ports.

## Bounded contexts

| Context     | Responsibility                                  |
|-------------|--------------------------------------------------|
| Task        | Intake, lifecycle, scope                         |
| Workflow    | Deterministic state machine execution            |
| Routing     | Strategy selection and agent assignment          |
| Policy      | Allow / deny / constrain / require-approval      |
| Approval    | Explicit approval gates                          |
| Execution   | Lease management, timeout/retry budgets          |
| Resilience  | Circuit breakers, kill switches                  |
| Escalation  | Escalation rules and triggers                    |
| Audit       | Append-only governance trail                     |

## Integration points

- **memory-engine**: consumed via outbound port (never embedded)
- **runtime adapters**: downstream of governance decisions
