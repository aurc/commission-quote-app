// Package cqapi is the mocked external vendor Commission Quote API.
//
// It stands in for a system we do not control: it refuses us when the key is
// wrong, fails at random, and is slow sometimes. Everything here models the
// vendor's behaviour, not ours.
package cqapi

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

// RiskBand is the vendor's risk classification, per contract.md section 1.
type RiskBand string

const (
	BandA RiskBand = "A"
	BandB RiskBand = "B"
	BandC RiskBand = "C"
)

// Money is exact here, never floating point.
//
// totalCommission = loanAmount * commissionRate rounded to cents. In float64,
// 250000.00 * 0.0180 is 4500.000000000001, and errors of that shape accumulate
// and eventually show up in a number someone is paid. Amounts are therefore held
// as whole cents and rates as whole ten thousandths, so every operation is
// integer arithmetic with one explicit rounding step at the end.
type (
	// Cents is a monetary amount in whole cents. 4500.00 is 450000.
	Cents int64
	// Rate is a commission rate in ten thousandths. 0.0180 is 180.
	Rate int64
)

const (
	centsPerUnit    = 100
	rateScale       = 10000
	maxTermCapMonth = 72 // where the term adjustment stops growing
)

// MarshalJSON renders cents as a JSON number with exactly two decimal places.
func (c Cents) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "%d.%02d", c/centsPerUnit, c%centsPerUnit), nil
}

// MarshalJSON renders a rate as a JSON number with exactly four decimal places.
func (r Rate) MarshalJSON() ([]byte, error) {
	return fmt.Appendf(nil, "%d.%04d", r/rateScale, r%rateScale), nil
}

// Float returns the rate as a float. For display and tests only, never for
// arithmetic that produces money.
func (r Rate) Float() float64 { return float64(r) / rateScale }

// baseRates is the vendor's pricing, in ten thousandths.
var baseRates = map[RiskBand]Rate{
	BandA: 100, // 0.0100
	BandB: 150, // 0.0150
	BandC: 225, // 0.0225
}

// CommissionRate returns the rate for a band and term.
//
//	base           = { A: 0.0100, B: 0.0150, C: 0.0225 }
//	termAdjustment = min(0.0005 * floor(months / 12), 0.0030)
//
// The adjustment stops growing at 72 months, so a 30 year term earns the same
// uplift as a 6 year one.
func CommissionRate(band RiskBand, months int64) (Rate, bool) {
	base, ok := baseRates[band]
	if !ok {
		return 0, false
	}
	adjustment := Rate(5 * (months / 12))
	if adjustment > 30 {
		adjustment = 30
	}
	return base + adjustment, true
}

// TotalCommission returns amount * rate rounded half up to the nearest cent.
//
// amount is in cents (1e-2) and rate in ten thousandths (1e-4), so their product
// is in units of 1e-6. Dividing by 1e4 returns cents. The worst case, the maximum
// loan at the maximum rate, is around 1.3e11 and nowhere near overflowing int64.
func TotalCommission(amount Cents, rate Rate) Cents {
	product := int64(amount) * int64(rate)
	return Cents((product + rateScale/2) / rateScale)
}

// Quote is the vendor's response payload.
type Quote struct {
	QuoteID         string `json:"quoteId"`
	CommissionRate  Rate   `json:"commissionRate"`
	TotalCommission Cents  `json:"totalCommission"`
}

// Generate prices a loan. The vendor owns this calculation; nothing downstream
// recomputes or second guesses it.
func Generate(amount Cents, months int64, band RiskBand) (Quote, error) {
	rate, ok := CommissionRate(band, months)
	if !ok {
		return Quote{}, fmt.Errorf("unknown risk band %q", band)
	}
	id, err := newQuoteID()
	if err != nil {
		return Quote{}, err
	}
	return Quote{
		QuoteID:         id,
		CommissionRate:  rate,
		TotalCommission: TotalCommission(amount, rate),
	}, nil
}

// newQuoteID returns a UUIDv4. Hand rolled because ten lines of bit twiddling
// does not justify a dependency.
func newQuoteID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate quote id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// ErrAmountFormat reports an amount that is not a plain decimal with at most two
// decimal places.
var ErrAmountFormat = errors.New("must be a decimal amount with at most 2 decimal places")

// ParseCents converts the raw JSON text of an amount into whole cents.
//
// The text is parsed rather than the decoded float, because that is the only way
// to tell 999.999 from 1000.00: by the time JSON has become a float64 the extra
// digit is already gone. Exponent notation is rejected because the published
// schema is a plain decimal amount.
func ParseCents(s string) (Cents, error) {
	if s == "" {
		return 0, ErrAmountFormat
	}
	negative := false
	switch s[0] {
	case '-':
		negative, s = true, s[1:]
	case '+':
		s = s[1:]
	}

	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" || !allDigits(whole) {
		return 0, ErrAmountFormat
	}
	if hasFrac && (frac == "" || len(frac) > 2 || !allDigits(frac)) {
		return 0, ErrAmountFormat
	}

	var units int64
	for _, c := range []byte(whole) {
		next := units*10 + int64(c-'0')
		if next < units { // overflow
			return 0, ErrAmountFormat
		}
		units = next
	}

	cents := int64(0)
	switch len(frac) {
	case 1:
		cents = int64(frac[0]-'0') * 10
	case 2:
		cents = int64(frac[0]-'0')*10 + int64(frac[1]-'0')
	}

	total := units*centsPerUnit + cents
	if negative {
		total = -total
	}
	return Cents(total), nil
}

func allDigits(s string) bool {
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
