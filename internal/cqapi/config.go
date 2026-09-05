package cqapi

import (
	"fmt"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/config"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

// Config is the vendor mock's configuration, per contract.md section 9.
type Config struct {
	Port         int
	LogLevel     string
	OTLPEndpoint string

	// APIKey is the key the vendor expects from us.
	APIKey secrets.Value

	// FailureRate and SlowRate drive the simulation the challenge asks for.
	FailureRate float64
	SlowRate    float64
	SlowDelay   time.Duration
	LatencyMin  time.Duration
	LatencyMax  time.Duration

	// Seed fixes the simulation. Zero means seed from the clock.
	Seed int64
}

// Load reads configuration from the environment, reporting every problem at once.
func Load() (Config, error) {
	l := config.New()
	cfg := Config{
		Port:         l.Port("PORT", 8083),
		LogLevel:     l.String("LOG_LEVEL", "info"),
		OTLPEndpoint: l.String("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		APIKey:       l.RequiredSecret("CQAPI_API_KEY"),
		FailureRate:  l.Rate("CQAPI_FAILURE_RATE", 0.15),
		SlowRate:     l.Rate("CQAPI_SLOW_RATE", 0.10),
		SlowDelay:    time.Duration(l.Int("CQAPI_SLOW_MS", 3000)) * time.Millisecond,
		LatencyMin:   time.Duration(l.Int("CQAPI_LATENCY_MIN_MS", 50)) * time.Millisecond,
		LatencyMax:   time.Duration(l.Int("CQAPI_LATENCY_MAX_MS", 400)) * time.Millisecond,
		Seed:         int64(l.Int("CQAPI_RANDOM_SEED", 0)),
	}
	if err := l.Err(); err != nil {
		return Config{}, err
	}
	if cfg.LatencyMin > cfg.LatencyMax {
		return Config{}, fmt.Errorf("config CQAPI_LATENCY_MIN_MS (%v) must not exceed CQAPI_LATENCY_MAX_MS (%v)", cfg.LatencyMin, cfg.LatencyMax)
	}
	return cfg, nil
}
