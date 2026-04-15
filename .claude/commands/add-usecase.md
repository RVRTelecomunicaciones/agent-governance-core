Add a new use case to agent-governance-core.

## Context

Read these files first:
- docs/rules.md
- docs/domain-invariants.md
- docs/architecture.md
- AGENTS.md

## Instructions

1. Ask me to describe the use case (what it does, which module it belongs to).
2. Verify the module already exists in internal/domain/ and internal/application/.
3. Identify affected domain entities and invariants.
4. Define the inbound port (interface) for this use case.
5. Define any new outbound ports needed.
6. Implement the application service.
7. Write unit tests for the use case.
8. Update the relevant adapter if needed (HTTP handler, etc.).

## Skills to use

- architecture-guardrails
- testing-quality
- The skill matching the affected module
