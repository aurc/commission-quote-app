package cqapi_test

import (
	"encoding/json"
	"testing"

	"github.com/aurc/commission-quote-app/internal/cqapi"
)

func TestCommissionRate(t *testing.T) {
	tests := []struct {
		name   string
		band   cqapi.RiskBand
		months int64
		want   cqapi.Rate
	}{
		{"band A, minimum term, no adjustment", cqapi.BandA, 6, 100},
		{"band B, minimum term", cqapi.BandB, 6, 150},
		{"band C, minimum term", cqapi.BandC, 6, 225},
		{"one full year earns one step", cqapi.BandA, 12, 105},
		{"partial years do not count", cqapi.BandA, 23, 105},
		{"two years", cqapi.BandA, 24, 110},
		{"the worked example from contract.md", cqapi.BandB, 240, 180},
		{"cap binds at 72 months", cqapi.BandA, 72, 130},
		{"cap holds beyond 72 months", cqapi.BandA, 84, 130},
		{"cap holds at the maximum term", cqapi.BandA, 360, 130},
		{"cap applies to every band", cqapi.BandC, 360, 255},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cqapi.CommissionRate(tt.band, tt.months)
			if !ok {
				t.Fatalf("band %q should be known", tt.band)
			}
			if got != tt.want {
				t.Errorf("CommissionRate(%q, %d) = %v (%.4f), want %v", tt.band, tt.months, got, got.Float(), tt.want)
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
	amount, err := cqapi.ParseCents("250000.00")
	if err != nil {
		t.Fatal(err)
	}
	rate, _ := cqapi.CommissionRate(cqapi.BandB, 240)
	total := cqapi.TotalCommission(amount, rate)

	if rate != 180 {
		t.Errorf("rate = %v, want 0.0180", rate)
	}
	if total != 450000 {
		t.Errorf("total = %v, want 4500.00", total)
	}
}

// The reason money is not float64: this product is 4500.000000000001 in binary
// floating point, and errors of that shape accumulate into real money.
func TestTotalCommissionIsExactWhereFloatWouldDrift(t *testing.T) {
	amount, _ := cqapi.ParseCents("250000.00")
	got := cqapi.TotalCommission(amount, 180)

	if got != 450000 {
		t.Fatalf("total = %d cents, want 450000", got)
	}
	if drifted := 250000.00 * 0.0180; drifted == 4500.00 {
		t.Skip("this platform's float64 does not drift here, the integer path is still the safe one")
	}
}

func TestTotalCommissionRounding(t *testing.T) {
	tests := []struct {
		name   string
		amount cqapi.Cents
		rate   cqapi.Rate
		want   cqapi.Cents
	}{
		{"exact", 100000, 100, 1000},          // 1000.00 at 0.0100 = 10.00
		{"rounds half up", 100050, 100, 1001}, // 10.005 -> 10.01
		{"rounds down below half", 100040, 100, 1000},
		{"smallest amount", 1, 100, 0}, // 0.01 at 0.0100 = 0.0001 -> 0.00
		{"maximum amount at maximum rate", 500000000, 255, 12750000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cqapi.TotalCommission(tt.amount, tt.rate); got != tt.want {
				t.Errorf("TotalCommission(%d, %d) = %d, want %d", tt.amount, tt.rate, got, tt.want)
			}
		})
	}
}

func TestParseCents(t *testing.T) {
	valid := map[string]cqapi.Cents{
		"1000":       100000,
		"1000.00":    100000,
		"1000.5":     100050,
		"1000.50":    100050,
		"0.01":       1,
		"5000000.00": 500000000,
		"+1000.00":   100000,
		"-1000.00":   -100000,
	}
	for in, want := range valid {
		t.Run("valid "+in, func(t *testing.T) {
			got, err := cqapi.ParseCents(in)
			if err != nil {
				t.Fatalf("ParseCents(%q) errored: %v", in, err)
			}
			if got != want {
				t.Errorf("ParseCents(%q) = %d, want %d", in, got, want)
			}
		})
	}

	invalid := []string{
		"999.999", // three decimals, the case a float64 would silently swallow
		"1e9",     // exponent notation is not a plain decimal amount
		"1_000",
		"abc",
		"",
		"1000.",
		".50",
		"1000.00.00",
		"1 000",
	}
	for _, in := range invalid {
		t.Run("invalid "+in, func(t *testing.T) {
			if _, err := cqapi.ParseCents(in); err == nil {
				t.Errorf("ParseCents(%q) should have failed", in)
			}
		})
	}
}

// The wire format has to look like money, not like a float.
func TestJSONRendering(t *testing.T) {
	q := cqapi.Quote{QuoteID: "id", CommissionRate: 180, TotalCommission: 450000}

	b, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"quoteId":"id","commissionRate":0.0180,"totalCommission":4500.00}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

func TestQuoteIDIsAUUIDv4(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		q, err := cqapi.Generate(100000, 12, cqapi.BandA)
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
