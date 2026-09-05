package cqapi_test

import (
	"net/http"
	"os"
	"sort"
	"testing"

	"github.com/aurc/commission-quote-app/internal/cqapimock"
	"gopkg.in/yaml.v3"
)

// The published spec and the implementation drift apart silently unless
// something checks. These tests are that check, and are the reason a hand
// written spec is a reasonable choice here rather than code generation.

type spec struct {
	Paths map[string]map[string]struct {
		Responses map[string]any `yaml:"responses"`
	} `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Required   []string `yaml:"required"`
			Properties map[string]struct {
				Enum []string `yaml:"enum"`
			} `yaml:"properties"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func loadSpec(t *testing.T) spec {
	t.Helper()
	b, err := os.ReadFile("../../api/cqapi.openapi.yaml")
	if err != nil {
		t.Fatalf("the vendor spec must be committed: %v", err)
	}
	var s spec
	if err := yaml.Unmarshal(b, &s); err != nil {
		t.Fatalf("the vendor spec must be valid YAML: %v", err)
	}
	return s
}

// Every status the handler can produce must be documented, and nothing may be
// documented that the handler cannot produce.
func TestSpecDocumentsExactlyTheStatusesTheHandlerReturns(t *testing.T) {
	s := loadSpec(t)

	documented := make([]string, 0, 4)
	for code := range s.Paths["/v1/quotes"]["post"].Responses {
		documented = append(documented, code)
	}
	sort.Strings(documented)

	want := []string{"201", "400", "401", "503"}
	if len(documented) != len(want) {
		t.Fatalf("documented responses = %v, want %v", documented, want)
	}
	for i := range want {
		if documented[i] != want[i] {
			t.Fatalf("documented responses = %v, want %v", documented, want)
		}
	}
}

// A band added to the code but not the spec, or the reverse, is a contract break.
func TestSpecRiskBandsMatchTheImplementedBands(t *testing.T) {
	s := loadSpec(t)

	documented := s.Components.Schemas["QuoteRequest"].Properties["riskBand"].Enum
	sort.Strings(documented)

	implemented := []string{string(cqapi.BandA), string(cqapi.BandB), string(cqapi.BandC)}
	sort.Strings(implemented)

	if len(documented) != len(implemented) {
		t.Fatalf("spec bands %v, implemented %v", documented, implemented)
	}
	for i := range implemented {
		if documented[i] != implemented[i] {
			t.Fatalf("spec bands %v, implemented %v", documented, implemented)
		}
	}

	// And the implemented set is exactly what the pricing table knows.
	for _, band := range documented {
		if _, ok := cqapi.CommissionRate(cqapi.RiskBand(band), 12); !ok {
			t.Errorf("spec documents band %q but it cannot be priced", band)
		}
	}
}

// Every field the spec marks required must actually be rejected when absent.
func TestSpecRequiredFieldsAreEnforced(t *testing.T) {
	s := loadSpec(t)

	bodies := map[string]string{
		"loanAmount":       `{"loanTermInMonths":240,"riskBand":"B"}`,
		"loanTermInMonths": `{"loanAmount":250000.00,"riskBand":"B"}`,
		"riskBand":         `{"loanAmount":250000.00,"loanTermInMonths":240}`,
	}

	required := s.Components.Schemas["QuoteRequest"].Required
	if len(required) == 0 {
		t.Fatal("the spec must mark the request fields required")
	}
	for _, field := range required {
		body, ok := bodies[field]
		if !ok {
			t.Fatalf("spec requires %q but this test has no case for it", field)
		}
		if rec := post(t, newServer(t), testKey, body); rec.Code != http.StatusBadRequest {
			t.Errorf("omitting required field %q returned %d, want 400", field, rec.Code)
		}
	}
}
