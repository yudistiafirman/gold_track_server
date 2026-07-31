package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	appmw "gold-track-be/internal/middleware"
	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type ExpenseHandler struct {
	expenseService service.ExpenseService
}

func NewExpenseHandler(expenseService service.ExpenseService) *ExpenseHandler {
	return &ExpenseHandler{expenseService: expenseService}
}

type expenseResponse struct {
	ID          string             `json:"id"`
	Category    productRefResponse `json:"category"`
	Amount      float64            `json:"amount"`
	Description string             `json:"description"`
	ExpenseDate string             `json:"expense_date"`
	CreatedAt   time.Time          `json:"created_at"`
}

func toExpenseResponse(e service.ExpenseSummary) expenseResponse {
	return expenseResponse{
		ID:          e.PublicID,
		Category:    toProductRefResponse(e.Category),
		Amount:      e.Amount,
		Description: e.Description,
		ExpenseDate: e.ExpenseDate,
		CreatedAt:   e.CreatedAt,
	}
}

type createExpenseRequest struct {
	CategoryID  string  `json:"category_id"`
	Amount      float64 `json:"amount"`
	Description string  `json:"description"`
	ExpenseDate string  `json:"expense_date"`
}

func (h *ExpenseHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	result, err := h.expenseService.Create(r.Context(), service.CreateExpenseInput{
		CategoryPublicID:  req.CategoryID,
		Amount:            req.Amount,
		Description:       req.Description,
		ExpenseDate:       req.ExpenseDate,
		CreatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toExpenseResponse(result))
}

func (h *ExpenseHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.expenseService.List(r.Context(), service.ListExpensesInput{
		CategoryPublicID: q.Get("category_id"),
		DateFrom:         q.Get("date_from"),
		DateTo:           q.Get("date_to"),
		Page:             page,
		Limit:            limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]expenseResponse, 0, len(result.Items))
	for _, e := range result.Items {
		items = append(items, toExpenseResponse(e))
	}

	response.JSON(w, http.StatusOK, expenseListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

type expenseListResponse struct {
	Items      []expenseResponse  `json:"items"`
	Pagination paginationResponse `json:"pagination"`
}

func (h *ExpenseHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.expenseService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toExpenseResponse(result))
}

type updateExpenseRequest struct {
	CategoryID  string  `json:"category_id"`
	Amount      float64 `json:"amount"`
	Description string  `json:"description"`
	ExpenseDate string  `json:"expense_date"`
}

func (h *ExpenseHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.expenseService.Update(r.Context(), service.UpdateExpenseInput{
		PublicID:         id,
		CategoryPublicID: req.CategoryID,
		Amount:           req.Amount,
		Description:      req.Description,
		ExpenseDate:      req.ExpenseDate,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toExpenseResponse(result))
}

// Delete hard-deletes the target expense — expenses has no is_active
// column either, so there is no soft-delete option here.
func (h *ExpenseHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.expenseService.Delete(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "pengeluaran dihapus"})
}
