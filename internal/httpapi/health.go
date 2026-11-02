package httpapi

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.readyCheck != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.readyCheck(ctx); err != nil {
			s.logger.Error("readiness", "err", err, "request_id", RequestID(r.Context()))
			writeError(w, http.StatusServiceUnavailable, "not_ready", "not ready")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
