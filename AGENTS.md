# Agent Skills Index

Every non-trivial task must use one or more skills.

## Available skills

- architecture-guardrails
- task-modeling
- routing-strategy
- workflow-state-machine
- policy-evaluation
- approval-gates
- resilience-controls
- escalation-modeling
- audit-trail
- memory-integration
- persistence-postgres
- api-contracts
- testing-quality

## Usage rules

1. Use architecture-guardrails whenever boundaries may change.
2. Use testing-quality for every non-trivial change.
3. Use workflow-state-machine for any task that changes workflow state.
4. Use policy-evaluation for any task that allows, denies or constrains actions.
5. Use resilience-controls for timeout, retry, kill switch or circuit-breaker logic.
6. Use memory-integration when governance consults memory-engine.

## Task mapping

### New entity / domain rule

- task-modeling
- architecture-guardrails
- testing-quality

### Routing changes

- routing-strategy
- policy-evaluation
- testing-quality

### Workflow execution changes

- workflow-state-machine
- resilience-controls
- testing-quality

### Approval changes

- approval-gates
- audit-trail
- testing-quality

### Memory-engine integration

- memory-integration
- api-contracts
- testing-quality

### Persistence / schema changes

- persistence-postgres
- architecture-guardrails
- testing-quality
