package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type ExpenseCategoryHandler struct {
	expenseCategoryService service.ExpenseCategoryService
}

func NewExpenseCategoryHandler(expenseCategoryService service.ExpenseCategoryService) *ExpenseCategoryHandler {
	return &ExpenseCategoryHandler{expenseCategoryService: expenseCategoryService}
}

// expenseCategoryResponse.ID is the public_id (UUID) — the internal bigint
// PK is never serialized to JSON. No is_active/updated_at — unlike
// categories/brands, expense_categories has neither column.
type expenseCategoryResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func toExpenseCategoryResponse(c service.ExpenseCategorySummary) expenseCategoryResponse {
	return expenseCategoryResponse{
		ID:        c.PublicID,
		Name:      c.Name,
		CreatedAt: c.CreatedAt,
	}
}

type createExpenseCategoryRequest struct {
	Name string `json:"name"`
}

func (h *ExpenseCategoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createExpenseCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.expenseCategoryService.Create(r.Context(), service.CreateExpenseCategoryInput{Name: req.Name})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toExpenseCategoryResponse(result))
}

func (h *ExpenseCategoryHandler) List(w http.ResponseWriter, r *http.Request) {
	categories, err := h.expenseCategoryService.List(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}

	list := make([]expenseCategoryResponse, 0, len(categories))
	for _, c := range categories {
		list = append(list, toExpenseCategoryResponse(c))
	}
	response.JSON(w, http.StatusOK, list)
}

func (h *ExpenseCategoryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.expenseCategoryService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toExpenseCategoryResponse(result))
}

type updateExpenseCategoryRequest struct {
	Name string `json:"name"`
}

func (h *ExpenseCategoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateExpenseCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.expenseCategoryService.Update(r.Context(), service.UpdateExpenseCategoryInput{
		PublicID: id,
		Name:     req.Name,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toExpenseCategoryResponse(result))
}

// Delete hard-deletes the target category — unlike categories/brands, there
// is no is_active column to soft-deactivate. Rejected with 409 if an
// expense still references this category (enforced at the DB level).
func (h *ExpenseCategoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.expenseCategoryService.Delete(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "kategori pengeluaran dihapus"})
}
