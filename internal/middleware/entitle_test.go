package middleware_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/aurc/commission-quote-app/internal/middleware"
	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

// The tests use an in memory table, but the service reads config/staff.csv.
// If the fixture is edited so that staff-001 loses the scope or staff-002 gains
// it, the entitlement tests would keep passing against a table that no longer
// reflects reality. This is the test that catches that.
func TestFixtureAgreesWithTheSeedConstants(t *testing.T) {
	d, err := staffdir.Load(filepath.Join("..", "..", "config", "staff.csv"))
	if err != nil {
		t.Fatalf("config/staff.csv must load: %v", err)
	}
	ctx := context.Background()

	entitled, err := d.Granted(ctx, middleware.SeedEntitledStaff)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(entitled, middleware.ScopeQuoteGenerate) {
		t.Errorf("%s must hold %s in the fixture, has %v",
			middleware.SeedEntitledStaff, middleware.ScopeQuoteGenerate, entitled)
	}

	unentitled, err := d.Granted(ctx, middleware.SeedUnentitledStaff)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(unentitled, middleware.ScopeQuoteGenerate) {
		t.Errorf("%s must not hold %s, or the 403 path is not being exercised",
			middleware.SeedUnentitledStaff, middleware.ScopeQuoteGenerate)
	}
}

// The directory satisfies the interface the Middleware authorises against, so
// the running service and the tests exercise the same code path.
func TestDirectorySatisfiesEntitlements(t *testing.T) {
	d, err := staffdir.Load(filepath.Join("..", "..", "config", "staff.csv"))
	if err != nil {
		t.Fatal(err)
	}

	var e middleware.Entitlements = d

	granted, err := middleware.Allows(context.Background(), e, middleware.SeedEntitledStaff, middleware.ScopeQuoteGenerate)
	if err != nil {
		t.Fatal(err)
	}
	if !granted {
		t.Error("the fixture backed directory must grant the entitled subject")
	}
}
