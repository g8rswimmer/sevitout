package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/g8rswimmer/sevitout/internal/store"
)

// PasswordHandler serves POST /auth/register and POST /auth/login.
type PasswordHandler struct {
	signer *JWTSigner
	users  store.UserStore
}

// NewPasswordHandler constructs a PasswordHandler.
func NewPasswordHandler(signer *JWTSigner, users store.UserStore) *PasswordHandler {
	return &PasswordHandler{signer: signer, users: users}
}

// RegisterRoutes mounts the handler's endpoints onto mux.
func (h *PasswordHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/register", h.register)
	mux.HandleFunc("/auth/login", h.login)
}

// authResponse is the JSON body returned on success.
type authResponse struct {
	Token string     `json:"token"`
	User  publicUser `json:"user"`
}

type publicUser struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	OrgRole string `json:"org_role"`
}

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *PasswordHandler) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = strings.TrimSpace(req.Name)

	if req.Email == "" || req.Name == "" || req.Password == "" {
		http.Error(w, "email, name, and password are required", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	role, err := h.bootstrapRole(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	user := &store.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		Name:         req.Name,
		OrgRole:      role,
		Active:       true,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.users.Create(r.Context(), user); err != nil {
		if errors.Is(err, store.ErrConflict) {
			http.Error(w, "email already registered", http.StatusConflict)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.respondWithToken(w, user)
}

func (h *PasswordHandler) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	user, err := h.users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !user.Active {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	h.respondWithToken(w, user)
}

func (h *PasswordHandler) respondWithToken(w http.ResponseWriter, user *store.User) {
	token, err := h.signer.Sign(user.ID, user.Email, string(user.OrgRole))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	body, err := json.Marshal(authResponse{
		Token: token,
		User: publicUser{
			ID:      user.ID,
			Email:   user.Email,
			Name:    user.Name,
			OrgRole: string(user.OrgRole),
		},
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    token,
		HttpOnly: true,
		Secure:   true,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// bootstrapRole returns Admin for the first registered user, Viewer for all others.
func (h *PasswordHandler) bootstrapRole(ctx context.Context) (store.OrgRole, error) {
	n, err := h.users.Count(ctx)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return store.OrgRoleAdmin, nil
	}
	return store.OrgRoleViewer, nil
}
