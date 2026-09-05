package bff

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/config"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

// minSigningKeyBytes matches the HS256 digest size.
const minSigningKeyBytes = 32

// Config is the BFF's configuration, per contract.md section 9.
type Config struct {
	Port         int
	LogLevel     string
	OTLPEndpoint string

	MiddlewareBaseURL string
	SigningKey        secrets.Value

	StaffFile       string
	CredentialsFile string

	SessionTTL   time.Duration
	CookieSecure bool

	// TokenTTL is short: the token is minted per request and never leaves the
	// mesh.
	TokenTTL time.Duration
	// RequestTimeout must exceed the Middleware's total budget, so the inner
	// layer reports the specific failure rather than this one reporting a
	// generic timeout. See contract.md section 6.
	RequestTimeout time.Duration
}

// Load reads configuration from the environment, reporting every problem at once.
func Load() (Config, error) {
	l := config.New()
	cfg := Config{
		Port:              l.Port("PORT", 8081),
		LogLevel:          l.String("LOG_LEVEL", "info"),
		OTLPEndpoint:      l.String("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		MiddlewareBaseURL: l.String("MIDDLEWARE_BASE_URL", "http://cqapp-middleware:8082"),
		SigningKey:        l.RequiredSecret("BFF_MIDDLEWARE_SIGNING_KEY"),
		StaffFile:         l.String("STAFF_FILE", "config/staff.csv"),
		CredentialsFile:   l.String("CREDENTIALS_FILE", "config/credentials.csv"),
		SessionTTL:        l.Duration("SESSION_TTL", 30*time.Minute),
		CookieSecure:      l.Bool("SESSION_COOKIE_SECURE", true),
		TokenTTL:          l.Duration("BFF_TOKEN_TTL", time.Minute),
		RequestTimeout:    l.Duration("BFF_REQUEST_TIMEOUT", 8*time.Second),
	}
	if err := l.Err(); err != nil {
		return Config{}, err
	}

	// The bearer crosses the wire to the Middleware. Over plain http to a remote
	// host it is readable on the path, and a stolen bearer is a working
	// credential until it expires.
	if err := checkMiddlewareTransport(cfg.MiddlewareBaseURL); err != nil {
		return Config{}, err
	}
	if n := len(cfg.SigningKey.Reveal()); n < minSigningKeyBytes {
		return Config{}, fmt.Errorf("config BFF_MIDDLEWARE_SIGNING_KEY: must be at least %d bytes for HS256, got %d", minSigningKeyBytes, n)
	}
	return cfg, nil
}

func checkMiddlewareTransport(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("config MIDDLEWARE_BASE_URL: %w", err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLocal(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("config MIDDLEWARE_BASE_URL: refusing to send a bearer token over plain http to %q, use https", u.Host)
	default:
		return fmt.Errorf("config MIDDLEWARE_BASE_URL: scheme must be http or https, got %q", u.Scheme)
	}
}

// isLocal reports whether host is this machine, or a service name on a private
// compose network where the traffic does not leave the host either.
func isLocal(host string) bool {
	if host == "localhost" || host == "cqapp-middleware" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
