# Workflow State Machine

Use this skill when:
- adding workflow states
- changing transitions
- modeling execution lifecycle

Goals:
- keep workflow state explicit
- prevent invalid transitions
- preserve auditability

Checklist:
1. What state is added or changed?
2. What transitions are valid?
3. What transitions are forbidden?
4. What terminal states exist?
5. What audit entry should be emitted?

Never:
- allow implicit state transitions
- model workflow state in handlers
- make kill/retry/approval ambiguous
