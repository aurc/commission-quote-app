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

// The api-key is attached to every outbound call. Over plain http to a remote
// host it is readable on the path, and the vendor is by definition external. A
// misconfigured scheme would be a quiet, total credential compromise that
// nothing else in the system would notice.
func TestVendorURLMustBeEncryptedOutsideLocalhost(t *testing.T) {
	refused := []string{
		"http://cqapi.vendor.example.com",
		"http://10.0.0.5:8083",
		"http://quotes.internal:8083",
		"ftp://cqapi.vendor.example.com",
	}
	for _, url := range refused {
		t.Run("refused "+url, func(t *testing.T) {
			t.Setenv("CQAPI_API_KEY", "vendor-key")
			t.Setenv("BFF_MIDDLEWARE_SIGNING_KEY", strings.Repeat("k", 32))
			t.Setenv("CQAPI_BASE_URL", url)

			_, err := middleware.Load()
			if err == nil {
				t.Fatalf("%s must be refused: the api-key would cross the network in clear", url)
			}
			if !strings.Contains(err.Error(), "CQAPI_BASE_URL") {
				t.Errorf("the error must name the variable, got: %v", err)
			}
		})
	}

	allowed := []string{
		"https://cqapi.vendor.example.com",
		"http://localhost:8083",
		"http://127.0.0.1:8083",
		"http://cqapi-mock:8083",
	}
	for _, url := range allowed {
		t.Run("allowed "+url, func(t *testing.T) {
			t.Setenv("CQAPI_API_KEY", "vendor-key")
			t.Setenv("BFF_MIDDLEWARE_SIGNING_KEY", strings.Repeat("k", 32))
			t.Setenv("CQAPI_BASE_URL", url)

			if _, err := middleware.Load(); err != nil {
				t.Errorf("%s should be accepted: %v", url, err)
			}
		})
	}
}

func TestBreakerThatCouldNeverTripIsRejected(t *testing.T) {
	t.Setenv("CQAPI_API_KEY", "vendor-key")
	t.Setenv("BFF_MIDDLEWARE_SIGNING_KEY", strings.Repeat("k", 32))
	t.Setenv("MIDDLEWARE_BREAKER_WINDOW", "10")
	t.Setenv("MIDDLEWARE_BREAKER_MIN_SAMPLES", "20")

	_, err := middleware.Load()

	if err == nil {
		t.Fatal("a minimum sample count larger than the window means a breaker that can never trip")
	}
}
