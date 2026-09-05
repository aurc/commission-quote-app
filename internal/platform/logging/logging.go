// Package logging provides the structured JSON logger described in
// contract.md section 8: one line per event, carrying component, method, line,
// and the correlation and trace identifiers pulled from the context.
//
// Call sites never pass those identifiers explicitly. They travel on the context
// and are attached by the handler, so a forgotten argument cannot break the join
// between a log line and a trace.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
)

type ctxKey int

const correlationKey ctxKey = iota

// WithCorrelationID returns a context carrying id.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}

// CorrelationID returns the correlation id on ctx, or "".
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey).(string)
	return id
}

// TraceIDs reports the active trace and span for ctx. The telemetry package
// replaces it during Init. Keeping it a hook means this package depends on no
// tracing SDK, so logging works identically whether or not tracing is wired in.
var TraceIDs = func(context.Context) (traceID, spanID string) { return "", "" }

// Options configures New. The zero value logs at info to stdout.
type Options struct {
	// Component names the service, bound once and present on every line.
	Component string
	Level     slog.Level
	Output    io.Writer
}

// New returns a JSON logger for a component.
func New(opts Options) *slog.Logger {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}
	h := slog.NewJSONHandler(out, &slog.HandlerOptions{Level: opts.Level})
	return slog.New(&contextHandler{inner: h}).With(slog.String("component", opts.Component))
}

// ParseLevel maps LOG_LEVEL to a slog level, defaulting to info for anything
// unrecognised so a typo degrades to a sensible default rather than silence.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// contextHandler attaches call site and context fields to every record.
type contextHandler struct{ inner slog.Handler }

func (h *contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{inner: h.inner.WithGroup(name)}
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if method, line, ok := callSite(r.PC); ok {
		r.AddAttrs(slog.String("method", method), slog.Int("line", line))
	}
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlationId", id))
	}
	if traceID, spanID := TraceIDs(ctx); traceID != "" {
		r.AddAttrs(slog.String("traceId", traceID), slog.String("spanId", spanID))
	}
	return h.inner.Handle(ctx, r)
}

// callSite resolves a program counter to a short function name and line. The
// package qualified name is kept and the module path dropped, so a line reads
// httpx.WriteError rather than the full import path.
func callSite(pc uintptr) (string, int, bool) {
	if pc == 0 {
		return "", 0, false
	}
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.Function == "" {
		return "", frame.Line, frame.Line != 0
	}
	name := frame.Function
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name, frame.Line, true
}
