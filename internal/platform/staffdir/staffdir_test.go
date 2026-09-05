package staffdir_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

func parse(t *testing.T, csv string) *staffdir.Directory {
	t.Helper()
	d, err := staffdir.Parse(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

const sample = `# a comment
id,name,scopes
staff-001,Alex Turner,quote:generate
staff-002,Sam Ellis,
staff-003,Jordan Pike,quote:generate;quote:read
`

func TestParse(t *testing.T) {
	d := parse(t, sample)

	if got := len(d.All()); got != 3 {
		t.Fatalf("parsed %d staff, want 3", got)
	}

	s, ok := d.Lookup("staff-003")
	if !ok {
		t.Fatal("staff-003 should be present")
	}
	if s.Name != "Jordan Pike" {
		t.Errorf("name = %q", s.Name)
	}
	if len(s.Scopes) != 2 || s.Scopes[0] != "quote:generate" || s.Scopes[1] != "quote:read" {
		t.Errorf("scopes = %v, want both parsed from the semicolon list", s.Scopes)
	}
}

// An empty scopes column is a real case: it is the only way the 403 path gets
// exercised, so it must parse rather than error.
func TestEmptyScopesIsValid(t *testing.T) {
	d := parse(t, sample)

	s, ok := d.Lookup("staff-002")
	if !ok {
		t.Fatal("staff-002 should be present")
	}
	if len(s.Scopes) != 0 {
		t.Errorf("scopes = %v, want none", s.Scopes)
	}
}

func TestGranted(t *testing.T) {
	d := parse(t, sample)
	ctx := context.Background()

	got, err := d.Granted(ctx, "staff-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "quote:generate" {
		t.Errorf("Granted(staff-001) = %v", got)
	}

	// Not being in the directory is a valid answer to "what may they do".
	got, err = d.Granted(ctx, "nobody")
	if err != nil {
		t.Errorf("an unknown subject must not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Granted(nobody) = %v, want nothing", got)
	}
}

// A caller must not be able to mutate the directory through what it returns.
func TestGrantedReturnsACopy(t *testing.T) {
	d := parse(t, sample)

	got, _ := d.Granted(context.Background(), "staff-001")
	got[0] = "admin:everything"

	again, _ := d.Granted(context.Background(), "staff-001")
	if again[0] != "quote:generate" {
		t.Errorf("the directory was mutated through a returned slice: %v", again)
	}
}

func TestParseRejectsBadFixtures(t *testing.T) {
	tests := map[string]string{
		"empty file":       ``,
		"no header":        "staff-001,Alex,quote:generate\n",
		"wrong header":     "subject,name,scopes\nstaff-001,Alex,quote:generate\n",
		"too few columns":  "id,name,scopes\nstaff-001,Alex\n",
		"too many columns": "id,name,scopes\nstaff-001,Alex,quote:generate,extra\n",
		"missing id":       "id,name,scopes\n,Alex,quote:generate\n",
		"no staff at all":  "id,name,scopes\n",
		// A duplicate would silently shadow one row's scopes, which is exactly
		// the drift this fixture exists to prevent.
		"duplicate id": "id,name,scopes\nstaff-001,Alex,quote:generate\nstaff-001,Alex Again,\n",
	}
	for name, csv := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := staffdir.Parse(strings.NewReader(csv)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// The committed fixture must actually load, or the service does not start.
func TestCommittedFixtureIsValid(t *testing.T) {
	d, err := staffdir.Load(filepath.Join("..", "..", "..", "config", "staff.csv"))
	if err != nil {
		t.Fatalf("config/staff.csv must load: %v", err)
	}

	all := d.All()
	if len(all) == 0 {
		t.Fatal("the fixture must contain staff")
	}

	// The 403 path is only testable while someone in the fixture holds nothing.
	var entitled, unentitled int
	for _, s := range all {
		if len(s.Scopes) == 0 {
			unentitled++
		} else {
			entitled++
		}
	}
	if entitled == 0 {
		t.Error("the fixture needs at least one entitled staff member")
	}
	if unentitled == 0 {
		t.Error("the fixture needs at least one staff member with no scopes, or the 403 path cannot be exercised")
	}
}

func TestLoadReportsAMissingFileUsefully(t *testing.T) {
	_, err := staffdir.Load("does/not/exist.csv")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "STAFF_FILE") {
		t.Errorf("the error should name the variable that fixes it, got: %v", err)
	}
}
