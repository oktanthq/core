package httpapi

import (
	"errors"
	"net/http"

	"github.com/oktanthq/core/internal/auth"
	"github.com/oktanthq/core/internal/platform"
)

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.verifier == nil {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "authentication is not configured")
			return
		}
		raw := auth.Bearer(r.Header.Get("Authorization"))
		if raw == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		principal, err := s.verifier.Verify(raw)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid token")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func (s *Server) requirePlatform(w http.ResponseWriter) bool {
	if s.platform == nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "database is not configured")
		return false
	}
	return true
}

func (s *Server) requireMember(w http.ResponseWriter, r *http.Request, orgID, minRole string) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
		return auth.Principal{}, false
	}
	if !s.requirePlatform(w) {
		return auth.Principal{}, false
	}
	role, err := s.platform.MemberRole(r.Context(), orgID, principal.UserID)
	if err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			writeError(w, http.StatusForbidden, "forbidden", "not an organization member")
			return auth.Principal{}, false
		}
		writeStoreError(w, s.logger, err)
		return auth.Principal{}, false
	}
	if !platform.RoleAtLeast(role, minRole) {
		writeError(w, http.StatusForbidden, "forbidden", "insufficient organization role")
		return auth.Principal{}, false
	}
	return principal, true
}
