package cqappmiddleware

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/money"
)

// Business rules, per contract.md section 4. Authoritative here; the front end
// mirrors them for feedback and is never trusted.
const (
	MinAmount = "1000.00"
	MaxAmount = "5000000.00"
	MinMonths = 6
	MaxMonths = 360

	maxAmountDecimals = 2
)

// Field error codes.
const (
	CodeAmountInvalid    = "amount_invalid"
	CodeAmountPrecision  = "amount_precision"
	CodeAmountOutOfRange = "amount_out_of_range"
	CodeTermInvalid      = "term_invalid"
	CodeTermNotInteger   = "term_not_integer"
	CodeTermOutOfRange   = "term_out_of_range"
	CodeRiskBandInvalid  = "risk_band_invalid"
	CodeMalformedBody    = "malformed_body"
)

// ValidBands is the accepted risk band domain.
var ValidBands = []string{"A", "B", "C"}

// QuoteRequest is a validated request, ready to send to the vendor.
type QuoteRequest struct {
	Amount   money.Amount
	Months   int64
	RiskBand string
}

// rawRequest keeps the numbers as raw JSON so their original text survives.
// contract.md section 4 constrains loanAmount to two decimal places, and that is
// only checkable before the digits are lost to a float. Raw also lets a quoted
// "1000" be rejected, which the decoder would otherwise accept into a numeric
// field, quietly making the schema we publish differ from the one we enforce.
type rawRequest struct {
	LoanAmount       json.RawMessage `json:"loanAmount"`
	LoanTermInMonths json.RawMessage `json:"loanTermInMonths"`
	RiskBand         *string         `json:"riskBand"`
}

// Validate parses and checks a request body.
//
// Every failure is collected and returned together rather than first failure
// wins. A form that reveals one problem at a time is a poor experience, and the
// front end renders this whole list at once.
func Validate(body []byte) (QuoteRequest, []httpx.FieldError) {
	var raw rawRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return QuoteRequest{}, []httpx.FieldError{{Field: "body", Code: CodeMalformedBody}}
	}
	// Trailing content after the object is not a valid single request body.
	if dec.More() {
		return QuoteRequest{}, []httpx.FieldError{{Field: "body", Code: CodeMalformedBody}}
	}

	var (
		out    QuoteRequest
		errs   []httpx.FieldError
		add    = func(field, code string) { errs = append(errs, httpx.FieldError{Field: field, Code: code}) }
		minAmt = mustAmount(MinAmount)
		maxAmt = mustAmount(MaxAmount)
	)

	// loanAmount
	switch text, ok := numberText(raw.LoanAmount); {
	case !ok:
		add("loanAmount", CodeAmountInvalid)
	default:
		amount, err := money.ParseAmount(text)
		switch {
		case err != nil:
			add("loanAmount", CodeAmountInvalid)
		case amount.DecimalPlaces() > maxAmountDecimals:
			add("loanAmount", CodeAmountPrecision)
		case amount.Cmp(minAmt) < 0 || amount.Cmp(maxAmt) > 0:
			add("loanAmount", CodeAmountOutOfRange)
		default:
			out.Amount = amount
		}
	}

	// loanTermInMonths
	switch text, ok := numberText(raw.LoanTermInMonths); {
	case !ok:
		add("loanTermInMonths", CodeTermInvalid)
	default:
		months, err := json.Number(text).Int64()
		switch {
		case err != nil && strings.ContainsAny(text, ".eE"):
			add("loanTermInMonths", CodeTermNotInteger)
		case err != nil:
			add("loanTermInMonths", CodeTermInvalid)
		case months < MinMonths || months > MaxMonths:
			add("loanTermInMonths", CodeTermOutOfRange)
		default:
			out.Months = months
		}
	}

	// riskBand
	switch {
	case raw.RiskBand == nil:
		add("riskBand", CodeRiskBandInvalid)
	default:
		band := *raw.RiskBand
		if !contains(ValidBands, band) {
			add("riskBand", CodeRiskBandInvalid)
			break
		}
		out.RiskBand = band
	}

	return out, errs
}

// numberText returns the literal text of a JSON number, rejecting an absent
// field, a null and anything quoted.
func numberText(raw json.RawMessage) (string, bool) {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s[0] == '"' {
		return "", false
	}
	return s, true
}

func contains(all []string, want string) bool {
	for _, v := range all {
		if v == want {
			return true
		}
	}
	return false
}

// mustAmount parses a compile time constant bound. A failure here is a
// programming error in this file, not a runtime condition.
func mustAmount(s string) money.Amount {
	a, err := money.ParseAmount(s)
	if err != nil {
		panic("middleware: invalid bound " + s + ": " + err.Error())
	}
	return a
}
