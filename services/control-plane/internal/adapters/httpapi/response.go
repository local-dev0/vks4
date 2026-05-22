package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	apperr "github.com/vks4/vks4/services/control-plane/internal/pkg/errors"
)

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

type errBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeError(w http.ResponseWriter, code int, ecode, msg string) {
	writeJSON(w, code, errBody{Code: ecode, Message: msg})
}

func writeAppError(w http.ResponseWriter, err error) {
	if ae, ok := apperr.As(err); ok {
		writeJSON(w, apperr.HTTPStatus(ae.Code), errBody{Code: string(ae.Code), Message: ae.Message, Details: ae.Details})
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", err.Error())
}

func chiRoutePattern(r *http.Request) string {
	rc := chi.RouteContext(r.Context())
	if rc == nil || rc.RoutePattern() == "" {
		return r.URL.Path
	}
	return rc.RoutePattern()
}
