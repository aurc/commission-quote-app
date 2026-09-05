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
//	make token
//	make token ARGS='-sub staff-002'      # a subject with no entitlement
//	make token ARGS='-scope ""'           # a token that does not request the scope
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/aurc/commission-quote-app/internal/middleware"
)

func main() {
	subject := flag.String("sub", middleware.SeedEntitledStaff, "staff subject to issue the token for")
	scope := flag.String("scope", middleware.ScopeQuoteGenerate, "space separated scopes to request, empty for none")
	ttl := flag.Duration("ttl", time.Minute, "how long the token is valid for")
	flag.Parse()

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
