// Command devstaff adds a staff member to the development fixtures.
//
// It appends one row to config/staff.csv and one to config/credentials.csv,
// hashing the password with bcrypt, so nobody has to hand write a hash or is
// tempted to store a plaintext one.
//
// The password is prompted for rather than taken as a flag. A flag would put the
// password in shell history and in the output of ps for every user on the
// machine, which is a bad habit to demonstrate even for a fixture. Piping a
// password in still works, so the tool remains scriptable.
//
//	make staff ARGS='-id staff-004 -name "Jane Doe"'
//	echo hunter2 | make staff ARGS='-id staff-005 -name "Sam" -scopes ""'
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"github.com/aurc/commission-quote-app/internal/platform/authtoken"
	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

// bcryptCost is the fixture's work factor. The default rather than the minimum:
// a fixture is the example someone copies.
const bcryptCost = bcrypt.DefaultCost

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "devstaff: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	id := flag.String("id", "", "staff id, for example staff-004")
	name := flag.String("name", "", "display name")
	scopes := flag.String("scopes", authtoken.ScopeQuoteGenerate, "semicolon separated scopes, empty for none")
	staffFile := flag.String("staff-file", envOr("STAFF_FILE", "config/staff.csv"), "staff fixture")
	credsFile := flag.String("credentials-file", envOr("CREDENTIALS_FILE", "config/credentials.csv"), "credentials fixture")
	flag.Parse()

	if strings.TrimSpace(*id) == "" || strings.TrimSpace(*name) == "" {
		return errors.New("-id and -name are required")
	}
	if strings.ContainsAny(*id+*name, ",\n\r") {
		return errors.New("id and name must not contain commas or newlines")
	}

	// Read the directory first, so a duplicate is refused before anything is
	// written and the two files cannot be left disagreeing.
	dir, err := staffdir.Load(*staffFile)
	if err != nil {
		return err
	}
	if _, exists := dir.Lookup(*id); exists {
		return fmt.Errorf("%q is already in %s", *id, *staffFile)
	}

	password, err := readPassword()
	if err != nil {
		return err
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if err := appendRow(*staffFile, fmt.Sprintf("%s,%s,%s", *id, *name, *scopes)); err != nil {
		return err
	}
	if err := appendRow(*credsFile, fmt.Sprintf("%s,%s", *id, hash)); err != nil {
		return fmt.Errorf("%w (note: %s was already updated, remove the row for %q)", err, *staffFile, *id)
	}

	fmt.Printf("added %s to %s and %s\n", *id, *staffFile, *credsFile)
	if strings.TrimSpace(*scopes) == "" {
		fmt.Printf("%s holds no scopes, so quote requests will be refused with 403\n", *id)
	}
	return nil
}

// readPassword prompts without echo when attached to a terminal, and reads a
// line otherwise so the tool stays scriptable.
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}

	fmt.Print("password: ")
	first, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}

	fmt.Print("confirm:  ")
	second, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}

// appendRow adds a line, making sure the file ends with a newline first so a
// missing trailing newline does not merge two rows.
func appendRow(path, row string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, info.Size()-1); err == nil && last[0] != '\n' {
			if _, err := f.WriteString("\n"); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
		}
	}
	if _, err := f.WriteString(row + "\n"); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
