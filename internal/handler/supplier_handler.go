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

type SupplierHandler struct {
	supplierService service.SupplierService
}

func NewSupplierHandler(supplierService service.SupplierService) *SupplierHandler {
	return &SupplierHandler{supplierService: supplierService}
}

// supplierResponse.ID is the public_id (UUID) — the internal bigint PK is
// never serialized to JSON.
type supplierResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Address   string    `json:"address"`
	Notes     string    `json:"notes"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toSupplierResponse(s service.SupplierSummary) supplierResponse {
	return supplierResponse{
		ID:        s.PublicID,
		Name:      s.Name,
		Phone:     s.Phone,
		Address:   s.Address,
		Notes:     s.Notes,
		IsActive:  s.IsActive,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

type supplierListResponse struct {
	Items      []supplierResponse `json:"items"`
	Pagination paginationResponse `json:"pagination"`
}

type createSupplierRequest struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
	Notes   string `json:"notes"`
}

func (h *SupplierHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createSupplierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.supplierService.Create(r.Context(), service.CreateSupplierInput{
		Name:    req.Name,
		Phone:   req.Phone,
		Address: req.Address,
		Notes:   req.Notes,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toSupplierResponse(result))
}

func (h *SupplierHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.supplierService.List(r.Context(), service.ListSuppliersInput{
		Search: q.Get("search"),
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]supplierResponse, 0, len(result.Items))
	for _, s := range result.Items {
		items = append(items, toSupplierResponse(s))
	}

	response.JSON(w, http.StatusOK, supplierListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

func (h *SupplierHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.supplierService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toSupplierResponse(result))
}

type updateSupplierRequest struct {
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Address  string `json:"address"`
	Notes    string `json:"notes"`
	IsActive bool   `json:"is_active"`
}

func (h *SupplierHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateSupplierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.supplierService.Update(r.Context(), service.UpdateSupplierInput{
		PublicID: id,
		Name:     req.Name,
		Phone:    req.Phone,
		Address:  req.Address,
		Notes:    req.Notes,
		IsActive: req.IsActive,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toSupplierResponse(result))
}

// Delete soft-deletes (deactivates) the target supplier; it never removes
// the row.
func (h *SupplierHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.supplierService.Deactivate(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "supplier dinonaktifkan"})
}

// supplierHistoryEntryResponse is one row of a supplier's combined
// purchase-order + SELL_SUPPLIER-transaction history — Source tells the
// two kinds of record apart (Code is po_code or transaction_code).
type supplierHistoryEntryResponse struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Code        string    `json:"code"`
	Status      string    `json:"status"`
	TotalAmount float64   `json:"total_amount"`
	CreatedAt   time.Time `json:"created_at"`
}

func toSupplierHistoryEntryResponse(e service.SupplierHistoryEntrySummary) supplierHistoryEntryResponse {
	return supplierHistoryEntryResponse{
		ID:          e.PublicID,
		Source:      e.Source,
		Code:        e.Code,
		Status:      e.Status,
		TotalAmount: e.TotalAmount,
		CreatedAt:   e.CreatedAt,
	}
}

type supplierHistoryListResponse struct {
	Items      []supplierHistoryEntryResponse `json:"items"`
	Pagination paginationResponse             `json:"pagination"`
}

// ListHistory returns a supplier's combined history — purchase orders (the
// shop buying from them) and SELL_SUPPLIER transactions (the shop selling
// back to them) — newest first, mirroring
// TransactionHandler.ListByCustomer's shape. Lives here rather than split
// across PurchaseOrderHandler/TransactionHandler since no single existing
// domain handler owns both underlying tables.
func (h *SupplierHandler) ListHistory(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.supplierService.ListHistory(r.Context(), service.ListSupplierHistoryInput{
		SupplierPublicID: id,
		Page:             page,
		Limit:            limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]supplierHistoryEntryResponse, 0, len(result.Items))
	for _, e := range result.Items {
		items = append(items, toSupplierHistoryEntryResponse(e))
	}

	response.JSON(w, http.StatusOK, supplierHistoryListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}
