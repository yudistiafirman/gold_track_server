package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type CustomerHandler struct {
	customerService service.CustomerService
}

func NewCustomerHandler(customerService service.CustomerService) *CustomerHandler {
	return &CustomerHandler{customerService: customerService}
}

// customerResponse.ID is the public_id (UUID) — the internal bigint PK is
// never serialized to JSON.
type customerResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	IDType    string    `json:"id_type"`
	IDNumber  string    `json:"id_number"`
	Address   string    `json:"address"`
	Notes     string    `json:"notes"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toCustomerResponse(c service.CustomerSummary) customerResponse {
	return customerResponse{
		ID:        c.PublicID,
		Name:      c.Name,
		Phone:     c.Phone,
		Email:     c.Email,
		IDType:    c.IDType,
		IDNumber:  c.IDNumber,
		Address:   c.Address,
		Notes:     c.Notes,
		IsActive:  c.IsActive,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

type customerListResponse struct {
	Items      []customerResponse `json:"items"`
	Pagination paginationResponse `json:"pagination"`
}

type createCustomerRequest struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	IDType   string `json:"id_type"`
	IDNumber string `json:"id_number"`
	Address  string `json:"address"`
	Notes    string `json:"notes"`
}

func (h *CustomerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.customerService.Create(r.Context(), service.CreateCustomerInput{
		Name:     req.Name,
		Phone:    req.Phone,
		Email:    req.Email,
		IDType:   req.IDType,
		IDNumber: req.IDNumber,
		Address:  req.Address,
		Notes:    req.Notes,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toCustomerResponse(result))
}

func (h *CustomerHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.customerService.List(r.Context(), service.ListCustomersInput{
		Search: q.Get("search"),
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]customerResponse, 0, len(result.Items))
	for _, c := range result.Items {
		items = append(items, toCustomerResponse(c))
	}

	response.JSON(w, http.StatusOK, customerListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

func (h *CustomerHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.customerService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toCustomerResponse(result))
}

type updateCustomerRequest struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	IDType   string `json:"id_type"`
	IDNumber string `json:"id_number"`
	Address  string `json:"address"`
	Notes    string `json:"notes"`
	IsActive bool   `json:"is_active"`
}

func (h *CustomerHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.customerService.Update(r.Context(), service.UpdateCustomerInput{
		PublicID: id,
		Name:     req.Name,
		Phone:    req.Phone,
		Email:    req.Email,
		IDType:   req.IDType,
		IDNumber: req.IDNumber,
		Address:  req.Address,
		Notes:    req.Notes,
		IsActive: req.IsActive,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toCustomerResponse(result))
}

// Delete soft-deletes (deactivates) the target customer; it never removes
// the row.
func (h *CustomerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.customerService.Deactivate(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "pelanggan dinonaktifkan"})
}
