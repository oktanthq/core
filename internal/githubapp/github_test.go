package githubapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateInstallationTokenAndResolveCommit(t *testing.T) {
	key := mustKey(t)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	var sawAuth bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ghs_") {
			sawAuth = true
		}
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/access_tokens"):
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"contents":"read"`) {
				t.Fatalf("body = %s", body)
			}
			if !strings.Contains(string(body), `"web"`) {
				t.Fatalf("repo not limited: %s", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"ghs_test_token","expires_at":"2026-11-13T20:00:00Z"}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/commits/"):
			_, _ = w.Write([]byte(`{"sha":"abc123def"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(Options{
		AppID:      "123",
		PrivateKey: string(pemBytes),
		APIURL:     server.URL,
		HTTPClient: server.Client(),
		Now:        func() time.Time { return time.Date(2026, 11, 13, 19, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	access, err := client.CreateInstallationToken(99, "acme/web")
	if err != nil {
		t.Fatalf("CreateInstallationToken: %v", err)
	}
	if access.Token != "ghs_test_token" {
		t.Fatalf("token = %q", access.Token)
	}

	sha, err := client.ResolveCommit(access.Token, "acme/web", "main")
	if err != nil {
		t.Fatalf("ResolveCommit: %v", err)
	}
	if sha != "abc123def" {
		t.Fatalf("sha = %q", sha)
	}
	if !sawAuth {
		t.Fatal("expected bearer token on commit lookup")
	}
}

func TestParseRSAPrivateKeyFormats(t *testing.T) {
	key := mustKey(t)
	der := x509.MarshalPKCS1PrivateKey(key)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
	escaped := strings.ReplaceAll(string(pemBytes), "\n", `\n`)

	if _, err := parseRSAPrivateKey(escaped); err != nil {
		t.Fatalf("escaped: %v", err)
	}
	if _, err := parseRSAPrivateKey(""); err == nil {
		t.Fatal("empty key succeeded")
	}
}

func TestParseInstallationID(t *testing.T) {
	id, err := ParseInstallationID("123456")
	if err != nil || id != 123456 {
		t.Fatalf("id = %d err %v", id, err)
	}
	if _, err := ParseInstallationID("abc"); err == nil {
		t.Fatal("expected error")
	}
}

func mustKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return key
}
