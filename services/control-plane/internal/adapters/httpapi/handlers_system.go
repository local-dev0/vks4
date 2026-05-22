package httpapi

import (
	"net/http"
	"strconv"

	"github.com/vks4/vks4/services/control-plane/internal/usecase"
)

type SystemHandlers struct{ aud *usecase.Auditor }

func NewSystemHandlers(a *usecase.Auditor) *SystemHandlers { return &SystemHandlers{aud: a} }

const ServiceVersion = "0.1.0"

func (h *SystemHandlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": ServiceVersion})
}

func (h *SystemHandlers) Audit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	list, err := h.aud.List(r.Context(), limit, offset)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// MonitoringSystem — упрощённый health roll-up. В первой итерации заглушка
// без реального опроса worker-ов; admin SPA может выводить эти данные.
func (h *SystemHandlers) MonitoringSystem(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":        []any{map[string]any{"id": "control-plane", "role": "control", "status": "up"}},
		"rooms":        0,
		"participants": 0,
		"alerts":       []any{},
	})
}
