// Package githubapp mints GitHub App installation tokens.
// Tokens and the App private key must never be logged.
package githubapp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = time.Hour

// Access is a short-lived installation token.
type Access struct {
	Token     string
	ExpiresAt time.Time
}

// Client talks to the GitHub App API.
type Client struct {
	appID      string
	key        *rsa.PrivateKey
	apiURL     string
	webURL     string
	slug       string
	httpClient *http.Client
	now        func() time.Time
}

// Options configure a Client.
type Options struct {
	AppID      string
	PrivateKey string
	APIURL     string
	WebURL     string
	Slug       string
	HTTPClient *http.Client
	Now        func() time.Time
}

// New builds a Client. PrivateKey may be PEM, \n-escaped PEM, or base64 PEM.
func New(opts Options) (*Client, error) {
	appID := strings.TrimSpace(opts.AppID)
	if appID == "" {
		return nil, errors.New("GITHUB_APP_ID is required")
	}
	key, err := parseRSAPrivateKey(opts.PrivateKey)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(strings.TrimSpace(opts.APIURL), "/")
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	webURL := strings.TrimRight(strings.TrimSpace(opts.WebURL), "/")
	if webURL == "" {
		webURL = "https://github.com"
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		appID:      appID,
		key:        key,
		apiURL:     apiURL,
		webURL:     webURL,
		slug:       strings.TrimSpace(opts.Slug),
		httpClient: httpClient,
		now:        now,
	}, nil
}

// InstallURL is the GitHub App installation page with signed state.
func (c *Client) InstallURL(state string) string {
	slug := c.slug
	if slug == "" {
		slug = "app"
	}
	return c.webURL + "/apps/" + slug + "/installations/new?state=" + state
}

// CreateInstallationToken mints a contents:read token limited to one repo.
func (c *Client) CreateInstallationToken(installationID int64, repoFullName string) (Access, error) {
	if c == nil {
		return Access{}, errors.New("github app is not configured")
	}
	if installationID <= 0 {
		return Access{}, errors.New("installation id is required")
	}
	appJWT, err := c.appJWT()
	if err != nil {
		return Access{}, err
	}

	body := map[string]any{
		"permissions": map[string]string{"contents": "read"},
	}
	if name := repoName(repoFullName); name != "" {
		body["repositories"] = []string{name}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Access{}, err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiURL, installationID)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(raw)))
	if err != nil {
		return Access{}, err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Access{}, fmt.Errorf("github installation token: request failed")
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 1<<20)
	respBody, err := io.ReadAll(limited)
	if err != nil {
		return Access{}, fmt.Errorf("github installation token: read failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Access{}, fmt.Errorf("github installation token: status %d", resp.StatusCode)
	}

	var parsed struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil || parsed.Token == "" {
		return Access{}, fmt.Errorf("github installation token: decode failed")
	}
	expires := c.now().Add(tokenTTL)
	if parsed.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, parsed.ExpiresAt); err == nil {
			expires = t
		}
	}
	return Access{Token: parsed.Token, ExpiresAt: expires.UTC()}, nil
}

// ResolveCommit returns the SHA for ref (branch, tag, or SHA) using the installation token.
func (c *Client) ResolveCommit(token, repoFullName, ref string) (string, error) {
	if c == nil {
		return "", errors.New("github app is not configured")
	}
	if token == "" || repoFullName == "" || ref == "" {
		return "", errors.New("commit lookup requires token, repo, and ref")
	}
	url := fmt.Sprintf("%s/repos/%s/commits/%s", c.apiURL, repoFullName, ref)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github commit: request failed")
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("github commit: read failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github commit: status %d", resp.StatusCode)
	}
	var parsed struct {
		SHA string `json:"sha"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil || parsed.SHA == "" {
		return "", fmt.Errorf("github commit: decode failed")
	}
	return parsed.SHA, nil
}

func (c *Client) appJWT() (string, error) {
	now := c.now().UTC()
	claims := jwt.MapClaims{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": c.appID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(c.key)
	if err != nil {
		return "", fmt.Errorf("github app jwt")
	}
	return signed, nil
}

func repoName(fullName string) string {
	_, name, ok := strings.Cut(fullName, "/")
	if !ok {
		return strings.TrimSpace(fullName)
	}
	return strings.TrimSpace(name)
}

func parseRSAPrivateKey(raw string) (*rsa.PrivateKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("GITHUB_APP_PRIVATE_KEY is required")
	}
	raw = strings.ReplaceAll(raw, `\n`, "\n")
	der := []byte(raw)
	block, _ := pem.Decode(der)
	if block == nil {
		decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(raw, "\n", ""))
		if err != nil {
			return nil, errors.New("GITHUB_APP_PRIVATE_KEY is not valid PEM")
		}
		block, _ = pem.Decode(decoded)
		if block == nil {
			key, err := parsePKCS(decoded)
			if err != nil {
				return nil, errors.New("GITHUB_APP_PRIVATE_KEY is not valid PEM")
			}
			return key, nil
		}
		der = decoded
	}
	key, err := parsePKCS(block.Bytes)
	if err != nil {
		return nil, errors.New("GITHUB_APP_PRIVATE_KEY is not a valid RSA key")
	}
	return key, nil
}

func parsePKCS(der []byte) (*rsa.PrivateKey, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not rsa")
	}
	return key, nil
}

// CloneURL builds the HTTPS git URL for a GitHub repo.
func CloneURL(webURL, fullName string) string {
	webURL = strings.TrimRight(webURL, "/")
	if webURL == "" {
		webURL = "https://github.com"
	}
	return webURL + "/" + strings.Trim(fullName, "/") + ".git"
}

// ParseInstallationID converts a stored external_account_id to an int64.
func ParseInstallationID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("installation id is missing")
	}
	var id int64
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, errors.New("installation id is invalid")
		}
		id = id*10 + int64(r-'0')
	}
	if id == 0 {
		return 0, errors.New("installation id is invalid")
	}
	return id, nil
}
