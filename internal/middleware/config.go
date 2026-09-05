package middleware

import (
	"fmt"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/config"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

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
	}
	if err := l.Err(); err != nil {
		return Config{}, err
	}
	// An HS256 key shorter than the digest it produces weakens the signature,
	// and a short key is exactly what gets invented by hand for a dev
	// environment and then quietly promoted.
	if n := len(cfg.SigningKey.Reveal()); n < minSigningKeyBytes {
		return Config{}, fmt.Errorf("config BFF_MIDDLEWARE_SIGNING_KEY: must be at least %d bytes for HS256, got %d", minSigningKeyBytes, n)
	}
	return cfg, nil
}
