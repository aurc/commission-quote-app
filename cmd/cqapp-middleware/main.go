// Command middleware runs the internal service that orchestrates access to the
// vendor Commission Quote API.
//
// It holds the vendor credential, verifies caller claims, validates
// authoritatively, and translates the vendor's world into ours.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aurc/commission-quote-app/internal/cqappmiddleware"
	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
)

const component = "cqapp-middleware"

func main() {
	// Container images are distroless, so a compose healthcheck has no shell to
	// run. The binary checks itself instead.
	health := flag.Int("health", 0, "check the service on this port and exit")
	flag.Parse()
	if *health != 0 {
		if err := httpx.CheckHealth(*health, 3*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "%s: unhealthy: %v\n", component, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		// Configuration failures happen before the logger exists.
		fmt.Fprintf(os.Stderr, "%s: %v\n", component, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := cqappmiddleware.Load()
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

	// MVP entitlements, read from the committed fixture. Production swaps this
	// for directory groups or a policy decision point behind the same
	// interface; the Middleware does not change.
	staff, err := staffdir.Load(cfg.StaffFile)
	if err != nil {
		return err
	}

	log.InfoContext(ctx, "starting middleware",
		slog.String("staffFile", cfg.StaffFile),
		slog.Int("staffCount", len(staff.All())),
		slog.String("vendorBaseUrl", cfg.VendorBaseURL),
		slog.String("vendorApiKey", cfg.VendorAPIKey.String()),
		slog.Duration("vendorTimeout", cfg.VendorTimeout),
		slog.Duration("requestBudget", cfg.RequestBudget),
	)

	return httpx.Serve(ctx, log, cqappmiddleware.NewRouter(cfg, staff, log), httpx.ServeOptions{
		Port:         cfg.Port,
		WriteTimeout: cfg.RequestBudget + 5*time.Second,
	})
}
