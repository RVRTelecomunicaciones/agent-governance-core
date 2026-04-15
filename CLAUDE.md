# Agent Governance Core — Claude Guide

## What this repository is

A reusable governance layer for AI agent execution systems.

It is responsible for:

- task intake
- routing decisions
- deterministic workflows
- policy evaluation
- approval gates
- timeout budgets
- retry budgets
- kill switches
- escalation rules
- audit trail

It integrates with:

- `memory-engine` for context and active knowledge
- runtime/adapters for actual execution

## What this repository is not

It is not:

- the memory engine
- the runtime execution layer
- the git/PR adapter layer
- a chatbot
- a generic workflow builder
- a free-form autonomous planner with no controls

## Required development mindset

Think like a production architect and backend engineer.
Prioritize safety, determinism, observability and clear contracts.
Do not invent extra scope.
Do not collapse governance, execution and memory into one module.

## Must-read files before coding

1. `docs/rules.md`
2. `docs/domain-invariants.md`
3. `AGENTS.md`

## Core design principles

1. Governance decides; runtime executes.
2. Policy must be explicit.
3. Routing must be explainable.
4. Workflows must be stateful and auditable.
5. High-risk actions must be gateable.
6. Kill is terminal.
7. A task cannot execute without a prior routing/policy decision.
8. Memory is consumed through ports, not embedded into domain logic.

## Before coding

Always state:

1. task understanding
2. affected modules
3. affected invariants
4. persistence impact
5. workflow/policy impact
6. test impact

## Technical stack

- Go
- PostgreSQL in production
- clean / hexagonal architecture
- HTTP + SDK first
- deterministic workflow execution
- memory-engine consumed via outbound port

## Output style

When implementing:

1. describe understanding
2. identify skills to use
3. show minimal implementation plan
4. implement
5. list tests
6. list assumptions or risks

## Never do this

- do not mix governance with runtime adapters
- do not let workflows mutate state without audit
- do not execute sensitive actions without policy checks
- do not introduce hidden state machines
- do not make kill/approval behavior ambiguous
