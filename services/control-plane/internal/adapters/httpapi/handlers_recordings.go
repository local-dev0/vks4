package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/usecase"
)

type RecordingHandlers struct{ rec *usecase.Recordings }

func NewRecordingHandlers(r *usecase.Recordings) *RecordingHandlers {
	return &RecordingHandlers{rec: r}
}

func (h *RecordingHandlers) Start(w http.ResponseWriter, r *http.Request) {
	roomID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	rec, err := h.rec.Start(r.Context(), UserIDFromCtx(r.Context()), roomID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *RecordingHandlers) Stop(w http.ResponseWriter, r *http.Request) {
	roomID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	rec, err := h.rec.Stop(r.Context(), UserIDFromCtx(r.Context()), roomID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *RecordingHandlers) List(w http.ResponseWriter, r *http.Request) {
	var roomID *uuid.UUID
	if v := r.URL.Query().Get("roomId"); v != "" {
		id, err := uuid.Parse(v)
		if err == nil {
			roomID = &id
		}
	}
	list, err := h.rec.List(r.Context(), roomID)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *RecordingHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	rec, err := h.rec.Get(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *RecordingHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	if err := h.rec.Delete(r.Context(), UserIDFromCtx(r.Context()), id); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RecordingHandlers) Download(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	rec, err := h.rec.Get(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if rec.URL == "" {
		writeError(w, http.StatusNotFound, "not_found", "recording not yet finalized")
		return
	}
	http.Redirect(w, r, rec.URL, http.StatusFound)
}
