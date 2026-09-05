// Command cqapp-bff runs the browser facing service.
//
// It owns the staff session, exchanges it for a bearer claim the Middleware
// accepts, and writes the words a person reads. It holds no business logic and
// no vendor credential.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aurc/commission-quote-app/internal/cqappbff"
	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
)

const component = "cqapp-bff"

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", component, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := cqappbff.Load()
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

	// Identity comes from the same fixture the Middleware reads for entitlement,
	// so sign in and authorisation cannot disagree about who exists.
	staff, err := staffdir.Load(cfg.StaffFile)
	if err != nil {
		return err
	}
	auth, err := cqappbff.NewFixtureAuth(staff, cfg.CredentialsFile)
	if err != nil {
		return err
	}

	sessions := cqappbff.NewSessionStore(cfg.SessionTTL)
	quotes := cqappbff.NewMiddlewareClient(cfg.MiddlewareBaseURL, cfg.SigningKey, cfg.TokenTTL, cfg.RequestTimeout, log)

	log.InfoContext(ctx, "starting bff",
		slog.String("middlewareBaseUrl", cfg.MiddlewareBaseURL),
		slog.String("staffFile", cfg.StaffFile),
		slog.Int("staffCount", len(staff.All())),
		slog.Duration("sessionTtl", cfg.SessionTTL),
		slog.Bool("cookieSecure", cfg.CookieSecure),
	)

	return httpx.Serve(ctx, log, cqappbff.NewRouter(cfg, auth, sessions, quotes, log), httpx.ServeOptions{
		Port:         cfg.Port,
		WriteTimeout: cfg.RequestTimeout + 5*time.Second,
	})
}
