package money_test

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/aurc/commission-quote-app/internal/platform/money"
)

func mustAmount(t *testing.T, s string) money.Amount {
	t.Helper()
	a, err := money.ParseAmount(s)
	if err != nil {
		t.Fatalf("ParseAmount(%q): %v", s, err)
	}
	return a
}

func TestParseAmountExactness(t *testing.T) {
	valid := map[string]string{
		"1000":                 "1000",
		"1000.00":              "1000",
		"1000.5":               "2001/2",
		"0.01":                 "1/100",
		"5000000.00":           "5000000",
		"+1000.00":             "1000",
		"-1000.00":             "-1000",
		"999.999":              "999999/1000",
		"0.005":                "1/200",
		"123456789012345678.9": "1234567890123456789/10",
	}
	for in, wantRat := range valid {
		t.Run(in, func(t *testing.T) {
			a := mustAmount(t, in)
			if got := a.Rat().RatString(); got != wantRat {
				t.Errorf("ParseAmount(%q) = %s, want %s", in, got, wantRat)
			}
		})
	}
}

func TestParseAmountRejectsNonDecimals(t *testing.T) {
	invalid := []string{
		"", "abc", "1e9", "1E9", "1_000", "1000.", ".50", "1000.00.00", "1 000",
		"0x10", "NaN", "Inf", "--1", "1-0",
		"1234567890123456789012345678901234567890", // beyond the length guard
	}
	for _, in := range invalid {
		t.Run(in, func(t *testing.T) {
			if _, err := money.ParseAmount(in); err == nil {
				t.Errorf("ParseAmount(%q) should have failed", in)
			}
		})
	}
}

// Precision is a business rule, not a parsing rule. The parser records what it
// saw so the vendor and the Middleware can enforce different limits.
func TestDecimalPlacesAreRecordedNotEnforced(t *testing.T) {
	tests := map[string]int{
		"1000":    0,
		"1000.5":  1,
		"1000.50": 2,
		"999.999": 3,
	}
	for in, want := range tests {
		if got := mustAmount(t, in).DecimalPlaces(); got != want {
			t.Errorf("DecimalPlaces(%q) = %d, want %d", in, got, want)
		}
	}
}

// The reason this package exists.
func TestMultiplicationIsExactWhereFloatWouldDrift(t *testing.T) {
	amount := mustAmount(t, "250000.00")
	rate := money.RateFromTenThousandths(180) // 0.0180

	total := amount.Mul(rate)

	if got := total.Rat().RatString(); got != "4500" {
		t.Errorf("exact product = %s, want 4500", got)
	}
	if got := total.String(); got != "4500.00" {
		t.Errorf("rendered = %s, want 4500.00", got)
	}

	// The same calculation in float64, for contrast.
	if f := 250000.00 * 0.0180; f == 4500.00 {
		t.Log("float64 happens not to drift on this platform; the exact path is still the safe one")
	} else {
		t.Logf("float64 gives %.17f, which is why arithmetic here is exact", f)
	}
}

// Nothing rounds until a value reaches a boundary, so a chain of operations
// cannot accumulate rounding error.
func TestNoRoundingUntilTheBoundary(t *testing.T) {
	third, err := money.ParseAmount("100")
	if err != nil {
		t.Fatal(err)
	}
	// 100 * 0.0001 applied is exact; the intermediate keeps full precision.
	tiny := money.RateFromTenThousandths(1)
	product := third.Mul(tiny)

	if got := product.Rat().RatString(); got != "1/100" {
		t.Fatalf("intermediate = %s, want 1/100 held exactly", got)
	}
	if got := product.String(); got != "0.01" {
		t.Errorf("boundary = %s, want 0.01", got)
	}
}

func TestRoundHalfUp(t *testing.T) {
	tests := []struct {
		in     string
		places int
		want   string
	}{
		{"10.004", 2, "10.00"},
		{"10.005", 2, "10.01"}, // the tie, rounded up
		{"10.006", 2, "10.01"},
		{"0.00005", 4, "0.0001"},
		{"0.00004", 4, "0.0000"},
		{"4500", 2, "4500.00"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r, ok := new(big.Rat).SetString(tt.in)
			if !ok {
				t.Fatalf("bad test input %q", tt.in)
			}
			if got := money.RoundHalfUp(r, tt.places).FloatString(tt.places); got != tt.want {
				t.Errorf("RoundHalfUp(%s, %d) = %s, want %s", tt.in, tt.places, got, tt.want)
			}
		})
	}
}

// Boundaries are integers, never floats.
func TestIntegerBoundaries(t *testing.T) {
	tests := map[string]int64{
		"4500.00": 450000,
		"0.01":    1,
		"10.005":  1001, // half up at the boundary
		"10.004":  1000,
		"0.001":   0,
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			cents, ok := mustAmount(t, in).Cents()
			if !ok {
				t.Fatalf("%q should fit in an int64", in)
			}
			if cents != want {
				t.Errorf("Cents(%q) = %d, want %d", in, cents, want)
			}
		})
	}

	if got := money.FromCents(450000).String(); got != "4500.00" {
		t.Errorf("FromCents round trip = %s", got)
	}
}

// An amount arrives from the network before it is range checked, so it can be
// arbitrarily large. Overflow must be reported, never wrapped silently.
func TestOversizedAmountDoesNotOverflowSilently(t *testing.T) {
	huge := mustAmount(t, "99999999999999999999.99")

	if _, ok := huge.Cents(); ok {
		t.Error("an amount beyond int64 cents must report that it does not fit")
	}
	// It is still exact and still comparable, which is what range validation needs.
	if huge.Cmp(mustAmount(t, "5000000.00")) <= 0 {
		t.Error("comparison must work on values too large for int64")
	}
}

func TestRateRendering(t *testing.T) {
	tests := map[int64]string{
		100: "0.0100",
		150: "0.0150",
		180: "0.0180",
		225: "0.0225",
		255: "0.0255",
		0:   "0.0000",
		1:   "0.0001",
	}
	for in, want := range tests {
		if got := money.RateFromTenThousandths(in).String(); got != want {
			t.Errorf("Rate(%d) = %s, want %s", in, got, want)
		}
	}
}

func TestRateArithmetic(t *testing.T) {
	base := money.RateFromTenThousandths(150)
	adjustment := money.RateFromTenThousandths(30)

	sum := base.Add(adjustment)

	if got := sum.String(); got != "0.0180" {
		t.Errorf("0.0150 + 0.0030 = %s, want 0.0180", got)
	}
	if n, ok := sum.TenThousandths(); !ok || n != 180 {
		t.Errorf("TenThousandths = %d, %v", n, ok)
	}
}

// The wire format must look like money, not like a float.
func TestJSONRendering(t *testing.T) {
	v := struct {
		Rate   money.Rate   `json:"commissionRate"`
		Amount money.Amount `json:"totalCommission"`
	}{
		Rate:   money.RateFromTenThousandths(180),
		Amount: mustAmount(t, "4500"),
	}

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"commissionRate":0.0180,"totalCommission":4500.00}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}

// The zero value must be usable rather than panicking on a nil rational.
func TestZeroValueIsUsable(t *testing.T) {
	var a money.Amount
	var r money.Rate

	if a.String() != "0.00" {
		t.Errorf("zero Amount = %s", a.String())
	}
	if r.String() != "0.0000" {
		t.Errorf("zero Rate = %s", r.String())
	}
	if a.Sign() != 0 {
		t.Error("zero Amount should have sign 0")
	}
}

// Values are immutable: an operation must not mutate its receiver or argument.
func TestOperationsDoNotMutate(t *testing.T) {
	a := mustAmount(t, "100.00")
	r := money.RateFromTenThousandths(150)

	_ = a.Mul(r)
	_ = r.Add(money.RateFromTenThousandths(30))

	if got := a.String(); got != "100.00" {
		t.Errorf("Mul mutated its receiver: %s", got)
	}
	if got := r.String(); got != "0.0150" {
		t.Errorf("Add mutated its receiver: %s", got)
	}

	// A caller cannot reach in through Rat either.
	exported := a.Rat()
	exported.SetInt64(999)
	if got := a.String(); got != "100.00" {
		t.Errorf("Rat exposed the internal value: %s", got)
	}
}
