package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	appmw "gold-track-be/internal/middleware"
	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type UserHandler struct {
	userService service.UserService
}

func NewUserHandler(userService service.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

// userResponse.ID is the public_id (UUID) — the internal bigint PK is never
// serialized to JSON.
type userResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	IsActive    bool       `json:"is_active"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func toUserResponse(u service.UserSummary) userResponse {
	return userResponse{
		ID:          u.PublicID,
		Name:        u.Name,
		Email:       u.Email,
		Role:        u.Role,
		IsActive:    u.IsActive,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

type createUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.userService.Create(r.Context(), service.CreateUserInput{
		Name:     req.Name,
		Email:    req.Email,
		Password: req.Password,
		Role:     req.Role,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toUserResponse(result))
}

func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.userService.List(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}

	list := make([]userResponse, 0, len(users))
	for _, u := range users {
		list = append(list, toUserResponse(u))
	}
	response.JSON(w, http.StatusOK, list)
}

func (h *UserHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.userService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toUserResponse(result))
}

type updateUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	IsActive bool   `json:"is_active"`
	Password string `json:"password"`
}

func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.userService.Update(r.Context(), service.UpdateUserInput{
		PublicID: id,
		Name:     req.Name,
		Email:    req.Email,
		Role:     req.Role,
		IsActive: req.IsActive,
		Password: req.Password,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toUserResponse(result))
}

// Delete soft-deletes (deactivates) the target user; it never removes the
// row, since every other table references users.id without ON DELETE CASCADE.
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	if err := h.userService.Deactivate(r.Context(), claims.UserID, id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "user dinonaktifkan"})
}

func publicIDParam(r *http.Request) (string, error) {
	id := chi.URLParam(r, "id")
	if !uuidPattern.MatchString(id) {
		return "", apperror.BadRequest("id tidak valid", nil)
	}
	return id, nil
}
