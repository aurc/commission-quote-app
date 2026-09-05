package bff

import "github.com/aurc/commission-quote-app/internal/platform/httpx"

// userMessages maps a Middleware error code to what the browser shows.
//
// This is the BFF's job, per contract.md section 5. The Middleware is an
// internal service with no browser and writes API messages; telling a batch job
// to sign in again would be nonsense. Tone, phrasing and any future localisation
// change here, next to the UI, rather than two hops away.
var userMessages = map[string]string{
	httpx.CodeValidationFailed:    "Check the highlighted fields.",
	httpx.CodeUnauthenticated:     "Your session has expired. Sign in again.",
	httpx.CodeForbidden:           "You do not have access to generate quotes.",
	httpx.CodeUpstreamUnavailable: "Quotes are unavailable right now. Try again shortly.",
	httpx.CodeUpstreamContract:    "Quotes are unavailable right now. Try again shortly.",
	httpx.CodeUpstreamTimeout:     "The quote service took too long. Try again.",
	httpx.CodeCircuitOpen:         "Quotes are paused briefly. Try again in a moment.",
	httpx.CodeInternal:            "Something went wrong. Try again.",
}

// userMessage returns the wording for a code.
//
// An unrecognised code falls back to the internal wording, so a code added to
// the Middleware later can never reach a user as raw API text.
func userMessage(code string) string {
	if m, ok := userMessages[code]; ok {
		return m
	}
	return userMessages[httpx.CodeInternal]
}
