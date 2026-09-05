package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	return m
}

// contract.md section 8 requires component, method, line, message and level.
func TestRequiredFieldsArePresent(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(logging.Options{Component: "middleware", Output: &buf})

	log.Info("quote requested")

	m := decode(t, &buf)
	if m["component"] != "middleware" {
		t.Errorf("component = %v", m["component"])
	}
	if m["msg"] != "quote requested" {
		t.Errorf("msg = %v", m["msg"])
	}
	if m["level"] != "INFO" {
		t.Errorf("level = %v", m["level"])
	}
	method, _ := m["method"].(string)
	if !strings.HasPrefix(method, "logging_test.") {
		t.Errorf("method should name the calling function, got %v", m["method"])
	}
	if _, ok := m["line"].(float64); !ok {
		t.Errorf("line missing or not a number: %v", m["line"])
	}
}

// The correlation id travels on the context, never as an argument.
func TestCorrelationIDComesFromContext(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(logging.Options{Component: "bff", Output: &buf})
	ctx := logging.WithCorrelationID(context.Background(), "abc123")

	log.InfoContext(ctx, "handled")

	if got := decode(t, &buf)["correlationId"]; got != "abc123" {
		t.Errorf("correlationId = %v", got)
	}
}

func TestNoCorrelationFieldWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(logging.Options{Component: "bff", Output: &buf})

	log.InfoContext(context.Background(), "handled")

	if _, present := decode(t, &buf)["correlationId"]; present {
		t.Error("correlationId should be omitted when not set, not emitted empty")
	}
}

// Tracing is optional. When telemetry replaces the hook, the ids appear.
func TestTraceIDsAppearWhenTheHookIsSet(t *testing.T) {
	original := logging.TraceIDs
	t.Cleanup(func() { logging.TraceIDs = original })
	logging.TraceIDs = func(context.Context) (string, string) { return "trace-1", "span-1" }

	var buf bytes.Buffer
	log := logging.New(logging.Options{Component: "middleware", Output: &buf})
	log.InfoContext(context.Background(), "outbound call")

	m := decode(t, &buf)
	if m["traceId"] != "trace-1" || m["spanId"] != "span-1" {
		t.Errorf("trace fields = %v, %v", m["traceId"], m["spanId"])
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := logging.New(logging.Options{Component: "cqapi", Level: slog.LevelWarn, Output: &buf})

	log.Info("should not appear")
	if buf.Len() != 0 {
		t.Errorf("info must be filtered at warn level, got %s", buf.String())
	}
	log.Warn("should appear")
	if buf.Len() == 0 {
		t.Error("warn must be emitted at warn level")
	}
}

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug":    slog.LevelDebug,
		"INFO":     slog.LevelInfo,
		" warn ":   slog.LevelWarn,
		"warning":  slog.LevelWarn,
		"error":    slog.LevelError,
		"":         slog.LevelInfo,
		"nonsense": slog.LevelInfo,
	}
	for in, want := range tests {
		if got := logging.ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
