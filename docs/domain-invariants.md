# Domain Invariants

## Task

- must have ID
- must have type
- must have goal/title
- must have scope
- must have status
- cannot skip directly to completed without execution path

## WorkflowRun

- must reference a Task
- must have explicit status
- cannot be running and awaiting approval at the same time
- kill is terminal
- failed/completed/killed are terminal

## RoutingDecision

- must reference a Task
- must specify selected strategy
- must specify selected agent role or execution mode
- must be explainable

## PolicyDecision

- must reference a Task
- must have explicit outcome
- deny is terminal for the evaluated action
- require_approval must specify approval requirement

## ApprovalRequest

- must reference a Task or WorkflowRun
- must have status
- must have reason
- resolved approvals must carry resolution metadata

## ExecutionLease

- must define timeout budget
- must define retry budget
- attempts cannot exceed budget

## AuditEntry

- append only
- must have actor, action, outcome and timestamp
