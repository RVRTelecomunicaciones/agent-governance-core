package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	obslog "github.com/russellcxl/agent-governance-core/internal/infrastructure/obs/log"
	"github.com/russellcxl/agent-governance-core/internal/domain/trace"
	"github.com/stretchr/testify/require"
)

func TestTraceHandler_AddsTraceAttrs_WhenContextHasTrace(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(obslog.NewTraceHandler(base))

	tr := trace.Trace{
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:  "00f067aa0ba902b7",
		Sampled: true,
	}
	ctx := trace.NewContext(context.Background(), tr)

	logger.InfoContext(ctx, "test message", "k", "v")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", record["trace_id"])
	require.Equal(t, "00f067aa0ba902b7", record["span_id"])
	require.Equal(t, "test message", record["msg"])
	require.Equal(t, "v", record["k"])
}

func TestTraceHandler_OmitsTraceAttrs_WhenContextEmpty(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(obslog.NewTraceHandler(base))

	logger.InfoContext(context.Background(), "no trace here")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	_, hasTrace := record["trace_id"]
	require.False(t, hasTrace, "no trace_id should be emitted when ctx has no trace")
}

func TestTraceHandler_WithAttrs_PreservesEnrichment(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(obslog.NewTraceHandler(base)).With("svc", "governance")

	tr := trace.Trace{
		TraceID: "aaaabbbbccccddddaaaabbbbccccdddd",
		SpanID:  "1122334455667788",
		Sampled: true,
	}
	ctx := trace.NewContext(context.Background(), tr)
	logger.InfoContext(ctx, "with attrs")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	require.Equal(t, "governance", record["svc"])
	require.Equal(t, "aaaabbbbccccddddaaaabbbbccccdddd", record["trace_id"])
	require.Equal(t, "1122334455667788", record["span_id"])
}
