package authtoken_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/aurc/commission-quote-app/internal/platform/authtoken"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

const key = secrets.Value("test-signing-key-0123456789abcdef")

func verifier(t *testing.T) *authtoken.Verifier {
	t.Helper()
	return authtoken.NewVerifier(key, 5*time.Second)
}

func TestMintedTokenVerifies(t *testing.T) {
	token, err := authtoken.Mint(key, "staff-001", []string{authtoken.ScopeQuoteGenerate}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	caller, err := verifier(t).Verify(token)
	if err != nil {
		t.Fatalf("a freshly minted token must verify: %v", err)
	}
	if caller.Subject != "staff-001" {
		t.Errorf("subject = %q", caller.Subject)
	}
	if len(caller.Requested) != 1 || caller.Requested[0] != authtoken.ScopeQuoteGenerate {
		t.Errorf("requested = %v", caller.Requested)
	}
	if caller.TokenID == "" {
		t.Error("jti must be set, it is what a replay cache would key on")
	}
}

// Every token gets its own id, or a replay cache could never tell two apart.
func TestEachTokenHasAUniqueID(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		token, err := authtoken.Mint(key, "staff-001", nil, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		caller, err := verifier(t).Verify(token)
		if err != nil {
			t.Fatal(err)
		}
		if seen[caller.TokenID] {
			t.Fatalf("jti %q was issued twice", caller.TokenID)
		}
		seen[caller.TokenID] = true
	}
}

// The claim is a request, not a grant, so it is carried through untouched for
// the Middleware to decide on.
func TestScopesAreCarriedNotInterpreted(t *testing.T) {
	token, err := authtoken.Mint(key, "staff-001", []string{"a:b", "c:d"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	caller, err := verifier(t).Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.Requested) != 2 {
		t.Errorf("requested = %v, want both scopes carried", caller.Requested)
	}
}

func TestEmptyScopeIsCarriedAsEmpty(t *testing.T) {
	token, err := authtoken.Mint(key, "staff-001", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	caller, err := verifier(t).Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if len(caller.Requested) != 0 {
		t.Errorf("requested = %v, want none", caller.Requested)
	}
}

// mint builds a token directly, so a test can break exactly one thing.
func mint(t *testing.T, claims authtoken.Claims, method jwt.SigningMethod, signWith any) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString(signWith)
	if err != nil {
		t.Fatalf("build token: %v", err)
	}
	return token
}

func validClaims(mutate func(*authtoken.Claims)) authtoken.Claims {
	now := time.Now()
	c := authtoken.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    authtoken.Issuer,
			Subject:   "staff-001",
			Audience:  jwt.ClaimStrings{authtoken.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
			ID:        "jti-1",
		},
		Scope: []string{authtoken.ScopeQuoteGenerate},
	}
	if mutate != nil {
		mutate(&c)
	}
	return c
}

func TestVerifyRejects(t *testing.T) {
	tests := []struct {
		name  string
		token func(t *testing.T) string
		why   string
	}{
		{"a different signing key", func(t *testing.T) string {
			return mint(t, validClaims(nil), jwt.SigningMethodHS256, []byte("a-completely-different-key-9876543210"))
		}, "anyone could mint tokens otherwise"},

		{"an expired token", func(t *testing.T) string {
			return mint(t, validClaims(func(c *authtoken.Claims) {
				past := time.Now().Add(-2 * time.Hour)
				c.IssuedAt = jwt.NewNumericDate(past)
				c.ExpiresAt = jwt.NewNumericDate(past.Add(time.Minute))
			}), jwt.SigningMethodHS256, []byte(key))
		}, "a leaked token would work forever"},

		{"no expiry at all", func(t *testing.T) string {
			return mint(t, validClaims(func(c *authtoken.Claims) { c.ExpiresAt = nil }),
				jwt.SigningMethodHS256, []byte(key))
		}, "a token without an expiry never stops working"},

		{"another issuer", func(t *testing.T) string {
			return mint(t, validClaims(func(c *authtoken.Claims) { c.Issuer = "someone-else" }),
				jwt.SigningMethodHS256, []byte(key))
		}, "only the BFF mints these"},

		{"another audience", func(t *testing.T) string {
			return mint(t, validClaims(func(c *authtoken.Claims) { c.Audience = jwt.ClaimStrings{"another-service"} }),
				jwt.SigningMethodHS256, []byte(key))
		}, "a token for one service must not open another"},

		{"no subject", func(t *testing.T) string {
			return mint(t, validClaims(func(c *authtoken.Claims) { c.Subject = "" }),
				jwt.SigningMethodHS256, []byte(key))
		}, "an anonymous caller would be attributed to nobody"},

		{"a whitespace subject", func(t *testing.T) string {
			return mint(t, validClaims(func(c *authtoken.Claims) { c.Subject = "   " }),
				jwt.SigningMethodHS256, []byte(key))
		}, "same, and it would log as an empty staff id"},

		{"not a token at all", func(*testing.T) string { return "not.a.jwt" }, ""},
		{"an empty string", func(*testing.T) string { return "" }, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := verifier(t).Verify(tt.token(t)); err == nil {
				t.Errorf("accepted %s: %s", tt.name, tt.why)
			}
		})
	}
}

// A verifier that trusts the token's own choice of algorithm can be handed an
// unsigned token, which is the classic way a JWT check fails open.
func TestAlgorithmIsPinned(t *testing.T) {
	unsigned := mint(t, validClaims(nil), jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType)

	if _, err := verifier(t).Verify(unsigned); err == nil {
		t.Fatal("an alg:none token was accepted")
	}
}

func TestNoSubjectIsReportedDistinctly(t *testing.T) {
	token := mint(t, validClaims(func(c *authtoken.Claims) { c.Subject = "" }),
		jwt.SigningMethodHS256, []byte(key))

	_, err := verifier(t).Verify(token)
	if !errors.Is(err, authtoken.ErrNoSubject) {
		t.Errorf("err = %v, want ErrNoSubject so a caller can tell why", err)
	}
}

// exp is 60 seconds, so containers that disagree by a second must not fail.
func TestClockSkewLeewayIsApplied(t *testing.T) {
	future := time.Now().Add(3 * time.Second)
	token := mint(t, validClaims(func(c *authtoken.Claims) {
		c.IssuedAt = jwt.NewNumericDate(future)
		c.ExpiresAt = jwt.NewNumericDate(future.Add(time.Minute))
	}), jwt.SigningMethodHS256, []byte(key))

	if _, err := authtoken.NewVerifier(key, 5*time.Second).Verify(token); err != nil {
		t.Errorf("a token 3s in the future must pass a 5s leeway: %v", err)
	}
	if _, err := authtoken.NewVerifier(key, 0).Verify(token); err == nil {
		t.Error("with no leeway, a future token should be refused")
	}
}

// The signing key is a credential and must not appear in anything a token
// carries.
func TestTokenDoesNotCarryTheKey(t *testing.T) {
	token, err := authtoken.Mint(key, "staff-001", []string{authtoken.ScopeQuoteGenerate}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(token, key.Reveal()) {
		t.Error("the signing key appeared in the token")
	}
}
