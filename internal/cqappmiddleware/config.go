package cqappmiddleware

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/config"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

// checkVendorTransport refuses to send the vendor credential in clear to
// anywhere but a local machine.
//
// The api-key is attached to every outbound call. Over plain http to a remote
// host it is readable by anything on the path, and the vendor is by definition
// external. A misconfigured scheme is a quiet, total credential compromise, and
// nothing else in the system would notice, so it is refused at startup.
//
// Loopback is allowed because that is the local development stack, where the
// traffic never leaves the machine.
func checkVendorTransport(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("config CQAPI_BASE_URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLocal(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("config CQAPI_BASE_URL: refusing to send the vendor api-key over plain http to %q, use https", u.Host)
	default:
		return fmt.Errorf("config CQAPI_BASE_URL: scheme must be http or https, got %q", u.Scheme)
	}
}

// isLocal reports whether host is this machine, or a container name on a private
// compose network where the traffic does not leave the host either.
func isLocal(host string) bool {
	if host == "localhost" || host == "cqapi-mock" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// minSigningKeyBytes matches the HS256 digest size. A shorter key is accepted
// by the algorithm but provides less strength than the MAC it produces.
const minSigningKeyBytes = 32

// Config is the Middleware's configuration, per contract.md section 9.
type Config struct {
	Port         int
	LogLevel     string
	OTLPEndpoint string

	// VendorBaseURL is where CQAPI lives.
	VendorBaseURL string
	// VendorAPIKey authenticates us to the vendor. Held here and nowhere else.
	VendorAPIKey secrets.Value
	// SigningKey verifies tokens minted by the BFF.
	SigningKey secrets.Value

	// VendorTimeout bounds one attempt; RequestBudget bounds the whole inbound
	// request. Budgets nest, per contract.md section 6, so the inner layer
	// reports the specific failure rather than the outer reporting a generic
	// timeout.
	VendorTimeout time.Duration
	RequestBudget time.Duration

	// ClockSkew absorbs disagreement between containers on a 60 second token.
	ClockSkew time.Duration

	// Retry and Breaker bound how hard we lean on a failing vendor.
	Retry   RetryConfig
	Breaker BreakerConfig

	// StaffFile is the fixture standing in for the entitlement source. The BFF
	// reads the same file for identity, so the two cannot disagree about who
	// exists.
	StaffFile string
}

// Load reads configuration from the environment, reporting every problem at once.
func Load() (Config, error) {
	l := config.New()
	cfg := Config{
		Port:          l.Port("PORT", 8082),
		LogLevel:      l.String("LOG_LEVEL", "info"),
		OTLPEndpoint:  l.String("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		VendorBaseURL: l.String("CQAPI_BASE_URL", "http://cqapi-mock:8083"),
		VendorAPIKey:  l.RequiredSecret("CQAPI_API_KEY"),
		SigningKey:    l.RequiredSecret("BFF_MIDDLEWARE_SIGNING_KEY"),
		VendorTimeout: l.Duration("MIDDLEWARE_VENDOR_TIMEOUT", 2*time.Second),
		RequestBudget: l.Duration("MIDDLEWARE_REQUEST_BUDGET", 6*time.Second),
		ClockSkew:     l.Duration("MIDDLEWARE_CLOCK_SKEW", 5*time.Second),
		StaffFile:     l.String("STAFF_FILE", "config/staff.csv"),
		Retry: RetryConfig{
			Attempts: l.Int("MIDDLEWARE_RETRY_ATTEMPTS", 3),
			Base:     l.Duration("MIDDLEWARE_RETRY_BASE", 150*time.Millisecond),
			Cap:      l.Duration("MIDDLEWARE_RETRY_CAP", time.Second),
		},
		Breaker: BreakerConfig{
			Window:     l.Int("MIDDLEWARE_BREAKER_WINDOW", 20),
			MinSamples: l.Int("MIDDLEWARE_BREAKER_MIN_SAMPLES", 10),
			Threshold:  l.Rate("MIDDLEWARE_BREAKER_THRESHOLD", 0.5),
			OpenFor:    l.Duration("MIDDLEWARE_BREAKER_OPEN_FOR", 10*time.Second),
			Probes:     l.Int("MIDDLEWARE_BREAKER_PROBES", 3),
		},
	}
	if err := l.Err(); err != nil {
		return Config{}, err
	}
	// An HS256 key shorter than the digest it produces weakens the signature,
	// and a short key is exactly what gets invented by hand for a dev
	// environment and then quietly promoted.
	if cfg.Retry.Attempts < 1 {
		return Config{}, fmt.Errorf("config MIDDLEWARE_RETRY_ATTEMPTS: must be at least 1, got %d", cfg.Retry.Attempts)
	}
	if cfg.Breaker.MinSamples > cfg.Breaker.Window {
		return Config{}, fmt.Errorf("config MIDDLEWARE_BREAKER_MIN_SAMPLES (%d) cannot exceed MIDDLEWARE_BREAKER_WINDOW (%d), the breaker could never trip",
			cfg.Breaker.MinSamples, cfg.Breaker.Window)
	}
	if err := checkVendorTransport(cfg.VendorBaseURL); err != nil {
		return Config{}, err
	}
	if n := len(cfg.SigningKey.Reveal()); n < minSigningKeyBytes {
		return Config{}, fmt.Errorf("config BFF_MIDDLEWARE_SIGNING_KEY: must be at least %d bytes for HS256, got %d", minSigningKeyBytes, n)
	}
	return cfg, nil
}
