// Package telemetry wires OpenTelemetry tracing, per assumptions.md 2.1.2.
//
// No collector is deployed in the MVP. With OTEL_EXPORTER_OTLP_ENDPOINT unset,
// spans are still created and trace context is still propagated across services;
// they are simply not exported. That keeps the propagation path exercised now, so
// attaching a collector later is configuration rather than code.
package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

// Options configures Init.
type Options struct {
	// ServiceName appears on every span. Use the component name.
	ServiceName string
	// Endpoint is the OTLP HTTP endpoint. Empty means create spans but export
	// nothing, which is the MVP default.
	Endpoint string
}

// Init installs the global tracer provider and propagators, and teaches the
// logging package how to read trace ids so log lines and traces join on one
// value. The returned function flushes and shuts down.
func Init(ctx context.Context, opts Options) (func(context.Context) error, error) {
	// W3C trace context is what crosses the wire between our services, per
	// contract.md section 8. Install it regardless of whether we export.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(opts.ServiceName),
		attribute.String("service.namespace", "cqapp"),
	))
	if err != nil {
		return nil, fmt.Errorf("telemetry resource: %w", err)
	}

	providerOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if opts.Endpoint != "" {
		// The exporter logs a malformed endpoint and carries on with a broken
		// target, which would silently drop every trace. Validate it here so a
		// bad value fails at startup instead, per contract.md section 9.
		if err := validateEndpoint(opts.Endpoint); err != nil {
			return nil, err
		}
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(opts.Endpoint))
		if err != nil {
			return nil, fmt.Errorf("telemetry exporter: %w", err)
		}
		providerOpts = append(providerOpts, sdktrace.WithBatcher(exp))
	}

	tp := sdktrace.NewTracerProvider(providerOpts...)
	otel.SetTracerProvider(tp)

	logging.TraceIDs = traceIDs

	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(ctx)
	}, nil
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("telemetry endpoint %q: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("telemetry endpoint %q: must be an http or https URL", endpoint)
	}
	if u.Host == "" {
		return fmt.Errorf("telemetry endpoint %q: missing host", endpoint)
	}
	return nil
}

// traceIDs reads the active span context. Returning empty strings when no span is
// recording is what keeps the trace fields out of logs in tests and in components
// that never start a span.
func traceIDs(ctx context.Context) (string, string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}

// Middleware instruments inbound requests, extracting any incoming trace context
// so a request keeps one trace across every hop.
func Middleware(component string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, component)
	}
}

// Transport instruments outbound requests, injecting trace context so the callee
// continues the same trace. Wrap the transport of every client we own.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base)
}
