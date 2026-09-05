package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/authtoken"
	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

// The token contract lives in platform/authtoken so the BFF and the Middleware
// share one definition without importing each other.
const (
	Issuer             = authtoken.Issuer
	Audience           = authtoken.Audience
	ScopeQuoteGenerate = authtoken.ScopeQuoteGenerate
)

// Claims, Caller and Verifier are the shared token types.
type (
	Claims   = authtoken.Claims
	Caller   = authtoken.Caller
	Verifier = authtoken.Verifier
)

// NewVerifier builds a token verifier.
func NewVerifier(secret secrets.Value, leeway time.Duration) *Verifier {
	return authtoken.NewVerifier(secret, leeway)
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
