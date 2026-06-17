package shared_test

import (
	"testing"

	"github.com/RVRTelecomunicaciones/agent-governance-core/internal/domain/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRiskLevel(t *testing.T) {
	validValues := []string{"low", "medium", "high", "critical"}

	for _, v := range validValues {
		t.Run("accepts "+v, func(t *testing.T) {
			rl, err := shared.NewRiskLevel(v)
			require.NoError(t, err)
			assert.Equal(t, v, rl.String())
		})
	}

	t.Run("rejects invalid value", func(t *testing.T) {
		_, err := shared.NewRiskLevel("extreme")
		assert.Error(t, err)
	})

	t.Run("rejects empty value", func(t *testing.T) {
		_, err := shared.NewRiskLevel("")
		assert.Error(t, err)
	})
}

func TestPriority(t *testing.T) {
	validValues := []string{"low", "normal", "high", "urgent"}

	for _, v := range validValues {
		t.Run("accepts "+v, func(t *testing.T) {
			p, err := shared.NewPriority(v)
			require.NoError(t, err)
			assert.Equal(t, v, p.String())
		})
	}

	t.Run("rejects invalid value", func(t *testing.T) {
		_, err := shared.NewPriority("critical")
		assert.Error(t, err)
	})

	t.Run("rejects empty value", func(t *testing.T) {
		_, err := shared.NewPriority("")
		assert.Error(t, err)
	})
}

func TestFailureStage(t *testing.T) {
	validValues := []string{
		"routing", "policy", "approval", "workflow",
		"execution", "runtime", "memory_context", "notification",
	}

	for _, v := range validValues {
		t.Run("accepts "+v, func(t *testing.T) {
			fs, err := shared.NewFailureStage(v)
			require.NoError(t, err)
			assert.Equal(t, v, fs.String())
		})
	}

	t.Run("rejects invalid value", func(t *testing.T) {
		_, err := shared.NewFailureStage("unknown")
		assert.Error(t, err)
	})

	t.Run("rejects empty value", func(t *testing.T) {
		_, err := shared.NewFailureStage("")
		assert.Error(t, err)
	})
}
