package telemetry_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aurc/commission-quote-app/internal/platform/logging"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
	"go.opentelemetry.io/otel"
)

// With no collector configured, tracing must still work end to end: spans are
// created and context propagates. Only export is disabled.
func TestSpansAndPropagationWorkWithoutAnExporter(t *testing.T) {
	shutdown, err := telemetry.Init(context.Background(), telemetry.Options{ServiceName: "test"})
	if err != nil {
		t.Fatalf("Init with no endpoint must succeed: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	ctx, span := otel.Tracer("test").Start(context.Background(), "outbound")
	defer span.End()

	traceID, spanID := logging.TraceIDs(ctx)
	if traceID == "" || spanID == "" {
		t.Fatal("a started span must expose trace and span ids to the logger")
	}

	// The ids must reach the wire as W3C traceparent.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	otel.GetTextMapPropagator().Inject(ctx, propagationCarrier(req.Header))
	if tp := req.Header.Get("traceparent"); tp == "" {
		t.Error("traceparent must be injected into outbound requests")
	}
}

func TestNoTraceIDsOutsideASpan(t *testing.T) {
	shutdown, err := telemetry.Init(context.Background(), telemetry.Options{ServiceName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	traceID, spanID := logging.TraceIDs(context.Background())
	if traceID != "" || spanID != "" {
		t.Errorf("expected empty ids outside a span, got %q %q", traceID, spanID)
	}
}

func TestInitFailsOnAnUnusableEndpoint(t *testing.T) {
	_, err := telemetry.Init(context.Background(), telemetry.Options{
		ServiceName: "test",
		Endpoint:    "://not a url",
	})
	if err == nil {
		t.Error("a malformed endpoint must fail at startup, not silently drop traces")
	}
}

// propagationCarrier adapts http.Header to the propagator's carrier interface.
type propagationCarrier http.Header

func (c propagationCarrier) Get(key string) string { return http.Header(c).Get(key) }
func (c propagationCarrier) Set(key, value string) { http.Header(c).Set(key, value) }
func (c propagationCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
