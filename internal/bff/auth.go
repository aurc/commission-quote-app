// Package bff is the browser facing service. Web semantics stop here: it owns
// the staff session, exchanges it for a bearer claim, and writes the words a
// person reads.
//
// It holds no business logic and no vendor credential.
package bff

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

// ErrInvalidCredentials is the only failure sign in ever reports. An unknown
// staff member and a wrong password are the same answer, deliberately: telling
// them apart tells an attacker who exists.
var ErrInvalidCredentials = errors.New("invalid credentials")

// AuthProvider authenticates a staff member.
//
// The MVP implementation reads two fixtures. Production replaces it with the
// bank's identity provider behind this interface, at which point no password
// ever reaches this service.
type AuthProvider interface {
	Authenticate(ctx context.Context, staffID, password string) (staffdir.Staff, error)
}

// dummyHash is compared against when no credential exists, so an unknown staff
// id costs the same bcrypt work as a wrong password. Without it, response timing
// answers "does this person exist" for anyone who cares to measure.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// FixtureAuth authenticates against config/staff.csv and config/credentials.csv.
type FixtureAuth struct {
	staff *staffdir.Directory
	hash  map[string][]byte
}

// NewFixtureAuth pairs a staff directory with a credentials file.
//
// A credential naming a staff member who does not exist is a startup failure.
// Two files that must agree is the drift risk of splitting them, so it is
// checked rather than trusted.
func NewFixtureAuth(staff *staffdir.Directory, credentialsPath string) (*FixtureAuth, error) {
	f, err := os.Open(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("open credentials %q: %w (set CREDENTIALS_FILE to point at it)", credentialsPath, err)
	}
	defer func() { _ = f.Close() }()

	hash, err := parseCredentials(f)
	if err != nil {
		return nil, fmt.Errorf("credentials %q: %w", credentialsPath, err)
	}
	for id := range hash {
		if _, ok := staff.Lookup(id); !ok {
			return nil, fmt.Errorf("credentials %q: %q is not in the staff directory", credentialsPath, id)
		}
	}
	return &FixtureAuth{staff: staff, hash: hash}, nil
}

// Authenticate verifies a password and returns the staff member.
func (a *FixtureAuth) Authenticate(_ context.Context, staffID, password string) (staffdir.Staff, error) {
	staff, known := a.staff.Lookup(staffID)
	stored, hasCredential := a.hash[staffID]
	if !hasCredential {
		// Still pay for a comparison, so the timing is the same either way.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return staffdir.Staff{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword(stored, []byte(password)); err != nil {
		return staffdir.Staff{}, ErrInvalidCredentials
	}
	if !known {
		// Unreachable while NewFixtureAuth's check holds, but a credential
		// without an identity must never authenticate.
		return staffdir.Staff{}, ErrInvalidCredentials
	}
	return staff, nil
}

// parseCredentials reads id and bcrypt hash pairs.
func parseCredentials(r io.Reader) (map[string][]byte, error) {
	cr := csv.NewReader(r)
	cr.Comment = '#'
	cr.FieldsPerRecord = 2
	cr.TrimLeadingSpace = true

	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("is empty")
	}
	if want := []string{"id", "passwordhash"}; !slices.Equal(lowerAll(rows[0]), want) {
		return nil, fmt.Errorf("header must be %v, got %v", want, rows[0])
	}

	out := make(map[string][]byte, len(rows)-1)
	for i, row := range rows[1:] {
		line := i + 2

		id := strings.TrimSpace(row[0])
		hash := strings.TrimSpace(row[1])
		if id == "" {
			return nil, fmt.Errorf("line %d: id is required", line)
		}
		if _, clash := out[id]; clash {
			return nil, fmt.Errorf("line %d: duplicate id %q", line, id)
		}
		// A plaintext or fast hashed password in this file would authenticate
		// happily and be wrong in a way nothing else would notice.
		if _, err := bcrypt.Cost([]byte(hash)); err != nil {
			return nil, fmt.Errorf("line %d: %q is not a bcrypt hash: %w", line, id, err)
		}
		out[id] = []byte(hash)
	}
	if len(out) == 0 {
		return nil, errors.New("contains no credentials")
	}
	return out, nil
}

func lowerAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToLower(strings.TrimSpace(s))
	}
	return out
}
