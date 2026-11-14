package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/oktanthq/core/internal/platform"
	"github.com/oktanthq/core/internal/project"
)

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Default().Error("encode json response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Error: errorDetail{Code: code, Message: message},
	})
}

func writeStoreError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var plat *platform.Error
	var client *project.Error
	switch {
	case errors.As(err, &plat) && errors.Is(err, platform.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", plat.Message)
	case errors.As(err, &plat) && errors.Is(err, platform.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", plat.Message)
	case errors.As(err, &plat) && errors.Is(err, platform.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", plat.Message)
	case errors.Is(err, platform.ErrNotFound):
		message := "not found"
		if errors.As(err, &plat) && plat.Message != "" {
			message = plat.Message
		}
		writeError(w, http.StatusNotFound, "not_found", message)
	case errors.As(err, &client) && errors.Is(err, project.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", client.Message)
	case errors.As(err, &client) && errors.Is(err, project.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", client.Message)
	case errors.Is(err, project.ErrNotFound):
		message := "project not found"
		if errors.As(err, &client) && client.Message != "" {
			message = client.Message
		}
		writeError(w, http.StatusNotFound, "not_found", message)
	default:
		logger.Error("store", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func validUUID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}
