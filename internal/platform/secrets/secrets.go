// Package secrets provides the seam between the application and whatever holds
// its credentials. The MVP reads environment variables; production swaps in the
// bank secret manager behind the same interface.
package secrets

import (
	"encoding/json"
	"log/slog"
	"os"
)

// Provider resolves a named secret. Absent and empty are both reported as not found,
// so a blank environment variable can never be mistaken for a configured secret.
type Provider interface {
	Secret(name string) (string, bool)
}

// EnvProvider reads secrets from the process environment.
type EnvProvider struct{}

func (EnvProvider) Secret(name string) (string, bool) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// MapProvider is an in-memory Provider for tests.
type MapProvider map[string]string

func (m MapProvider) Secret(name string) (string, bool) {
	v, ok := m[name]
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// Value holds a secret and refuses to reveal it by accident. Its String, LogValue
// and MarshalJSON all return the mask, so a Value that reaches a log line, a
// formatted string or a response body is masked rather than exposed. Reveal is the
// single, greppable way to obtain the real value.
//
// This makes contract.md section 8 an enforced property rather than a convention.
type Value string

func (v Value) String() string { return Mask(string(v)) }

func (v Value) LogValue() slog.Value { return slog.StringValue(Mask(string(v))) }

func (v Value) MarshalJSON() ([]byte, error) { return json.Marshal(Mask(string(v))) }

// Reveal returns the underlying secret. Call it only when handing the value to the
// party it authenticates against.
func (v Value) Reveal() string { return string(v) }

// Mask renders a secret as ****<last 4>. Anything shorter than 8 characters is
// masked completely, because revealing 4 of 6 characters gives away too much.
func Mask(s string) string {
	if len(s) < 8 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}
