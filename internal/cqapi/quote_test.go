package cqapi_test

import (
	"testing"

	"github.com/aurc/commission-quote-app/internal/cqapi"
	"github.com/aurc/commission-quote-app/internal/platform/money"
)

func TestCommissionRate(t *testing.T) {
	tests := []struct {
		name   string
		band   cqapi.RiskBand
		months int64
		want   string
	}{
		{"band A, minimum term, no adjustment", cqapi.BandA, 6, "0.0100"},
		{"band B, minimum term", cqapi.BandB, 6, "0.0150"},
		{"band C, minimum term", cqapi.BandC, 6, "0.0225"},
		{"one full year earns one step", cqapi.BandA, 12, "0.0105"},
		{"partial years do not count", cqapi.BandA, 23, "0.0105"},
		{"two years", cqapi.BandA, 24, "0.0110"},
		{"the worked example from contract.md", cqapi.BandB, 240, "0.0180"},
		{"cap binds at 72 months", cqapi.BandA, 72, "0.0130"},
		{"cap holds beyond 72 months", cqapi.BandA, 84, "0.0130"},
		{"cap holds at the maximum term", cqapi.BandA, 360, "0.0130"},
		{"cap applies to every band", cqapi.BandC, 360, "0.0255"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cqapi.CommissionRate(tt.band, tt.months)
			if !ok {
				t.Fatalf("band %q should be known", tt.band)
			}
			if got.String() != tt.want {
				t.Errorf("CommissionRate(%q, %d) = %s, want %s", tt.band, tt.months, got, tt.want)
			}
		})
	}
}

func TestUnknownBandIsRejected(t *testing.T) {
	for _, band := range []cqapi.RiskBand{"D", "a", "", "AA"} {
		if _, ok := cqapi.CommissionRate(band, 12); ok {
			t.Errorf("band %q must not be priced", band)
		}
	}
}

// The worked example in contract.md section 2, end to end.
func TestWorkedExample(t *testing.T) {
	amount, err := money.ParseAmount("250000.00")
	if err != nil {
		t.Fatal(err)
	}

	rate, ok := cqapi.CommissionRate(cqapi.BandB, 240)
	if !ok {
		t.Fatal("band B must be priced")
	}
	total := amount.Mul(rate)

	if got := rate.String(); got != "0.0180" {
		t.Errorf("rate = %s, want 0.0180", got)
	}
	if got := total.String(); got != "4500.00" {
		t.Errorf("total = %s, want 4500.00", got)
	}
	// Exact, not merely correct to two places.
	if got := total.Rat().RatString(); got != "4500" {
		t.Errorf("exact total = %s, want 4500", got)
	}
}

func TestGenerateProducesAConsistentQuote(t *testing.T) {
	amount, err := money.ParseAmount("100000.00")
	if err != nil {
		t.Fatal(err)
	}

	q, err := cqapi.Generate(amount, 24, cqapi.BandC)
	if err != nil {
		t.Fatal(err)
	}

	if got := q.CommissionRate.String(); got != "0.0235" {
		t.Errorf("rate = %s, want 0.0235", got)
	}
	if got := q.TotalCommission.String(); got != "2350.00" {
		t.Errorf("total = %s, want 2350.00", got)
	}
}

func TestGenerateRejectsAnUnknownBand(t *testing.T) {
	if _, err := cqapi.Generate(money.FromCents(100000), 12, "Z"); err == nil {
		t.Error("an unknown band must not be priced")
	}
}

func TestQuoteIDIsAUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		q, err := cqapi.Generate(money.FromCents(100000), 12, cqapi.BandA)
		if err != nil {
			t.Fatal(err)
		}
		if len(q.QuoteID) != 36 {
			t.Fatalf("quoteId %q is not a UUID", q.QuoteID)
		}
		if q.QuoteID[14] != '4' {
			t.Errorf("quoteId %q is not version 4", q.QuoteID)
		}
		if v := q.QuoteID[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
			t.Errorf("quoteId %q has the wrong variant", q.QuoteID)
		}
		if seen[q.QuoteID] {
			t.Fatalf("quoteId %q was issued twice", q.QuoteID)
		}
		seen[q.QuoteID] = true
	}
}
