package shared

import "fmt"

// RiskLevel represents the risk classification of an operation.
type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelCritical RiskLevel = "critical"
)

var validRiskLevels = map[RiskLevel]bool{
	RiskLevelLow:      true,
	RiskLevelMedium:   true,
	RiskLevelHigh:     true,
	RiskLevelCritical: true,
}

func NewRiskLevel(s string) (RiskLevel, error) {
	rl := RiskLevel(s)
	if !validRiskLevels[rl] {
		return "", fmt.Errorf("invalid risk level: %q", s)
	}
	return rl, nil
}

func (rl RiskLevel) String() string { return string(rl) }

// Priority represents the priority of an operation.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

var validPriorities = map[Priority]bool{
	PriorityLow:    true,
	PriorityNormal: true,
	PriorityHigh:   true,
	PriorityUrgent: true,
}

func NewPriority(s string) (Priority, error) {
	p := Priority(s)
	if !validPriorities[p] {
		return "", fmt.Errorf("invalid priority: %q", s)
	}
	return p, nil
}

func (p Priority) String() string { return string(p) }

// FailureStage represents the stage at which a failure occurred.
type FailureStage string

const (
	FailureStageRouting       FailureStage = "routing"
	FailureStagePolicy        FailureStage = "policy"
	FailureStageApproval      FailureStage = "approval"
	FailureStageWorkflow      FailureStage = "workflow"
	FailureStageExecution     FailureStage = "execution"
	FailureStageRuntime       FailureStage = "runtime"
	FailureStageMemoryContext FailureStage = "memory_context"
	FailureStageNotification  FailureStage = "notification"
)

var validFailureStages = map[FailureStage]bool{
	FailureStageRouting:       true,
	FailureStagePolicy:        true,
	FailureStageApproval:      true,
	FailureStageWorkflow:      true,
	FailureStageExecution:     true,
	FailureStageRuntime:       true,
	FailureStageMemoryContext: true,
	FailureStageNotification:  true,
}

func NewFailureStage(s string) (FailureStage, error) {
	fs := FailureStage(s)
	if !validFailureStages[fs] {
		return "", fmt.Errorf("invalid failure stage: %q", s)
	}
	return fs, nil
}

func (fs FailureStage) String() string { return string(fs) }
