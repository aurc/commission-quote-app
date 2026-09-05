package middleware_test

import (
	"encoding/json"
	"net/http"
	"sort"
	"testing"
)

func fieldErrors(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Details []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("not the error envelope: %v\n%s", err, body)
	}
	out := map[string]string{}
	for _, d := range env.Error.Details {
		out[d.Field] = d.Code
	}
	return out
}

// Every rule in contract.md section 4, with the boundaries on both sides.
func TestValidation(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		field string
		code  string
	}{
		// loanAmount
		{"amount missing", `{"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_invalid"},
		{"amount null", `{"loanAmount":null,"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_invalid"},
		{"amount quoted", `{"loanAmount":"250000.00","loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_invalid"},
		{"amount not a number", `{"loanAmount":true,"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_invalid"},
		{"amount in exponent notation", `{"loanAmount":1e6,"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_invalid"},
		{"amount with three decimals", `{"loanAmount":999999.999,"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_precision"},
		{"amount below the minimum", `{"loanAmount":999.99,"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_out_of_range"},
		{"amount above the maximum", `{"loanAmount":5000000.01,"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_out_of_range"},
		{"amount zero", `{"loanAmount":0,"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_out_of_range"},
		{"amount negative", `{"loanAmount":-250000.00,"loanTermInMonths":240,"riskBand":"B"}`, "loanAmount", "amount_out_of_range"},

		// loanTermInMonths
		{"term missing", `{"loanAmount":250000.00,"riskBand":"B"}`, "loanTermInMonths", "term_invalid"},
		{"term quoted", `{"loanAmount":250000.00,"loanTermInMonths":"240","riskBand":"B"}`, "loanTermInMonths", "term_invalid"},
		{"term fractional", `{"loanAmount":250000.00,"loanTermInMonths":12.5,"riskBand":"B"}`, "loanTermInMonths", "term_not_integer"},
		{"term below the minimum", `{"loanAmount":250000.00,"loanTermInMonths":5,"riskBand":"B"}`, "loanTermInMonths", "term_out_of_range"},
		{"term above the maximum", `{"loanAmount":250000.00,"loanTermInMonths":361,"riskBand":"B"}`, "loanTermInMonths", "term_out_of_range"},
		{"term zero", `{"loanAmount":250000.00,"loanTermInMonths":0,"riskBand":"B"}`, "loanTermInMonths", "term_out_of_range"},
		{"term negative", `{"loanAmount":250000.00,"loanTermInMonths":-12,"riskBand":"B"}`, "loanTermInMonths", "term_out_of_range"},

		// riskBand
		{"band missing", `{"loanAmount":250000.00,"loanTermInMonths":240}`, "riskBand", "risk_band_invalid"},
		{"band lowercase", `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"b"}`, "riskBand", "risk_band_invalid"},
		{"band unknown", `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"D"}`, "riskBand", "risk_band_invalid"},
		{"band empty", `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":""}`, "riskBand", "risk_band_invalid"},
		{"band padded", `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":" B"}`, "riskBand", "risk_band_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := quote(t, newMiddleware(t, okVendor(t)), tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			got := fieldErrors(t, rec.Body.Bytes())
			if got[tt.field] != tt.code {
				t.Errorf("%s = %q, want %q (all: %v)", tt.field, got[tt.field], tt.code, got)
			}
		})
	}
}

// The boundaries themselves are valid. Off by one here is a real bug.
func TestBoundariesAreInclusive(t *testing.T) {
	h := newMiddleware(t, okVendor(t))

	valid := []string{
		`{"loanAmount":1000.00,"loanTermInMonths":6,"riskBand":"A"}`,
		`{"loanAmount":5000000.00,"loanTermInMonths":360,"riskBand":"C"}`,
		`{"loanAmount":1000,"loanTermInMonths":6,"riskBand":"A"}`,
		`{"loanAmount":1000.5,"loanTermInMonths":6,"riskBand":"B"}`,
	}
	for _, body := range valid {
		if rec := quote(t, h, body); rec.Code != http.StatusOK {
			t.Errorf("body %s returned %d, want 200: %s", body, rec.Code, rec.Body)
		}
	}
}

// A form that reveals one problem at a time is a poor experience, and the front
// end renders the whole list.
func TestAllFailuresAreReportedTogether(t *testing.T) {
	rec := quote(t, newMiddleware(t, okVendor(t)),
		`{"loanAmount":1,"loanTermInMonths":9999,"riskBand":"Z"}`)

	got := fieldErrors(t, rec.Body.Bytes())
	if len(got) != 3 {
		t.Fatalf("expected all 3 fields reported, got %v", got)
	}

	fields := make([]string, 0, len(got))
	for f := range got {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	want := []string{"loanAmount", "loanTermInMonths", "riskBand"}
	for i := range want {
		if fields[i] != want[i] {
			t.Errorf("fields = %v, want %v", fields, want)
		}
	}
}

func TestMalformedBodies(t *testing.T) {
	tests := map[string]string{
		"empty":            ``,
		"not an object":    `[]`,
		"broken json":      `{"loanAmount":`,
		"unknown field":    `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B","extra":1}`,
		"trailing content": `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B"}{}`,
		"just a string":    `"hello"`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			rec := quote(t, newMiddleware(t, okVendor(t)), body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			if got := fieldErrors(t, rec.Body.Bytes())["body"]; got != "malformed_body" {
				t.Errorf("body code = %q, want malformed_body", got)
			}
		})
	}
}

// An invalid request must never reach the vendor. Validation is a gate, not a
// suggestion.
func TestInvalidRequestsNeverReachTheVendor(t *testing.T) {
	vendor := okVendor(t)
	h := newMiddleware(t, vendor)

	quote(t, h, `{"loanAmount":1,"loanTermInMonths":9999,"riskBand":"Z"}`)

	if vendor.lastRequest() != "" {
		t.Errorf("an invalid request was forwarded to the vendor: %s", vendor.lastRequest())
	}
}
