package cqappbff_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The Middleware writes API messages; the BFF writes user copy. This is the
// test that keeps CQ-04's split real.
func TestMiddlewareMessagesAreReplacedWithUserCopy(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		middlewareBody  string
		wantStatus      int
		wantCode        string
		wantUserMessage string
		wantDetailsKept bool
	}{
		{
			name:            "validation",
			status:          http.StatusBadRequest,
			middlewareBody:  `{"error":{"code":"VALIDATION_FAILED","message":"request failed validation","details":[{"field":"loanAmount","code":"amount_out_of_range"}],"correlationId":"c1"}}`,
			wantStatus:      http.StatusBadRequest,
			wantCode:        "VALIDATION_FAILED",
			wantUserMessage: "Check the highlighted fields.",
			wantDetailsKept: true,
		},
		{
			name:            "forbidden",
			status:          http.StatusForbidden,
			middlewareBody:  `{"error":{"code":"FORBIDDEN","message":"caller is not entitled to the required scope","correlationId":"c2"}}`,
			wantStatus:      http.StatusForbidden,
			wantCode:        "FORBIDDEN",
			wantUserMessage: "You do not have access to generate quotes.",
		},
		{
			name:            "vendor unavailable",
			status:          http.StatusBadGateway,
			middlewareBody:  `{"error":{"code":"UPSTREAM_UNAVAILABLE","message":"upstream quote provider unavailable","correlationId":"c3"}}`,
			wantStatus:      http.StatusBadGateway,
			wantCode:        "UPSTREAM_UNAVAILABLE",
			wantUserMessage: "Quotes are unavailable right now. Try again shortly.",
		},
		{
			name:            "timeout",
			status:          http.StatusGatewayTimeout,
			middlewareBody:  `{"error":{"code":"UPSTREAM_TIMEOUT","message":"upstream quote provider timed out","correlationId":"c4"}}`,
			wantStatus:      http.StatusGatewayTimeout,
			wantCode:        "UPSTREAM_TIMEOUT",
			wantUserMessage: "The quote service took too long. Try again.",
		},
		{
			name:            "circuit open",
			status:          http.StatusServiceUnavailable,
			middlewareBody:  `{"error":{"code":"UPSTREAM_CIRCUIT_OPEN","message":"upstream calls suspended by circuit breaker","correlationId":"c5"}}`,
			wantStatus:      http.StatusServiceUnavailable,
			wantCode:        "UPSTREAM_CIRCUIT_OPEN",
			wantUserMessage: "Quotes are paused briefly. Try again in a moment.",
		},
		{
			// A code this BFF has never heard of must not reach a user as raw
			// API text.
			name:            "unknown code from a future middleware",
			status:          http.StatusTeapot,
			middlewareBody:  `{"error":{"code":"SOMETHING_NEW","message":"vendor quota exceeded for tenant 42","correlationId":"c6"}}`,
			wantStatus:      http.StatusTeapot,
			wantCode:        "SOMETHING_NEW",
			wantUserMessage: "Something went wrong. Try again.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newStack(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.middlewareBody))
			})
			cookie := signIn(t, s, entitledStaff(t), devPassword)

			rec := post(t, s, "/api/v1/quotes", `{"loanAmount":1,"loanTermInMonths":1,"riskBand":"Z"}`, cookie)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var env struct {
				Error struct {
					Code          string `json:"code"`
					Message       string `json:"message"`
					Details       []any  `json:"details"`
					CorrelationID string `json:"correlationId"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("not the error envelope: %v\n%s", err, rec.Body)
			}

			if env.Error.Code != tt.wantCode {
				t.Errorf("code = %q, want %q; the code is the contract and must survive", env.Error.Code, tt.wantCode)
			}
			if env.Error.Message != tt.wantUserMessage {
				t.Errorf("message = %q, want %q", env.Error.Message, tt.wantUserMessage)
			}
			if tt.wantDetailsKept && len(env.Error.Details) == 0 {
				t.Error("details must survive, the front end maps them to fields")
			}
			if env.Error.CorrelationID == "" {
				t.Error("correlationId must survive, a user quotes it when reporting a problem")
			}
		})
	}
}

// No API phrasing should reach a browser.
func TestNoMiddlewarePhrasingSurvives(t *testing.T) {
	s := newStack(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"code":"UPSTREAM_UNAVAILABLE","message":"upstream quote provider unavailable","correlationId":"c"}}`))
	})
	cookie := signIn(t, s, entitledStaff(t), devPassword)

	rec := post(t, s, "/api/v1/quotes", `{}`, cookie)

	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}

	// The code is checked separately and is deliberately preserved, so only the
	// message is inspected: UPSTREAM_UNAVAILABLE legitimately contains
	// "upstream", and a client branches on it.
	for _, phrase := range []string{"upstream", "bearer", "scope", "provider", "vendor", "token"} {
		if strings.Contains(strings.ToLower(env.Error.Message), phrase) {
			t.Errorf("API phrasing %q reached the browser in the message: %q", phrase, env.Error.Message)
		}
	}
}

// A 401 from the Middleware means our token was rejected, which is our fault.
// Passing it through would send the user to sign in again and change nothing.
func TestMiddlewareRejectingOurTokenIsNotTheUsersProblem(t *testing.T) {
	s := newStack(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHENTICATED","message":"bearer token missing, invalid or expired","correlationId":"c"}}`))
	})
	cookie := signIn(t, s, entitledStaff(t), devPassword)

	rec := post(t, s, "/api/v1/quotes", `{}`, cookie)

	if rec.Code == http.StatusUnauthorized {
		t.Fatal("the user was told to sign in again for a fault in this service")
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if !s.logs.Contains("the middleware rejected our token") {
		t.Error("the real cause must be logged for an operator")
	}

	// The session is still valid, since nothing was wrong with it.
	req := newGet("/api/session", cookie)
	rec2 := recorder()
	s.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Error("the staff session should be untouched by a middleware token fault")
	}
}

// A successful quote is passed through untouched: the numbers are the vendor's.
func TestSuccessfulQuoteIsNotRewritten(t *testing.T) {
	const body = `{"quoteId":"q-1","commissionRate":0.0180,"totalCommission":4500.00}`
	s := newStack(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
	cookie := signIn(t, s, entitledStaff(t), devPassword)

	rec := post(t, s, "/api/v1/quotes", `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B"}`, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != body {
		t.Errorf("the quote was altered:\n got  %s\n want %s", rec.Body.String(), body)
	}
}
