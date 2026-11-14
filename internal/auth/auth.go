// Package auth verifies Supabase access tokens and attaches the caller to context.
package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Principal is the authenticated caller.
type Principal struct {
	UserID string
	Email  string
	Role   string
}

type contextKey int

const principalKey contextKey = 1

var (
	errMissingSecret = errors.New("jwt secret is not configured")
	errInvalidToken  = errors.New("invalid token")
)

// Verifier checks Supabase HS256 access tokens.
type Verifier struct {
	secret   []byte
	issuer   string
	audience string
	now      func() time.Time
}

// Options configure a Verifier.
type Options struct {
	JWTSecret string
	Issuer    string
	Audience  string
	Now       func() time.Time
}

// NewVerifier returns a Verifier. JWTSecret is required.
func NewVerifier(opts Options) (*Verifier, error) {
	secret := strings.TrimSpace(opts.JWTSecret)
	if secret == "" {
		return nil, errMissingSecret
	}
	audience := opts.Audience
	if audience == "" {
		audience = "authenticated"
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Verifier{
		secret:   []byte(secret),
		issuer:   strings.TrimRight(strings.TrimSpace(opts.Issuer), "/"),
		audience: audience,
		now:      now,
	}, nil
}

// Verify parses and validates a bearer token. The raw token must never be logged.
func (v *Verifier) Verify(token string) (Principal, error) {
	if v == nil || len(v.secret) == 0 {
		return Principal{}, errMissingSecret
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return Principal{}, errInvalidToken
	}

	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errInvalidToken
		}
		return v.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithTimeFunc(v.now))
	if err != nil || parsed == nil || !parsed.Valid {
		return Principal{}, errInvalidToken
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return Principal{}, errInvalidToken
	}

	if err := v.validateClaims(claims); err != nil {
		return Principal{}, err
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return Principal{}, errInvalidToken
	}
	email, _ := claims["email"].(string)
	role, _ := claims["role"].(string)
	if role != "" && role != "authenticated" {
		return Principal{}, errInvalidToken
	}

	return Principal{UserID: sub, Email: email, Role: role}, nil
}

func (v *Verifier) validateClaims(claims jwt.MapClaims) error {
	now := v.now()
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil || !exp.After(now) {
		return errInvalidToken
	}
	if v.issuer != "" {
		iss, _ := claims["iss"].(string)
		if iss != v.issuer && iss != v.issuer+"/auth/v1" {
			return errInvalidToken
		}
	}
	if v.audience != "" {
		aud, err := claims.GetAudience()
		if err != nil {
			return errInvalidToken
		}
		if !audContains(aud, v.audience) {
			return errInvalidToken
		}
	}
	return nil
}

func audContains(aud jwt.ClaimStrings, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

// WithPrincipal stores p on ctx.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

// PrincipalFrom returns the caller stored by middleware.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey).(Principal)
	return p, ok
}

// IssuerFromURL builds the Supabase Auth issuer from the project URL.
func IssuerFromURL(supabaseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(supabaseURL), "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/auth/v1") {
		return base
	}
	return base + "/auth/v1"
}

// Bearer extracts the token from an Authorization header.
func Bearer(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
