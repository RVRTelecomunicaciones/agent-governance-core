package memory_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/russellcxl/agent-governance-core/internal/adapters/outbound/memory"
	"github.com/russellcxl/agent-governance-core/internal/domain/shared"
)

func validTaskID(t *testing.T) shared.TaskID {
	t.Helper()
	id, err := shared.NewTaskID("01KP7WTMTBAE67Z5NHCJZHQ5XV")
	require.NoError(t, err)
	return id
}

func TestStubMemoryContextProvider_ReturnsNonNilEmptyContext(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	provider := memory.NewStubMemoryContextProvider(logger)

	result, err := provider.GetRelevantContext(context.Background(), validTaskID(t), "test query")

	require.NoError(t, err)
	require.NotNil(t, result, "MemoryContext must not be nil")
	assert.Equal(t, float64(0), result.SimilarTaskFailureRate)
	assert.False(t, result.HasRelevantHeuristics)
}

func TestStubMemoryContextProvider_DoesNotError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	provider := memory.NewStubMemoryContextProvider(logger)

	_, err := provider.GetRelevantContext(context.Background(), validTaskID(t), "any query")

	assert.NoError(t, err)
}

func TestStubMemoryContextProvider_LogsWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	provider := memory.NewStubMemoryContextProvider(logger)

	_, _ = provider.GetRelevantContext(context.Background(), validTaskID(t), "search query")

	logOutput := buf.String()
	assert.Contains(t, logOutput, "memory-engine unavailable")
	assert.Contains(t, logOutput, "memory_context_provider")
}
