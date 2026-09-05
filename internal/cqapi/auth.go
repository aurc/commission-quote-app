package cqapi

import (
	"crypto/subtle"
	"log/slog"
	"net/http"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

// APIKeyHeader is the header the vendor requires, per the challenge brief.
const APIKeyHeader = "api-key"

// RequireAPIKey rejects any request without the vendor's key.
//
// Missing and wrong are treated identically: 401, empty body, no
// WWW-Authenticate, nothing that tells a caller which of the two happened. The
// comparison is constant time so it does not leak the key by timing either.
//
// This is the vendor's own response shape, deliberately not our error envelope.
// Translating it into ours is the Middleware's job, in CQ-04.
func RequireAPIKey(expected secrets.Value, log *slog.Logger) httpx.Middleware {
	want := []byte(expected.Reveal())
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get(APIKeyHeader)
			if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
				// The supplied value is never logged. It is attacker controlled,
				// and could be a valid credential for some other system.
				log.WarnContext(r.Context(), "rejected request with an invalid api-key",
					slog.Bool("keyPresent", got != ""),
					slog.String("expectedKey", expected.String()),
				)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
