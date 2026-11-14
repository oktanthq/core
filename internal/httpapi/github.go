package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/oktanthq/core/internal/auth"
	"github.com/oktanthq/core/internal/platform"
)

type connectGitHubRequest struct {
	RedirectTo string `json:"redirect_to"`
}

type connectGitHubResponse struct {
	AuthorizeURL string `json:"authorize_url"`
}

type oauthState struct {
	OrgID      string `json:"org_id"`
	UserID     string `json:"user_id"`
	RedirectTo string `json:"redirect_to"`
	Exp        int64  `json:"exp"`
}

var errInvalidRedirect = errors.New("invalid redirect")

func (s *Server) connectGitHub(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgId")
	if !validUUID(orgID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid organization id")
		return
	}
	if _, ok := s.requireMember(w, r, orgID, platform.RoleAdmin); !ok {
		return
	}
	if s.github == nil || s.stateSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "github app is not configured")
		return
	}

	var body connectGitHubRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(w, r, &body); err != nil {
			writeDecodeError(w, err)
			return
		}
	}
	redirectTo, err := allowedRedirect(body.RedirectTo, s.appOrigin)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "redirect_to is not allowed")
		return
	}

	principal, _ := auth.PrincipalFrom(r.Context())
	state, err := encodeState(s.stateSecret, oauthState{
		OrgID:      orgID,
		UserID:     principal.UserID,
		RedirectTo: redirectTo,
		Exp:        s.now().Add(15 * time.Minute).Unix(),
	})
	if err != nil {
		s.logger.Error("github state", "err", err, "request_id", RequestID(r.Context()))
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, connectGitHubResponse{AuthorizeURL: s.github.InstallURL(url.QueryEscape(state))})
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	if s.platform == nil || s.stateSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "github app is not configured")
		return
	}
	state, err := decodeState(s.stateSecret, r.URL.Query().Get("state"), s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid state")
		return
	}
	installationID := strings.TrimSpace(r.URL.Query().Get("installation_id"))
	if installationID == "" {
		redirectWithGitHub(w, r, state.RedirectTo, s.appOrigin, "error")
		return
	}
	if _, err := s.platform.UpsertGitHubInstallation(r.Context(), platform.GitHubInstallInput{
		OrganizationID: state.OrgID,
		InstallationID: installationID,
	}); err != nil {
		s.logger.Error("github install", "err", err, "request_id", RequestID(r.Context()))
		redirectWithGitHub(w, r, state.RedirectTo, s.appOrigin, "error")
		return
	}
	redirectWithGitHub(w, r, state.RedirectTo, s.appOrigin, "connected")
}

func allowedRedirect(raw, appOrigin string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/", nil
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return "", errInvalidRedirect
	}
	origin, err := url.Parse(appOrigin)
	if err != nil || origin.Host == "" {
		return "", errInvalidRedirect
	}
	if parsed.Scheme != origin.Scheme || !strings.EqualFold(parsed.Host, origin.Host) {
		return "", errInvalidRedirect
	}
	return raw, nil
}

func redirectWithGitHub(w http.ResponseWriter, r *http.Request, redirectTo, appOrigin, status string) {
	target := redirectTo
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = strings.TrimRight(appOrigin, "/") + target
	}
	u, err := url.Parse(target)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": status})
		return
	}
	q := u.Query()
	q.Set("github", status)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func encodeState(secret string, state oauthState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

func decodeState(secret, raw string, now time.Time) (oauthState, error) {
	raw, _ = url.QueryUnescape(raw)
	payload, sig, ok := strings.Cut(raw, ".")
	if !ok || payload == "" || sig == "" {
		return oauthState{}, errInvalidRedirect
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(expected, got) {
		return oauthState{}, errInvalidRedirect
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return oauthState{}, errInvalidRedirect
	}
	var state oauthState
	if err := json.Unmarshal(decoded, &state); err != nil {
		return oauthState{}, errInvalidRedirect
	}
	if state.OrgID == "" || state.Exp < now.Unix() {
		return oauthState{}, errInvalidRedirect
	}
	return state, nil
}
