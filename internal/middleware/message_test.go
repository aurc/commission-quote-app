package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Middleware is an internal service with no browser. A message telling a
// caller to sign in is a remedy expressed in terms of a UI that may not exist,
// and would be nonsense to a batch job. The BFF owns user facing wording.
func TestMessagesDoNotContainUICopy(t *testing.T) {
	uiPhrases := []string{
		"sign in", "log in", "session", "click", "page", "screen",
		"highlighted", "try again", "your ",
	}

	cases := []struct {
		name string
		call func(t *testing.T) *httptest.ResponseRecorder
	}{
		{"unauthenticated", func(t *testing.T) *httptest.ResponseRecorder {
			return callWith(t, newMiddleware(t, okVendor(t)), "")
		}},
		{"forbidden", func(t *testing.T) *httptest.ResponseRecorder {
			return callWith(t, newMiddleware(t, okVendor(t)), "Bearer "+mint(t, tokenOpts{subject: unentitledSubject(t)}))
		}},
		{"validation", func(t *testing.T) *httptest.ResponseRecorder {
			return quote(t, newMiddleware(t, okVendor(t)), `{"loanAmount":1,"loanTermInMonths":1,"riskBand":"Z"}`)
		}},
		{"upstream unavailable", func(t *testing.T) *httptest.ResponseRecorder {
			vendor := newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			return quote(t, newMiddleware(t, vendor), validRequest)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.call(t)

			var env struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatal(err)
			}
			if env.Error.Message == "" {
				t.Fatal("every error must carry a message")
			}

			lower := strings.ToLower(env.Error.Message)
			for _, phrase := range uiPhrases {
				if strings.Contains(lower, phrase) {
					t.Errorf("message %q contains UI copy %q; the BFF owns user wording", env.Error.Message, phrase)
				}
			}
		})
	}
}
