// Package cqapi is the mocked external vendor Commission Quote API.
//
// It stands in for a system we do not control: it refuses us when the key is
// wrong, fails at random, and is slow sometimes. Everything here models the
// vendor's behaviour, not ours.
package cqapi

import (
	"crypto/rand"
	"fmt"

	"github.com/aurc/commission-quote-app/internal/platform/money"
)

// RiskBand is the vendor's risk classification, per contract.md section 1.
type RiskBand string

const (
	BandA RiskBand = "A"
	BandB RiskBand = "B"
	BandC RiskBand = "C"
)

// Pricing, in ten thousandths. Held as integers because that is what the
// vendor's rate card is: whole basis points, not measured quantities.
const (
	baseRateA = 100 // 0.0100
	baseRateB = 150 // 0.0150
	baseRateC = 225 // 0.0225

	termStep        = 5  // 0.0005 per full year
	termAdjustCap   = 30 // 0.0030
	monthsPerYear   = 12
	termCapAtMonths = 72 // where the adjustment stops growing
)

var baseRates = map[RiskBand]int64{
	BandA: baseRateA,
	BandB: baseRateB,
	BandC: baseRateC,
}

// CommissionRate returns the rate for a band and term.
//
//	base           = { A: 0.0100, B: 0.0150, C: 0.0225 }
//	termAdjustment = min(0.0005 * floor(months / 12), 0.0030)
//
// The adjustment stops growing at 72 months, so a 30 year term earns the same
// uplift as a 6 year one.
func CommissionRate(band RiskBand, months int64) (money.Rate, bool) {
	base, ok := baseRates[band]
	if !ok {
		return money.Rate{}, false
	}
	adjustment := termStep * (months / monthsPerYear)
	if adjustment > termAdjustCap {
		adjustment = termAdjustCap
	}
	return money.RateFromTenThousandths(base + adjustment), true
}

// Quote is the vendor's response payload.
type Quote struct {
	QuoteID         string       `json:"quoteId"`
	CommissionRate  money.Rate   `json:"commissionRate"`
	TotalCommission money.Amount `json:"totalCommission"`
}

// Generate prices a loan. The vendor owns this calculation; nothing downstream
// recomputes or second guesses it.
//
// The multiplication is exact and unrounded; rounding to cents happens once,
// where the amount is rendered.
func Generate(amount money.Amount, months int64, band RiskBand) (Quote, error) {
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
		TotalCommission: amount.Mul(rate),
	}, nil
}

// newQuoteID returns a UUIDv4. Hand rolled because ten lines of bit twiddling
// does not justify a dependency. Verifying a JWT, by contrast, does: see CQ-04.
func newQuoteID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate quote id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
