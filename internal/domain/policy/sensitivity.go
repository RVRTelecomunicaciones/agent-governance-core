package policy

// sensitiveActions is a static classification of actions considered sensitive.
var sensitiveActions = map[string]bool{
	"shell_execution":   true,
	"git_push":          true,
	"external_api_call": true,
	"deployment":        true,
	"data_deletion":     true,
}

// IsActionSensitive returns true if the given action is classified as sensitive.
func IsActionSensitive(action string) bool {
	return sensitiveActions[action]
}
