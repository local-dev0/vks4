package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
	"github.com/vks4/vks4/services/control-plane/internal/usecase"
)

type LayoutHandlers struct{ tpl *usecase.LayoutTemplates }

func NewLayoutHandlers(t *usecase.LayoutTemplates) *LayoutHandlers {
	return &LayoutHandlers{tpl: t}
}

func (h *LayoutHandlers) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.tpl.List(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	if items == nil {
		items = []domain.LayoutTemplate{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *LayoutHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	t, err := h.tpl.Get(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type createTemplateReq struct {
	Name          string                  `json:"name"`
	Width         int                     `json:"width"`
	Height        int                     `json:"height"`
	Cells         []domain.LayoutCell     `json:"cells"`
	Background    domain.LayoutBackground `json:"background"`
	NameBgAlpha   *float64                `json:"nameBgAlpha,omitempty"`
	NameBgColor   *string                 `json:"nameBgColor,omitempty"`
	NameFontSize  *int                    `json:"nameFontSize,omitempty"`
	NameFontColor *string                 `json:"nameFontColor,omitempty"`
}

func (h *LayoutHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var in createTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	t, err := h.tpl.Create(r.Context(), UserIDFromCtx(r.Context()), usecase.CreateLayoutTemplateInput{
		Name: in.Name, Width: in.Width, Height: in.Height,
		Cells: in.Cells, Background: in.Background,
		NameBgAlpha: in.NameBgAlpha, NameBgColor: in.NameBgColor,
		NameFontSize: in.NameFontSize, NameFontColor: in.NameFontColor,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (h *LayoutHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	var set map[string]any
	if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	t, err := h.tpl.Update(r.Context(), UserIDFromCtx(r.Context()), id, set)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *LayoutHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	if err := h.tpl.Delete(r.Context(), UserIDFromCtx(r.Context()), id); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
