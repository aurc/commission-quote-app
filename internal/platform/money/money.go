// Package money holds exact monetary values.
//
// Arithmetic is done with math/big.Rat, which is exact for any decimal input and
// for any product of decimals. Nothing here rounds until a value reaches a
// boundary, and rounding is then one named, tested operation rather than a side
// effect of the type.
//
// Boundaries are integers. A value crosses into JSON, a log line or a store as
// whole cents or whole ten thousandths, never as a float64. float64 cannot hold
// 0.01 exactly, so 250000.00 * 0.0180 is 4500.000000000001, and errors of that
// shape accumulate into a number somebody is eventually paid.
//
// Amount and Rate are immutable. Every operation returns a new value, so a
// shared big.Rat can never be mutated behind a caller's back.
package money

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// maxTextLen bounds parser input. Amounts arrive from the network, and big.Rat
// will happily build a number with a million digits.
const maxTextLen = 32

var (
	// ErrFormat reports text that is not a plain decimal number.
	ErrFormat = errors.New("must be a plain decimal number")
	// ErrTooLong reports implausibly long input.
	ErrTooLong = errors.New("is too long to be a monetary value")
)

// Amount is an exact monetary quantity in a single currency (AUD).
type Amount struct {
	r *big.Rat
	// scale records the decimal places seen in the source text, so a validator
	// can enforce a precision rule that parsing itself does not impose.
	scale int
}

// Rate is an exact rate, such as a commission rate. 0.0180 is 1.80%.
type Rate struct {
	r *big.Rat
}

// The scales used at our boundaries.
const (
	amountPlaces = 2 // cents
	ratePlaces   = 4 // ten thousandths
)

func (a Amount) rat() *big.Rat {
	if a.r == nil {
		return new(big.Rat)
	}
	return a.r
}

func (r Rate) rat() *big.Rat {
	if r.r == nil {
		return new(big.Rat)
	}
	return r.r
}

// ParseAmount reads a plain decimal amount exactly.
//
// It deliberately accepts any number of decimal places and records how many were
// present. Precision is a business rule, not a parsing rule: the vendor and the
// Middleware enforce different ones, and both need to see what the caller
// actually sent. Exponent notation is rejected because the published schemas
// describe a plain decimal amount.
func ParseAmount(text string) (Amount, error) {
	r, scale, err := parseDecimal(text)
	if err != nil {
		return Amount{}, err
	}
	return Amount{r: r, scale: scale}, nil
}

// ParseRate reads a plain decimal rate exactly.
func ParseRate(text string) (Rate, error) {
	r, _, err := parseDecimal(text)
	if err != nil {
		return Rate{}, err
	}
	return Rate{r: r}, nil
}

// FromCents builds an Amount from whole cents.
func FromCents(cents int64) Amount {
	return Amount{r: new(big.Rat).SetFrac64(cents, 100), scale: amountPlaces}
}

// RateFromTenThousandths builds a Rate from whole ten thousandths. 180 is 0.0180.
func RateFromTenThousandths(n int64) Rate {
	return Rate{r: new(big.Rat).SetFrac64(n, 10000)}
}

// DecimalPlaces reports how many decimal places the source text carried. It is
// how the two decimal place rule in contract.md section 4 is enforced.
func (a Amount) DecimalPlaces() int { return a.scale }

// Sign reports the sign of the amount.
func (a Amount) Sign() int { return a.rat().Sign() }

// Cmp compares two amounts, exactly.
func (a Amount) Cmp(b Amount) int { return a.rat().Cmp(b.rat()) }

// Mul returns a * r, exactly and without rounding. The result keeps full
// precision; rounding happens only when it reaches a boundary.
func (a Amount) Mul(r Rate) Amount {
	return Amount{r: new(big.Rat).Mul(a.rat(), r.rat())}
}

// Cents returns the amount as whole cents, rounded half up, and reports whether
// it fits in an int64. A caller that has already range checked the value can
// ignore nothing: the boolean exists because an unchecked amount arrives from
// the network and could be arbitrarily large.
func (a Amount) Cents() (int64, bool) {
	return scaledInt(a.rat(), amountPlaces)
}

// TenThousandths returns the rate as a whole number of ten thousandths, rounded
// half up, and whether it fits in an int64.
func (r Rate) TenThousandths() (int64, bool) {
	return scaledInt(r.rat(), ratePlaces)
}

// Add returns r + o.
func (r Rate) Add(o Rate) Rate { return Rate{r: new(big.Rat).Add(r.rat(), o.rat())} }

// Cmp compares two rates, exactly.
func (r Rate) Cmp(o Rate) int { return r.rat().Cmp(o.rat()) }

// Float returns the rate as a float64. For display and logs only, never for
// arithmetic that produces money.
func (r Rate) Float() float64 {
	f, _ := r.rat().Float64()
	return f
}

// String renders the amount with exactly two decimal places, rounded half up.
func (a Amount) String() string { return format(a.rat(), amountPlaces) }

// String renders the rate with exactly four decimal places, rounded half up.
func (r Rate) String() string { return format(r.rat(), ratePlaces) }

// MarshalJSON renders the amount as a JSON number with two decimal places.
func (a Amount) MarshalJSON() ([]byte, error) { return []byte(a.String()), nil }

// MarshalJSON renders the rate as a JSON number with four decimal places.
func (r Rate) MarshalJSON() ([]byte, error) { return []byte(r.String()), nil }

// Rat returns a copy of the underlying rational, for callers that need to do
// their own exact arithmetic.
func (a Amount) Rat() *big.Rat { return new(big.Rat).Set(a.rat()) }

// Rat returns a copy of the underlying rational.
func (r Rate) Rat() *big.Rat { return new(big.Rat).Set(r.rat()) }

// RoundHalfUp rounds r to the given number of decimal places, half up.
//
// Half up means a tie goes towards positive infinity: 10.005 becomes 10.01.
// Monetary amounts in this system are validated positive before they reach a
// rounding boundary, so the behaviour of a negative tie never arises in
// practice; it is defined here only so the function is total.
func RoundHalfUp(r *big.Rat, places int) *big.Rat {
	scale := scaleFactor(places)
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(scale))
	sum := new(big.Rat).Add(scaled, big.NewRat(1, 2))

	// big.Int.Div is Euclidean, which for a positive divisor is floor.
	n := new(big.Int).Div(sum.Num(), sum.Denom())
	return new(big.Rat).SetFrac(n, scale)
}

func scaleFactor(places int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
}

// scaledInt rounds to the given places and returns the result as an integer at
// that scale, reporting whether it fits in an int64.
func scaledInt(r *big.Rat, places int) (int64, bool) {
	rounded := RoundHalfUp(r, places)
	n := new(big.Int).Mul(rounded.Num(), scaleFactor(places))
	n.Div(n, rounded.Denom())
	if !n.IsInt64() {
		return 0, false
	}
	return n.Int64(), true
}

// format renders a rational with exactly the given number of decimal places,
// using the same half up rule as every other boundary in this package.
func format(r *big.Rat, places int) string {
	return RoundHalfUp(r, places).FloatString(places)
}

// parseDecimal reads a plain decimal into an exact rational, returning the
// number of decimal places seen.
func parseDecimal(text string) (*big.Rat, int, error) {
	if len(text) > maxTextLen {
		return nil, 0, ErrTooLong
	}
	s := text
	if s == "" {
		return nil, 0, ErrFormat
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
		return nil, 0, ErrFormat
	}
	if hasFrac && (frac == "" || !allDigits(frac)) {
		return nil, 0, ErrFormat
	}

	// digits/10^places is exact for any decimal, which is the whole point of
	// going through big.Rat rather than a float.
	digits, ok := new(big.Int).SetString(whole+frac, 10)
	if !ok {
		return nil, 0, ErrFormat
	}
	if negative {
		digits.Neg(digits)
	}

	places := len(frac)
	r := new(big.Rat).SetFrac(digits, scaleFactor(places))
	return r, places, nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// GoString helps test failure output stay readable.
func (a Amount) GoString() string { return fmt.Sprintf("money.Amount(%s)", a.rat().RatString()) }

// GoString helps test failure output stay readable.
func (r Rate) GoString() string { return fmt.Sprintf("money.Rate(%s)", r.rat().RatString()) }
