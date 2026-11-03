package httpapi

import "net/http"

func (s *Server) meta(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "core",
		"version": s.version,
	})
}
