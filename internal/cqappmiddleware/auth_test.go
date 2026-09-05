package cqappmiddleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/aurc/commission-quote-app/internal/cqappmiddleware"
)

const (
	signingKey = "test-signing-key-0123456789abcdef"
	vendorKey  = "test-vendor-key-abcd"
)

// mint builds a token. Every field is a parameter so a test can break exactly
// one thing and leave the rest valid.
type tokenOpts struct {
	subject   string
	scope     []string
	issuer    string
	audience  string
	expiresIn time.Duration
	issuedAt  time.Time
	method    jwt.SigningMethod
	key       any
	omitExp   bool
}

func mint(t *testing.T, o tokenOpts) string {
	t.Helper()

	if o.subject == "" {
		o.subject = entitledSubject(t)
	}
	if o.scope == nil {
		o.scope = []string{cqappmiddleware.ScopeQuoteGenerate}
	}
	if o.issuer == "" {
		o.issuer = cqappmiddleware.Issuer
	}
	if o.audience == "" {
		o.audience = cqappmiddleware.Audience
	}
	if o.expiresIn == 0 {
		o.expiresIn = time.Minute
	}
	if o.issuedAt.IsZero() {
		o.issuedAt = time.Now()
	}
	if o.method == nil {
		o.method = jwt.SigningMethodHS256
	}
	if o.key == nil {
		o.key = []byte(signingKey)
	}

	claims := cqappmiddleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   o.issuer,
			Subject:  o.subject,
			Audience: jwt.ClaimStrings{o.audience},
			IssuedAt: jwt.NewNumericDate(o.issuedAt),
			ID:       "jti-test-1",
		},
		Scope: o.scope,
	}
	if !o.omitExp {
		claims.ExpiresAt = jwt.NewNumericDate(o.issuedAt.Add(o.expiresIn))
	}

	signed, err := jwt.NewWithClaims(o.method, claims).SignedString(o.key)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return signed
}

func testConfig() cqappmiddleware.Config {
	return cqappmiddleware.Config{
		SigningKey:    signingKey,
		VendorAPIKey:  vendorKey,
		VendorTimeout: 2 * time.Second,
		RequestBudget: 6 * time.Second,
		ClockSkew:     5 * time.Second,
	}
}

// callWith sends a valid body with the supplied Authorization header value.
func callWith(t *testing.T, h http.Handler, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/quotes", strings.NewReader(validRequest))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("response is not the error envelope: %v\n%s", err, rec.Body)
	}
	return env.Error.Code
}

// Everything that fails to establish who the caller is must be a 401.
func TestAuthenticationFailuresAre401(t *testing.T) {
	h := newMiddleware(t, okVendor(t))

	tests := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"empty bearer", "Bearer "},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"no scheme", mint(t, tokenOpts{})},
		{"garbage token", "Bearer not.a.jwt"},
		{"signed with the wrong key", "Bearer " + mint(t, tokenOpts{key: []byte("a-different-signing-key-9876543210")})},
		{"expired", "Bearer " + mint(t, tokenOpts{issuedAt: time.Now().Add(-2 * time.Hour), expiresIn: time.Minute})},
		{"no expiry at all", "Bearer " + mint(t, tokenOpts{omitExp: true})},
		{"wrong issuer", "Bearer " + mint(t, tokenOpts{issuer: "someone-else"})},
		{"wrong audience", "Bearer " + mint(t, tokenOpts{audience: "another-service"})},
		{"no subject", "Bearer " + mint(t, tokenOpts{subject: " "})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := callWith(t, h, tt.header)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := errorCode(t, rec); got != "UNAUTHENTICATED" {
				t.Errorf("code = %q, want UNAUTHENTICATED", got)
			}
		})
	}
}

// A verifier that trusts the token's choice of algorithm can be handed an
// unsigned token.
func TestAlgorithmIsPinned(t *testing.T) {
	h := newMiddleware(t, okVendor(t))

	claims := jwt.MapClaims{
		"iss":   cqappmiddleware.Issuer,
		"aud":   cqappmiddleware.Audience,
		"sub":   entitledSubject(t),
		"scope": []string{cqappmiddleware.ScopeQuoteGenerate},
		"exp":   time.Now().Add(time.Minute).Unix(),
	}
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	rec := callWith(t, h, "Bearer "+unsigned)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an alg:none token was accepted, status = %d", rec.Code)
	}
}

// A valid token whose subject holds no grant must be refused, and refused as
// 403: signing in again would not help.
func TestEntitlementIsDecidedByTheMiddlewareNotTheToken(t *testing.T) {
	h := newMiddleware(t, okVendor(t))

	tests := []struct {
		name  string
		token tokenOpts
	}{
		{
			// The forged case. The caller writes itself the scope; the
			// Middleware's own source does not grant it.
			name:  "subject not entitled, even though the token claims the scope",
			token: tokenOpts{subject: unentitledSubject(t)},
		},
		{
			name:  "unknown subject",
			token: tokenOpts{subject: "staff-does-not-exist"},
		},
		{
			name:  "entitled subject that did not request the scope",
			token: tokenOpts{scope: []string{"something:else"}},
		},
		{
			name:  "entitled subject with no scope claim at all",
			token: tokenOpts{scope: []string{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := callWith(t, h, "Bearer "+mint(t, tt.token))

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if got := errorCode(t, rec); got != "FORBIDDEN" {
				t.Errorf("code = %q, want FORBIDDEN", got)
			}
		})
	}
}

func TestEntitledCallerProceeds(t *testing.T) {
	h := newMiddleware(t, okVendor(t))

	rec := callWith(t, h, "Bearer "+mint(t, tokenOpts{}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}

// A token issued slightly in the future must not fail: two containers need not
// agree to the millisecond.
func TestClockSkewIsTolerated(t *testing.T) {
	h := newMiddleware(t, okVendor(t))

	token := mint(t, tokenOpts{issuedAt: time.Now().Add(2 * time.Second)})

	if rec := callWith(t, h, "Bearer "+token); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 within the skew allowance: %s", rec.Code, rec.Body)
	}
}

// The token is a live credential and must never reach a log.
func TestTokenIsNeverLogged(t *testing.T) {
	var logs testBuffer
	h := newMiddlewareWithLogs(t, okVendor(t), &logs)

	token := mint(t, tokenOpts{key: []byte("a-different-signing-key-9876543210")})
	callWith(t, h, "Bearer "+token)

	if logs.Contains(token) {
		t.Error("the bearer token appeared in the logs")
	}
}

func TestHealthNeedsNoToken(t *testing.T) {
	h := newMiddleware(t, okVendor(t))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
