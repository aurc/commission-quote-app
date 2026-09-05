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

// Entitlements reports what a subject is permitted to do.
//
// This is the seam that keeps authorisation out of the caller's hands. The scope
// claim on a token is the caller asking to do something; this is the answer. The
// distinction matters because the BFF mints the token and is also the party being
// checked, so trusting its scope claim would let it decide its own authority.
//
// No staff identifier appears anywhere in this package. The MVP implementation
// is internal/platform/staffdir over config/staff.csv; production is directory
// group membership or a policy decision point behind the same interface.
type Entitlements interface {
	Granted(ctx context.Context, subject string) ([]string, error)
}

// Allows reports whether subject holds scope.
func Allows(ctx context.Context, e Entitlements, subject, scope string) (bool, error) {
	granted, err := e.Granted(ctx, subject)
	if err != nil {
		return false, err
	}
	return slices.Contains(granted, scope), nil
}
