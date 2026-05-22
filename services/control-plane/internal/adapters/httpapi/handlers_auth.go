package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/vks4/vks4/services/control-plane/internal/usecase"
)

type AuthHandlers struct {
	auth  *usecase.Auth
	users *usecase.Users
}

func NewAuthHandlers(a *usecase.Auth, u *usecase.Users) *AuthHandlers { return &AuthHandlers{auth: a, users: u} }

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokensResp struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	ExpiresIn int    `json:"expiresIn"`
}

func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var in loginReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	t, _, err := h.auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokensResp{Access: t.Access, Refresh: t.Refresh, ExpiresIn: t.ExpiresIn})
}

type refreshReq struct {
	Refresh string `json:"refresh"`
}

func (h *AuthHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var in refreshReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad json")
		return
	}
	t, err := h.auth.Refresh(r.Context(), in.Refresh)
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokensResp{Access: t.Access, Refresh: t.Refresh, ExpiresIn: t.ExpiresIn})
}

func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	u, err := h.users.Me(r.Context(), UserIDFromCtx(r.Context()))
	if err != nil {
		writeAppError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	// Stateless: клиент должен удалить access; refresh — может быть отозван через /auth/refresh с пустым.
	w.WriteHeader(http.StatusNoContent)
}
