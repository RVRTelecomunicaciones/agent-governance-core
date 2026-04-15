# Policy Evaluation

Use this skill when:
- adding policy checks
- evaluating risk/action constraints
- deciding allow/deny/approval

Goals:
- keep policy outcomes explicit
- keep policy decisions explainable
- keep dangerous actions constrained

Checklist:
1. What action is being evaluated?
2. What risk level applies?
3. What outcome is possible?
4. Is approval required?
5. What audit entry must be recorded?

Never:
- hide deny logic inside runtime code
- allow side effects before policy evaluation
- make policy outcome implicit
