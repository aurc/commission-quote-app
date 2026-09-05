// Package staffdir reads the staff fixture that stands in for the bank's
// identity and entitlement systems.
//
// In production these are two systems: the IdP says who a person is, and the
// directory or policy service says what they may do. They are reached through
// the AuthProvider and Entitlements interfaces, and this package is what those
// interfaces resolve to until then.
//
// The BFF and the Middleware read the same file for different columns. That is
// deliberate: two hand edited lists that must agree would let a staff member
// sign in successfully and then be refused every quote, which is a confusing
// failure and an easy one to introduce.
package staffdir

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// scopeSeparator divides multiple scopes within the single CSV column.
const scopeSeparator = ";"

// Staff is one row of the fixture.
type Staff struct {
	// ID is the subject a token is issued for and entitlement is decided on.
	ID string
	// Name is for display in the front end. It is not an identifier.
	Name string
	// Scopes is what this person is granted. Possibly empty.
	Scopes []string
}

// Directory is the parsed fixture.
type Directory struct {
	byID  map[string]Staff
	order []Staff
}

// Load reads the fixture at path.
func Load(path string) (*Directory, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open staff fixture %q: %w (set STAFF_FILE to point at it)", path, err)
	}
	defer func() { _ = f.Close() }()

	d, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("staff fixture %q: %w", path, err)
	}
	return d, nil
}

// Parse reads the fixture from r.
func Parse(r io.Reader) (*Directory, error) {
	cr := csv.NewReader(r)
	cr.Comment = '#'
	cr.FieldsPerRecord = 3
	cr.TrimLeadingSpace = true

	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("is empty")
	}

	header := rows[0]
	if want := []string{"id", "name", "scopes"}; !slices.Equal(lower(header), want) {
		return nil, fmt.Errorf("header must be %v, got %v", want, header)
	}

	d := &Directory{byID: make(map[string]Staff, len(rows)-1)}
	for i, row := range rows[1:] {
		line := i + 2 // header, and one based

		id := strings.TrimSpace(row[0])
		if id == "" {
			return nil, fmt.Errorf("line %d: id is required", line)
		}
		if _, clash := d.byID[id]; clash {
			// A duplicate would silently shadow one row's scopes, which is
			// exactly the kind of drift this file exists to prevent.
			return nil, fmt.Errorf("line %d: duplicate id %q", line, id)
		}

		s := Staff{
			ID:     id,
			Name:   strings.TrimSpace(row[1]),
			Scopes: parseScopes(row[2]),
		}
		d.byID[id] = s
		d.order = append(d.order, s)
	}

	if len(d.order) == 0 {
		return nil, errors.New("contains no staff")
	}
	return d, nil
}

func parseScopes(field string) []string {
	var out []string
	for _, s := range strings.Split(field, scopeSeparator) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func lower(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

// Lookup returns the staff member with the given id.
func (d *Directory) Lookup(id string) (Staff, bool) {
	s, ok := d.byID[id]
	return s, ok
}

// All returns every staff member, in file order.
func (d *Directory) All() []Staff { return slices.Clone(d.order) }

// Granted reports the scopes held by subject, satisfying the Middleware's
// Entitlements interface. An unknown subject holds nothing rather than being an
// error: not being in the directory is a valid answer to "what may they do".
func (d *Directory) Granted(_ context.Context, subject string) ([]string, error) {
	s, ok := d.byID[subject]
	if !ok {
		return nil, nil
	}
	return slices.Clone(s.Scopes), nil
}
