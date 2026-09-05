// Package config loads typed configuration from the environment once at startup.
//
// Errors are collected rather than returned on first failure, so an operator with
// three missing variables learns all three from one run. A component calls Err
// before it starts serving; a missing required key is fatal by design, per
// contract.md section 9.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/secrets"
)

// Loader reads values from a Provider, accumulating any problems it finds.
type Loader struct {
	src  secrets.Provider
	errs []error
}

// New returns a Loader reading from the process environment.
func New() *Loader { return &Loader{src: secrets.EnvProvider{}} }

// NewFrom returns a Loader reading from src. Used by tests.
func NewFrom(src secrets.Provider) *Loader { return &Loader{src: src} }

// Err reports every problem found, or nil.
func (l *Loader) Err() error { return errors.Join(l.errs...) }

func (l *Loader) fail(key string, err error) {
	l.errs = append(l.errs, fmt.Errorf("config %s: %w", key, err))
}

var errRequired = errors.New("is required but not set")

// String returns the value of key, or def when unset.
func (l *Loader) String(key, def string) string {
	if v, ok := l.src.Secret(key); ok {
		return v
	}
	return def
}

// RequiredString returns the value of key, recording an error when unset.
func (l *Loader) RequiredString(key string) string {
	v, ok := l.src.Secret(key)
	if !ok {
		l.fail(key, errRequired)
		return ""
	}
	return v
}

// RequiredSecret returns a masked-by-default secret, recording an error when unset.
// Prefer this over RequiredString for anything that authenticates us to someone else.
func (l *Loader) RequiredSecret(key string) secrets.Value {
	return secrets.Value(l.RequiredString(key))
}

// Int returns the integer at key, or def when unset. A malformed value is an error,
// never a silent fallback to the default.
func (l *Loader) Int(key string, def int) int {
	v, ok := l.src.Secret(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail(key, fmt.Errorf("must be an integer, got %q", v))
		return def
	}
	return n
}

// Float returns the float at key, or def when unset.
func (l *Loader) Float(key string, def float64) float64 {
	v, ok := l.src.Secret(key)
	if !ok {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		l.fail(key, fmt.Errorf("must be a number, got %q", v))
		return def
	}
	return f
}

// Rate returns a probability at key, constrained to 0.0 to 1.0 inclusive.
func (l *Loader) Rate(key string, def float64) float64 {
	f := l.Float(key, def)
	if f < 0 || f > 1 {
		l.fail(key, fmt.Errorf("must be between 0 and 1, got %v", f))
		return def
	}
	return f
}

// Duration returns the duration at key, or def when unset. Accepts Go duration
// syntax such as 2s or 150ms.
func (l *Loader) Duration(key string, def time.Duration) time.Duration {
	v, ok := l.src.Secret(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(key, fmt.Errorf("must be a duration such as 2s, got %q", v))
		return def
	}
	return d
}

// Bool returns the boolean at key, or def when unset. Accepts the forms
// strconv.ParseBool does, so true, false, 1 and 0 all work.
func (l *Loader) Bool(key string, def bool) bool {
	v, ok := l.src.Secret(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(key, fmt.Errorf("must be true or false, got %q", v))
		return def
	}
	return b
}

// Port returns a TCP port at key, or def when unset.
func (l *Loader) Port(key string, def int) int {
	p := l.Int(key, def)
	if p < 1 || p > 65535 {
		l.fail(key, fmt.Errorf("must be a valid port, got %d", p))
		return def
	}
	return p
}
