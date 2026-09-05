package cqappmiddleware_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/aurc/commission-quote-app/internal/cqappmiddleware"
	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

// The fixture is the only place a staff identifier appears. These tests read it
// rather than asserting against constants, so editing config/staff.csv cannot
// leave them passing against something the service no longer does.
func TestFixtureSupportsBothAuthorisationOutcomes(t *testing.T) {
	all := staffFixture(t).All()

	var entitled, unentitled int
	for _, s := range all {
		if slices.Contains(s.Scopes, cqappmiddleware.ScopeQuoteGenerate) {
			entitled++
		} else {
			unentitled++
		}
	}

	if entitled == 0 {
		t.Errorf("the fixture needs a staff member holding %s", cqappmiddleware.ScopeQuoteGenerate)
	}
	if unentitled == 0 {
		t.Error("the fixture needs a staff member holding nothing, or the 403 path is not exercised")
	}
}

// The directory satisfies the interface the Middleware authorises against, so
// the running service and the tests exercise the same code path.
func TestDirectorySatisfiesEntitlements(t *testing.T) {
	var e cqappmiddleware.Entitlements = staffFixture(t)
	ctx := context.Background()

	granted, err := cqappmiddleware.Allows(ctx, e, entitledSubject(t), cqappmiddleware.ScopeQuoteGenerate)
	if err != nil {
		t.Fatal(err)
	}
	if !granted {
		t.Error("the entitled subject must be granted the scope")
	}

	granted, err = cqappmiddleware.Allows(ctx, e, unentitledSubject(t), cqappmiddleware.ScopeQuoteGenerate)
	if err != nil {
		t.Fatal(err)
	}
	if granted {
		t.Error("the unentitled subject must not be granted the scope")
	}
}

// Not being in the directory is a valid answer to "what may they do", and must
// be a refusal rather than an error.
func TestUnknownSubjectIsRefusedNotAnError(t *testing.T) {
	granted, err := cqappmiddleware.Allows(context.Background(), staffFixture(t),
		"staff-not-in-the-fixture", cqappmiddleware.ScopeQuoteGenerate)

	if err != nil {
		t.Fatalf("an unknown subject must not be an error: %v", err)
	}
	if granted {
		t.Error("an unknown subject must not be granted anything")
	}
}

// Authorisation reads from the source, so a change there changes the answer
// without touching code. This is what the fixture buys.
func TestEntitlementFollowsTheSource(t *testing.T) {
	dir, err := staffdir.Parse(strings.NewReader(
		"id,name,scopes\n" +
			"someone,Someone,quote:generate\n" +
			"nobody,Nobody,\n"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if granted, _ := cqappmiddleware.Allows(ctx, dir, "someone", cqappmiddleware.ScopeQuoteGenerate); !granted {
		t.Error("a subject granted the scope in the source must be allowed")
	}
	if granted, _ := cqappmiddleware.Allows(ctx, dir, "nobody", cqappmiddleware.ScopeQuoteGenerate); granted {
		t.Error("a subject without the scope in the source must be refused")
	}
}

// An Entitlements source that fails must not be read as a grant.
func TestEntitlementSourceFailureIsNotAGrant(t *testing.T) {
	granted, err := cqappmiddleware.Allows(context.Background(), failingEntitlements{}, "anyone", cqappmiddleware.ScopeQuoteGenerate)

	if err == nil {
		t.Fatal("the error must be reported")
	}
	if granted {
		t.Error("a failing source must never grant")
	}
}

type failingEntitlements struct{}

func (failingEntitlements) Granted(context.Context, string) ([]string, error) {
	return nil, context.DeadlineExceeded
}
