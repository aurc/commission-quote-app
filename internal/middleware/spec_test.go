package middleware_test

import (
	"os"
	"sort"
	"testing"

	"github.com/aurc/commission-quote-app/internal/middleware"
	"gopkg.in/yaml.v3"
)

// A hand written spec is only defensible if something stops it drifting from the
// code. These tests are that guard.

type spec struct {
	Paths map[string]map[string]struct {
		Responses      map[string]any `yaml:"responses"`
		RequiredScopes []string       `yaml:"x-required-scopes"`
	} `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Required   []string `yaml:"required"`
			Properties map[string]struct {
				Enum    []string `yaml:"enum"`
				Minimum *float64 `yaml:"minimum"`
				Maximum *float64 `yaml:"maximum"`
			} `yaml:"properties"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func loadSpec(t *testing.T) spec {
	t.Helper()
	b, err := os.ReadFile("../../api/middleware.openapi.yaml")
	if err != nil {
		t.Fatalf("the middleware spec must be committed: %v", err)
	}
	var s spec
	if err := yaml.Unmarshal(b, &s); err != nil {
		t.Fatalf("the middleware spec must be valid YAML: %v", err)
	}
	return s
}

func TestSpecDocumentsExactlyTheStatusesTheHandlerReturns(t *testing.T) {
	s := loadSpec(t)

	documented := make([]string, 0, 6)
	for code := range s.Paths["/v1/quotes"]["post"].Responses {
		documented = append(documented, code)
	}
	sort.Strings(documented)

	// Every status the handler can produce in CQ-04. 503 UPSTREAM_CIRCUIT_OPEN
	// arrives with the breaker in CQ-05 and is documented then, not before.
	want := []string{"200", "400", "401", "403", "502", "504"}
	if len(documented) != len(want) {
		t.Fatalf("documented = %v, want %v", documented, want)
	}
	for i := range want {
		if documented[i] != want[i] {
			t.Fatalf("documented = %v, want %v", documented, want)
		}
	}
}

// The whole point of publishing the scope: a consumer should learn it from the
// contract, not from a 403.
func TestSpecDeclaresTheScopeThatIsActuallyEnforced(t *testing.T) {
	s := loadSpec(t)

	declared := s.Paths["/v1/quotes"]["post"].RequiredScopes
	if len(declared) != 1 {
		t.Fatalf("expected exactly one declared scope, got %v", declared)
	}
	if declared[0] != middleware.ScopeQuoteGenerate {
		t.Errorf("spec declares %q, code enforces %q", declared[0], middleware.ScopeQuoteGenerate)
	}
}

// The published ranges must be the ones the validator applies.
func TestSpecRangesMatchTheValidator(t *testing.T) {
	s := loadSpec(t)
	props := s.Components.Schemas["QuoteRequest"].Properties

	minAmount, maxAmount := props["loanAmount"].Minimum, props["loanAmount"].Maximum
	if minAmount == nil || maxAmount == nil {
		t.Fatal("loanAmount must publish its range")
	}
	if got, want := *minAmount, 1000.00; got != want {
		t.Errorf("spec loanAmount minimum %v, validator %s", got, middleware.MinAmount)
	}
	if got, want := *maxAmount, 5000000.00; got != want {
		t.Errorf("spec loanAmount maximum %v, validator %s", got, middleware.MaxAmount)
	}

	minTerm, maxTerm := props["loanTermInMonths"].Minimum, props["loanTermInMonths"].Maximum
	if minTerm == nil || maxTerm == nil {
		t.Fatal("loanTermInMonths must publish its range")
	}
	if int64(*minTerm) != middleware.MinMonths {
		t.Errorf("spec term minimum %v, validator %d", *minTerm, middleware.MinMonths)
	}
	if int64(*maxTerm) != middleware.MaxMonths {
		t.Errorf("spec term maximum %v, validator %d", *maxTerm, middleware.MaxMonths)
	}
}

func TestSpecRiskBandsMatchTheValidator(t *testing.T) {
	s := loadSpec(t)

	documented := s.Components.Schemas["QuoteRequest"].Properties["riskBand"].Enum
	sort.Strings(documented)

	implemented := append([]string(nil), middleware.ValidBands...)
	sort.Strings(implemented)

	if len(documented) != len(implemented) {
		t.Fatalf("spec bands %v, validator %v", documented, implemented)
	}
	for i := range implemented {
		if documented[i] != implemented[i] {
			t.Fatalf("spec bands %v, validator %v", documented, implemented)
		}
	}
}
