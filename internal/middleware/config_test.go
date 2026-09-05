package middleware_test

import (
	"strings"
	"testing"

	"github.com/aurc/commission-quote-app/internal/middleware"
)

// A short HS256 key is accepted by the algorithm but is weaker than the MAC it
// produces, and a short key is what gets invented by hand for a dev environment
// and then quietly promoted.
func TestShortSigningKeyIsRejectedAtStartup(t *testing.T) {
	t.Setenv("CQAPI_API_KEY", "vendor-key")
	t.Setenv("BFF_MIDDLEWARE_SIGNING_KEY", "too-short")

	_, err := middleware.Load()

	if err == nil {
		t.Fatal("a signing key shorter than 32 bytes must fail at startup")
	}
	if !strings.Contains(err.Error(), "BFF_MIDDLEWARE_SIGNING_KEY") {
		t.Errorf("the error must name the variable, got: %v", err)
	}
}

func TestAdequateSigningKeyIsAccepted(t *testing.T) {
	t.Setenv("CQAPI_API_KEY", "vendor-key")
	t.Setenv("BFF_MIDDLEWARE_SIGNING_KEY", strings.Repeat("k", 32))
	// Assert the default, so clear anything a developer's .env may have set.
	t.Setenv("CQAPI_BASE_URL", "")

	cfg, err := middleware.Load()
	if err != nil {
		t.Fatalf("a 32 byte key must be accepted: %v", err)
	}
	if cfg.VendorBaseURL != "http://cqapi-mock:8083" {
		t.Errorf("default vendor URL = %q, must match the compose service name", cfg.VendorBaseURL)
	}
}

// Every missing required variable should be reported from one run.
func TestMissingSecretsAreAllReported(t *testing.T) {
	t.Setenv("CQAPI_API_KEY", "")
	t.Setenv("BFF_MIDDLEWARE_SIGNING_KEY", "")

	_, err := middleware.Load()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, key := range []string{"CQAPI_API_KEY", "BFF_MIDDLEWARE_SIGNING_KEY"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error must name %s, got: %v", key, err)
		}
	}
}
