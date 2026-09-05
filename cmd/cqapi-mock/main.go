// Command cqapi runs the mocked external vendor Commission Quote API.
//
// It stands in for the vendor system the challenge states is under construction.
// It is not part of the application we are building; it is the thing our
// Middleware has to survive.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aurc/commission-quote-app/internal/cqapimock"
	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
)

const component = "cqapi-mock"

func main() {
	if err := run(context.Background()); err != nil {
		// Configuration failures happen before the logger exists, so this goes
		// to stderr. Failing loudly at startup is the contract.md section 9 rule.
		fmt.Fprintf(os.Stderr, "%s: %v\n", component, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := cqapi.Load()
	if err != nil {
		return err
	}

	log := logging.New(logging.Options{
		Component: component,
		Level:     logging.ParseLevel(cfg.LogLevel),
	})

	shutdownTelemetry, err := telemetry.Init(ctx, telemetry.Options{
		ServiceName: component,
		Endpoint:    cfg.OTLPEndpoint,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := shutdownTelemetry(context.Background()); err != nil {
			log.Error("telemetry shutdown failed", slog.String("cause", err.Error()))
		}
	}()

	log.InfoContext(ctx, "starting vendor mock",
		slog.String("apiKey", cfg.APIKey.String()),
		slog.Float64("failureRate", cfg.FailureRate),
		slog.Float64("slowRate", cfg.SlowRate),
		slog.Int64("seed", cfg.Seed),
	)

	return httpx.Serve(ctx, log, cqapi.NewRouter(cfg, log), httpx.ServeOptions{
		Port: cfg.Port,
		// Generous, because this service is deliberately slow sometimes.
		WriteTimeout: cfg.SlowDelay + 10*time.Second,
	})
}
