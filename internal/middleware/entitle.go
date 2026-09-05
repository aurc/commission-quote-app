// Package middleware is the internal service that orchestrates access to the
// vendor Commission Quote API.
//
// It is the only component that holds the vendor credential and the only one
// that decides whether a caller may generate a quote.
package middleware

import (
	"context"
	"slices"
)

// ScopeQuoteGenerate is the scope required to generate a quote. It is published
// in api/middleware.openapi.yaml so a consumer learns the requirement from the
// contract rather than by being refused.
const ScopeQuoteGenerate = "quote:generate"

// Entitlements reports what a subject is permitted to do.
//
// This is the seam that keeps authorisation out of the caller's hands. The scope
// claim on a token is the caller asking to do something; this is the answer. The
// distinction matters because the BFF mints the token and is also the party being
// checked, so trusting its scope claim would let it decide its own authority.
//
// MVP: an in memory table. Production: directory group membership or a policy
// decision point behind the same interface.
type Entitlements interface {
	Granted(ctx context.Context, subject string) ([]string, error)
}

// StaticEntitlements is an in memory grant table.
type StaticEntitlements map[string][]string

// Granted returns the scopes held by subject.
func (s StaticEntitlements) Granted(_ context.Context, subject string) ([]string, error) {
	return s[subject], nil
}

// Allows reports whether subject holds scope.
func Allows(ctx context.Context, e Entitlements, subject, scope string) (bool, error) {
	granted, err := e.Granted(ctx, subject)
	if err != nil {
		return false, err
	}
	return slices.Contains(granted, scope), nil
}

// Subjects in the committed staff fixture, config/staff.csv. Named here so
// tests do not hard code strings that live in a file, and so a fixture edit that
// removes them fails loudly rather than quietly weakening the tests.
//
// Two of them, deliberately: with only an entitled user the 403 path would be
// unreachable and untestable, which is the same flaw that made the original
// scope check circular.
const (
	// SeedEntitledStaff may generate quotes.
	SeedEntitledStaff = "staff-001"
	// SeedUnentitledStaff is authenticated but holds no grant.
	SeedUnentitledStaff = "staff-002"
)

// DefaultEntitlements returns an in memory grant table matching the committed
// fixture. Used by tests; the running service loads config/staff.csv through
// staffdir so that it and the BFF cannot disagree about who exists.
func DefaultEntitlements() StaticEntitlements {
	return StaticEntitlements{
		SeedEntitledStaff:   {ScopeQuoteGenerate},
		SeedUnentitledStaff: {},
	}
}
