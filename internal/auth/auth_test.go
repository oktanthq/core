package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-jwt-secret"

func TestVerifyValidToken(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	v := mustVerifier(t, now)
	token := sign(t, testSecret, jwt.MapClaims{
		"sub":   "11111111-1111-1111-1111-111111111111",
		"email": "ada@example.com",
		"role":  "authenticated",
		"aud":   "authenticated",
		"iss":   "https://example.supabase.co/auth/v1",
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	})

	p, err := v.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if p.UserID != "11111111-1111-1111-1111-111111111111" || p.Email != "ada@example.com" {
		t.Fatalf("principal = %+v", p)
	}
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	v := mustVerifier(t, now)

	validClaims := jwt.MapClaims{
		"sub":  "11111111-1111-1111-1111-111111111111",
		"role": "authenticated",
		"aud":  "authenticated",
		"iss":  "https://example.supabase.co/auth/v1",
		"exp":  now.Add(time.Hour).Unix(),
	}

	t.Run("missing", func(t *testing.T) {
		if _, err := v.Verify(""); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("expired", func(t *testing.T) {
		claims := cloneClaims(validClaims)
		claims["exp"] = now.Add(-time.Minute).Unix()
		if _, err := v.Verify(sign(t, testSecret, claims)); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("wrong signature", func(t *testing.T) {
		if _, err := v.Verify(sign(t, "other-secret", validClaims)); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("wrong audience", func(t *testing.T) {
		claims := cloneClaims(validClaims)
		claims["aud"] = "anon"
		if _, err := v.Verify(sign(t, testSecret, claims)); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("anon role", func(t *testing.T) {
		claims := cloneClaims(validClaims)
		claims["role"] = "anon"
		if _, err := v.Verify(sign(t, testSecret, claims)); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("tampered", func(t *testing.T) {
		token := sign(t, testSecret, validClaims) + "x"
		if _, err := v.Verify(token); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestBearer(t *testing.T) {
	if got := Bearer("Bearer abc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := Bearer("bearer abc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := Bearer("abc"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func mustVerifier(t *testing.T, now time.Time) *Verifier {
	t.Helper()
	v, err := NewVerifier(Options{
		JWTSecret: testSecret,
		Issuer:    "https://example.supabase.co/auth/v1",
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return v
}

func sign(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func cloneClaims(in jwt.MapClaims) jwt.MapClaims {
	out := jwt.MapClaims{}
	for k, v := range in {
		out[k] = v
	}
	return out
}
