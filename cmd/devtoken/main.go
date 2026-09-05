// Command devtoken prints a bearer token for calling the Middleware directly
// during development.
//
// Development only. It exists because the BFF, which mints these tokens for
// real, arrives in CQ-06, and until then the Middleware cannot be exercised by
// hand. It is not in the SERVICES list, is never built into an image, and should
// be deleted once the BFF can issue a session.
//
// It mints nothing it is not already entitled to: it signs with the same
// BFF_MIDDLEWARE_SIGNING_KEY the Middleware verifies with, so anyone who can run
// it already holds the key.
//
//	make token                            # the first entitled staff member
//	make token ARGS='-sub <id>'           # any id from config/staff.csv
//	make token ARGS='-scope ""'           # a token that does not request the scope
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/aurc/commission-quote-app/internal/middleware"
	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

func main() {
	subject := flag.String("sub", "", "staff subject, defaults to the first entitled staff member in the fixture")
	scope := flag.String("scope", middleware.ScopeQuoteGenerate, "space separated scopes to request, empty for none")
	ttl := flag.Duration("ttl", time.Minute, "how long the token is valid for")
	flag.Parse()

	if *subject == "" {
		found, err := firstEntitled()
		if err != nil {
			fmt.Fprintf(os.Stderr, "devtoken: %v\n", err)
			os.Exit(1)
		}
		*subject = found
	}

	key := os.Getenv("BFF_MIDDLEWARE_SIGNING_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "devtoken: BFF_MIDDLEWARE_SIGNING_KEY is not set, run 'make env' first")
		os.Exit(1)
	}

	now := time.Now()
	claims := middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    middleware.Issuer,
			Subject:   *subject,
			Audience:  jwt.ClaimStrings{middleware.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(*ttl)),
			ID:        fmt.Sprintf("devtoken-%d", now.UnixNano()),
		},
		Scope: strings.Fields(*scope),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(key))
	if err != nil {
		fmt.Fprintf(os.Stderr, "devtoken: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(signed)
}

// firstEntitled picks a usable default from the staff fixture, so this tool has
// no staff identifier baked into it and keeps working when the fixture changes.
func firstEntitled() (string, error) {
	path := os.Getenv("STAFF_FILE")
	if path == "" {
		path = "config/staff.csv"
	}
	dir, err := staffdir.Load(path)
	if err != nil {
		return "", fmt.Errorf("%w (or pass -sub)", err)
	}
	for _, s := range dir.All() {
		if slices.Contains(s.Scopes, middleware.ScopeQuoteGenerate) {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("no staff member in %s holds %s, pass -sub", path, middleware.ScopeQuoteGenerate)
}
