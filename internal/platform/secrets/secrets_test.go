package secrets_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

func TestMask(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"long", "super-secret-key-abcd", "****abcd"},
		{"exactly eight", "abcdefgh", "****efgh"},
		{"seven is too short to reveal any", "abcdefg", "****"},
		{"short", "abc", "****"},
		{"empty", "", "****"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := secrets.Mask(tt.in); got != tt.want {
				t.Errorf("Mask(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The point of Value: a secret that leaks into a log line is still masked.
func TestValueNeverLogsInClear(t *testing.T) {
	const raw = "vendor-api-key-9f2c"
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	log.Info("calling vendor", "apiKey", secrets.Value(raw))

	out := buf.String()
	if strings.Contains(out, raw) {
		t.Fatalf("secret leaked into log output: %s", out)
	}
	if !strings.Contains(out, "****9f2c") {
		t.Errorf("expected masked value in output, got: %s", out)
	}
}

func TestValueNeverMarshalsInClear(t *testing.T) {
	const raw = "vendor-api-key-9f2c"
	b, err := json.Marshal(struct {
		Key secrets.Value `json:"key"`
	}{Key: raw})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), raw) {
		t.Fatalf("secret leaked into JSON: %s", b)
	}
}

func TestValueFormattingIsMasked(t *testing.T) {
	v := secrets.Value("vendor-api-key-9f2c")
	if got := v.String(); got != "****9f2c" {
		t.Errorf("String() = %q", got)
	}
	if v.Reveal() != "vendor-api-key-9f2c" {
		t.Error("Reveal must return the real secret")
	}
}

func TestEmptyEnvIsNotFound(t *testing.T) {
	t.Setenv("CQ_TEST_BLANK", "")
	if _, ok := (secrets.EnvProvider{}).Secret("CQ_TEST_BLANK"); ok {
		t.Error("a blank env var must not count as configured")
	}
	t.Setenv("CQ_TEST_SET", "value")
	if v, ok := (secrets.EnvProvider{}).Secret("CQ_TEST_SET"); !ok || v != "value" {
		t.Errorf("Secret() = %q, %v", v, ok)
	}
}
