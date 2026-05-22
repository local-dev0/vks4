package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/vks4/vks4/services/control-plane/internal/domain"
	"github.com/vks4/vks4/services/control-plane/internal/usecase"
)

type UserHandlers struct{ users *usecase.Users }

func NewUserHandlers(u *usecase.Users) *UserHandlers { return &UserHandlers{users: u} }

func (h *UserHandlers) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.users.List(r.Context())
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type createUserReq struct {
	Email    string      `json:"email"`
	Password string      `json:"password"`
	Name     string      `json:"name"`
	Role     domain.Role `json:"role"`
}

func (h *UserHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var in createUserReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	u, err := h.users.Create(r.Context(), UserIDFromCtx(r.Context()), usecase.CreateUserInput{
		Email: in.Email, Password: in.Password, Name: in.Name, Role: in.Role,
	})
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

type patchUserReq struct {
	Role     *domain.Role `json:"role,omitempty"`
	Password *string      `json:"password,omitempty"`
	Disabled *bool        `json:"disabled,omitempty"`
}

func (h *UserHandlers) Patch(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	var in patchUserReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	actor := UserIDFromCtx(r.Context())
	if in.Role != nil {
		if err := h.users.SetRole(r.Context(), actor, id, *in.Role); err != nil {
			writeAppError(w, err)
			return
		}
	}
	if in.Password != nil {
		if err := h.users.SetPassword(r.Context(), actor, id, *in.Password); err != nil {
			writeAppError(w, err)
			return
		}
	}
	if in.Disabled != nil {
		if err := h.users.SetDisabled(r.Context(), actor, id, *in.Disabled); err != nil {
			writeAppError(w, err)
			return
		}
	}
	u, err := h.users.Me(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *UserHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad id")
		return
	}
	if err := h.users.Delete(r.Context(), UserIDFromCtx(r.Context()), id); err != nil {
		writeAppError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
