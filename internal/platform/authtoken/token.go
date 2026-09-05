// Package authtoken holds the token contract between the BFF and the Middleware,
// per contract.md section 7.
//
// It lives in platform so that neither service imports the other. They deploy
// separately and must agree on the issuer, audience, algorithm and claim shape;
// a shared definition makes disagreement impossible rather than merely tested
// for.
package authtoken

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

const (
	// Issuer is the BFF, the only component that mints these.
	Issuer = "cqapp-bff"
	// Audience is the Middleware, the only component that accepts them.
	Audience = "cqapp-middleware"
	// ScopeQuoteGenerate is requested by the caller and granted, or not, by the
	// Middleware. It is a request, never a grant.
	ScopeQuoteGenerate = "quote:generate"

	// signingMethod is pinned. A verifier that accepts whatever the token's
	// header nominates can be handed an unsigned token, or an HMAC token it
	// verifies with a key it believed was for RSA.
	signingMethod = "HS256"
)

// Claims are the claims both sides agree on.
type Claims struct {
	jwt.RegisteredClaims
	// Scope is what the caller is asking to do. It is not a grant.
	Scope []string `json:"scope"`
}

// Caller is a verified identity. Requested is named so it cannot be mistaken for
// what was granted.
type Caller struct {
	Subject   string
	TokenID   string
	Requested []string
}

// ErrNoSubject reports a token that identifies nobody.
var ErrNoSubject = errors.New("token has no subject")

// Mint issues a token for a subject. TTL is short because the token is minted
// per request and never leaves the mesh.
func Mint(key secrets.Value, subject string, scopes []string, ttl time.Duration) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	now := time.Now()

	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Subject:   subject,
			Audience:  jwt.ClaimStrings{Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        id,
		},
		Scope: scopes,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key.Reveal()))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// Verifier checks tokens.
type Verifier struct {
	key    []byte
	parser *jwt.Parser
}

// NewVerifier builds a Verifier. Leeway absorbs clock skew between containers,
// which matters on a token that lives for a minute.
func NewVerifier(key secrets.Value, leeway time.Duration) *Verifier {
	return &Verifier{
		key: []byte(key.Reveal()),
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{signingMethod}),
			jwt.WithIssuer(Issuer),
			jwt.WithAudience(Audience),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(leeway),
			jwt.WithIssuedAt(),
		),
	}
}

// Verify parses and validates a token, returning the caller it identifies.
func (v *Verifier) Verify(raw string) (Caller, error) {
	var claims Claims
	if _, err := v.parser.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) {
		return v.key, nil
	}); err != nil {
		return Caller{}, err
	}

	// A blank or whitespace subject is a failure to establish identity, not an
	// identity with no entitlements. Letting it through would authenticate an
	// anonymous caller and attribute their request to nobody.
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return Caller{}, ErrNoSubject
	}

	return Caller{Subject: subject, TokenID: claims.ID, Requested: claims.Scope}, nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
