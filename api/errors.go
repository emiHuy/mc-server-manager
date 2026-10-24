package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/emiHuy/mc-server-manager/minecraft"
)

type errorResponse struct {
	Error string `json:"error"`
}

type apiError struct {
	status  int
	message string
}

var (
	errMethodNotAllowed     = &apiError{http.StatusMethodNotAllowed, "method not allowed"}
	errNotFound             = &apiError{http.StatusNotFound, "not found"}
	errBadRequest           = &apiError{http.StatusBadRequest, "invalid request body"}
	errUnsupportedMediaType = &apiError{http.StatusUnsupportedMediaType, "content type must be application/json"}
	errInvalidLines         = &apiError{http.StatusBadRequest, "lines must be an integer from 1 to 1000"}
)

func (err *apiError) Error() string {
	return err.message
}

// statusFor maps an error to an HTTP status
func statusFor(err error) int {
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		return ae.status
	case errors.Is(err, minecraft.ErrInvalidState):
		return http.StatusConflict
	case errors.Is(err, minecraft.ErrInvalidCommand):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := statusFor(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		slog.Error("request failed", "path", r.URL.Path, "error", err)
		message = "internal error"
	}
	writeJSON(w, status, errorResponse{Error: message})
}
