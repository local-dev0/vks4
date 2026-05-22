package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
	"github.com/vks4/vks4/services/control-plane/internal/usecase"
)

type RoomHandlers struct{ rooms *usecase.Rooms }

func NewRoomHandlers(r *usecase.Rooms) *RoomHandlers { return &RoomHandlers{rooms: r} }

func (h *RoomHandlers) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, total, err := h.rooms.List(r.Context(), q, limit, offset)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

type roomCreateReq struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Mode            domain.RoomMode `json:"mode"`
	MaxParticipants int             `json:"maxParticipants"`
	WaitingRoom     bool            `json:"waitingRoom"`
	DefaultLayout   *domain.Layout  `json:"defaultLayout"`
}

func (h *RoomHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var in roomCreateReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	room, err := h.rooms.Create(r.Context(), UserIDFromCtx(r.Context()), usecase.CreateRoomInput{
		Name: in.Name, Description: in.Description, Mode: in.Mode,
		MaxParticipants: in.MaxParticipants, WaitingRoom: in.WaitingRoom, DefaultLayout: in.DefaultLayout,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, room)
}

func (h *RoomHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	room, err := h.rooms.Get(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandlers) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	// camelCase → snake_case (white-listed).
	set := map[string]any{}
	for k, v := range raw {
		switch k {
		case "name", "description", "mode":
			set[k] = v
		case "maxParticipants":
			set["max_participants"] = v
		case "waitingRoom":
			set["waiting_room"] = v
		case "defaultLayout":
			set["default_layout"] = v
		}
	}
	room, err := h.rooms.Update(r.Context(), UserIDFromCtx(r.Context()), id, set)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *RoomHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	if err := h.rooms.Delete(r.Context(), UserIDFromCtx(r.Context()), id); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RoomHandlers) Lock(w http.ResponseWriter, r *http.Request)   { h.setLock(w, r, true) }
func (h *RoomHandlers) Unlock(w http.ResponseWriter, r *http.Request) { h.setLock(w, r, false) }

func (h *RoomHandlers) setLock(w http.ResponseWriter, r *http.Request, locked bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	if err := h.rooms.SetLocked(r.Context(), UserIDFromCtx(r.Context()), id, locked); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetSlots — GET текущего slot→peerID mapping комнаты, читается из media-worker.
// Room Control использует это для инициализации state после перезагрузки страницы.
func (h *RoomHandlers) GetSlots(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	slots, err := h.rooms.Slots(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if slots == nil {
		slots = map[string]string{}
	}
	writeJSON(w, http.StatusOK, slots)
}

func (h *RoomHandlers) Slots(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	var raw map[string]string
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	assign := make(map[int]string, len(raw))
	for k, v := range raw {
		var i int
		if _, err := fmt.Sscanf(k, "%d", &i); err == nil {
			assign[i] = v
		}
	}
	if err := h.rooms.AssignSlots(r.Context(), UserIDFromCtx(r.Context()), id, assign); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RoomHandlers) Layout(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	var l domain.Layout
	if err := json.NewDecoder(r.Body).Decode(&l); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	if err := h.rooms.UpdateLayout(r.Context(), UserIDFromCtx(r.Context()), id, l); err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (h *RoomHandlers) Participants(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	pp, err := h.rooms.Participants(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	if pp == nil {
		pp = nil
	}
	writeJSON(w, http.StatusOK, pp)
}

func (h *RoomHandlers) Kick(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	peerID := chi.URLParam(r, "peerId")
	if peerID == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "peerId required")
		return
	}
	if err := h.rooms.Kick(r.Context(), UserIDFromCtx(r.Context()), id, peerID); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
