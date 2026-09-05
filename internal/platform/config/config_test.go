package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/config"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

func TestDefaultsApplyWhenUnset(t *testing.T) {
	l := config.NewFrom(secrets.MapProvider{})

	if got := l.String("HOST", "localhost"); got != "localhost" {
		t.Errorf("String default = %q", got)
	}
	if got := l.Int("RETRIES", 3); got != 3 {
		t.Errorf("Int default = %d", got)
	}
	if got := l.Duration("TIMEOUT", 2*time.Second); got != 2*time.Second {
		t.Errorf("Duration default = %v", got)
	}
	if got := l.Rate("FAILURE_RATE", 0.15); got != 0.15 {
		t.Errorf("Rate default = %v", got)
	}
	if err := l.Err(); err != nil {
		t.Errorf("defaults must not produce errors: %v", err)
	}
}

func TestValuesOverrideDefaults(t *testing.T) {
	l := config.NewFrom(secrets.MapProvider{
		"HOST":    "cqapi",
		"RETRIES": "5",
		"TIMEOUT": "150ms",
	})

	if got := l.String("HOST", "localhost"); got != "cqapi" {
		t.Errorf("String = %q", got)
	}
	if got := l.Int("RETRIES", 3); got != 5 {
		t.Errorf("Int = %d", got)
	}
	if got := l.Duration("TIMEOUT", time.Second); got != 150*time.Millisecond {
		t.Errorf("Duration = %v", got)
	}
	if err := l.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// An operator with several missing keys should learn all of them from one run.
func TestErrorsAreCollectedNotFirstFailureWins(t *testing.T) {
	l := config.NewFrom(secrets.MapProvider{})
	l.RequiredString("CQAPI_API_KEY")
	l.RequiredString("BFF_MIDDLEWARE_SIGNING_KEY")

	err := l.Err()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, key := range []string{"CQAPI_API_KEY", "BFF_MIDDLEWARE_SIGNING_KEY"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error must name %s, got: %v", key, err)
		}
	}
}

// A malformed value must fail loudly rather than silently becoming the default.
func TestMalformedValuesAreErrorsNotSilentDefaults(t *testing.T) {
	tests := []struct {
		name string
		load func(*config.Loader)
		env  secrets.MapProvider
	}{
		{"int", func(l *config.Loader) { l.Int("RETRIES", 3) }, secrets.MapProvider{"RETRIES": "many"}},
		{"duration", func(l *config.Loader) { l.Duration("TIMEOUT", time.Second) }, secrets.MapProvider{"TIMEOUT": "2 seconds"}},
		{"float", func(l *config.Loader) { l.Float("RATE", 0.1) }, secrets.MapProvider{"RATE": "high"}},
		{"rate above one", func(l *config.Loader) { l.Rate("RATE", 0.1) }, secrets.MapProvider{"RATE": "1.5"}},
		{"rate below zero", func(l *config.Loader) { l.Rate("RATE", 0.1) }, secrets.MapProvider{"RATE": "-0.2"}},
		{"port out of range", func(l *config.Loader) { l.Port("PORT", 8080) }, secrets.MapProvider{"PORT": "70000"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := config.NewFrom(tt.env)
			tt.load(l)
			if l.Err() == nil {
				t.Error("expected an error for a malformed value")
			}
		})
	}
}

func TestRequiredSecretIsMaskedByDefault(t *testing.T) {
	l := config.NewFrom(secrets.MapProvider{"CQAPI_API_KEY": "vendor-key-abcd"})
	key := l.RequiredSecret("CQAPI_API_KEY")

	if err := l.Err(); err != nil {
		t.Fatal(err)
	}
	if key.String() != "****abcd" {
		t.Errorf("secret must be masked when formatted, got %q", key.String())
	}
	if key.Reveal() != "vendor-key-abcd" {
		t.Error("Reveal must return the real value")
	}
}
