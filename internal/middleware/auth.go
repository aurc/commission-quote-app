package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

// Token identity, per contract.md section 7.
const (
	Issuer   = "cqapp-bff"
	Audience = "cqapp-middleware"

	// signingMethod is pinned. A verifier that accepts whatever the token's
	// header nominates can be handed an unsigned token, or an HMAC token it
	// verifies with a key it believed was for RSA.
	signingMethod = "HS256"
)

// Claims are the claims we read. RegisteredClaims covers iss, aud, exp, iat,
// sub and jti.
type Claims struct {
	jwt.RegisteredClaims
	// Scope is what the caller is asking to do. It is not a grant; see
	// Entitlements.
	Scope []string `json:"scope"`
}

// Caller is a verified identity. Requested holds the scopes the token asked
// for, deliberately named so it cannot be mistaken for what was granted.
type Caller struct {
	Subject   string
	TokenID   string
	Requested []string
}

type callerKey struct{}

// WithCaller returns a context carrying c.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom returns the verified caller on ctx.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

// Verifier checks bearer tokens minted by the BFF.
type Verifier struct {
	key    []byte
	parser *jwt.Parser
}

// NewVerifier builds a Verifier. Leeway absorbs clock skew between containers;
// exp is only 60 seconds, so a few seconds of tolerance is the difference
// between working and failing intermittently.
func NewVerifier(secret secrets.Value, leeway time.Duration) *Verifier {
	return &Verifier{
		key: []byte(secret.Reveal()),
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

var errNoSubject = errors.New("token has no subject")

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
	// anonymous caller and then attribute their request to nobody in the logs.
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" {
		return Caller{}, errNoSubject
	}
	return Caller{
		Subject:   subject,
		TokenID:   claims.ID,
		Requested: claims.Scope,
	}, nil
}

// Authenticate verifies the bearer token and attaches the caller to the request.
//
// Everything that fails here is a 401: the caller has not established who they
// are. Whether they may do anything is a separate question, answered by
// Authorise.
func Authenticate(v *Verifier, log *slog.Logger) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, err := bearerToken(r)
			if err != nil {
				httpx.WriteError(r.Context(), w, log, httpx.Unauthenticated(err))
				return
			}

			caller, err := v.Verify(raw)
			if err != nil {
				// The token itself is never logged. It is a live credential.
				httpx.WriteError(r.Context(), w, log, httpx.Unauthenticated(err))
				return
			}

			next.ServeHTTP(w, r.WithContext(WithCaller(r.Context(), caller)))
		})
	}
}

// Authorise decides whether the verified caller may exercise scope.
//
// Two conditions, both required. The token must request the scope, and the
// Middleware's own Entitlements source must grant it to that subject. The first
// alone would be circular: the BFF writes the scope claim and the BFF is the
// party being checked.
func Authorise(e Entitlements, scope string, log *slog.Logger) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			caller, ok := CallerFrom(ctx)
			if !ok {
				// Authenticate did not run. A wiring mistake, not a caller error.
				httpx.WriteError(ctx, w, log, httpx.Internal(errors.New("authorise ran without authenticate")))
				return
			}

			if !slices.Contains(caller.Requested, scope) {
				log.WarnContext(ctx, "caller did not request the required scope",
					slog.String("staffId", caller.Subject),
					slog.String("required", scope),
				)
				httpx.WriteError(ctx, w, log, httpx.Forbidden(errors.New("scope not requested")))
				return
			}

			granted, err := Allows(ctx, e, caller.Subject, scope)
			if err != nil {
				httpx.WriteError(ctx, w, log, httpx.Internal(err))
				return
			}
			if !granted {
				log.WarnContext(ctx, "caller is not entitled to the requested scope",
					slog.String("staffId", caller.Subject),
					slog.String("required", scope),
				)
				httpx.WriteError(ctx, w, log, httpx.Forbidden(errors.New("scope not granted")))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

var errNoBearer = errors.New("missing or malformed Authorization header")

func bearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	scheme, token, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return "", errNoBearer
	}
	return strings.TrimSpace(token), nil
}
