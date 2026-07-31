package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	appmw "gold-track-be/internal/middleware"
	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type StockItemHandler struct {
	stockItemService service.StockItemService
}

func NewStockItemHandler(stockItemService service.StockItemService) *StockItemHandler {
	return &StockItemHandler{stockItemService: stockItemService}
}

// stockItemResponse.ID is the public_id (UUID) — the internal bigint PK is
// never serialized to JSON. purchase_date is a plain "2006-01-02" string
// (the column is a DATE, not a TIMESTAMPTZ).
type stockItemResponse struct {
	ID            string             `json:"id"`
	Product       productRefResponse `json:"product"`
	Barcode       string             `json:"barcode"`
	SerialNumber  string             `json:"serial_number"`
	Condition     string             `json:"condition"`
	PurchasePrice float64            `json:"purchase_price"`
	PurchaseDate  string             `json:"purchase_date"`
	Status        string             `json:"status"`
	SoldAt        *time.Time         `json:"sold_at"`
	Notes         string             `json:"notes"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

func toStockItemResponse(s service.StockItemSummary) stockItemResponse {
	return stockItemResponse{
		ID:            s.PublicID,
		Product:       toProductRefResponse(s.Product),
		Barcode:       s.Barcode,
		SerialNumber:  s.SerialNumber,
		Condition:     s.Condition,
		PurchasePrice: s.PurchasePrice,
		PurchaseDate:  s.PurchaseDate,
		Status:        s.Status,
		SoldAt:        s.SoldAt,
		Notes:         s.Notes,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

type stockItemListResponse struct {
	Items      []stockItemResponse `json:"items"`
	Pagination paginationResponse  `json:"pagination"`
}

type stockItemLabelResponse struct {
	Barcode      string  `json:"barcode"`
	ProductName  string  `json:"product_name"`
	WeightGram   float64 `json:"weight_gram"`
	SerialNumber string  `json:"serial_number"`
}

type createStockItemRequest struct {
	SerialNumber  string  `json:"serial_number"`
	Condition     string  `json:"condition"`
	PurchasePrice float64 `json:"purchase_price"`
	PurchaseDate  string  `json:"purchase_date"`
	Notes         string  `json:"notes"`
}

func (h *StockItemHandler) Create(w http.ResponseWriter, r *http.Request) {
	productID, err := productIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req createStockItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	result, err := h.stockItemService.Create(r.Context(), service.CreateStockItemInput{
		ProductPublicID:   productID,
		SerialNumber:      req.SerialNumber,
		Condition:         req.Condition,
		PurchasePrice:     req.PurchasePrice,
		PurchaseDate:      req.PurchaseDate,
		Notes:             req.Notes,
		CreatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toStockItemResponse(result))
}

func (h *StockItemHandler) ListByProduct(w http.ResponseWriter, r *http.Request) {
	productID, err := productIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.stockItemService.List(r.Context(), service.ListStockItemsInput{
		ProductPublicID: productID,
		Status:          q.Get("status"),
		Condition:       q.Get("condition"),
		Search:          q.Get("search"),
		Page:            page,
		Limit:           limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]stockItemResponse, 0, len(result.Items))
	for _, s := range result.Items {
		items = append(items, toStockItemResponse(s))
	}

	response.JSON(w, http.StatusOK, stockItemListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

func (h *StockItemHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.stockItemService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toStockItemResponse(result))
}

func (h *StockItemHandler) GetLabel(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	label, err := h.stockItemService.GetLabel(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, stockItemLabelResponse{
		Barcode:      label.Barcode,
		ProductName:  label.ProductName,
		WeightGram:   label.WeightGram,
		SerialNumber: label.SerialNumber,
	})
}

type updateStockItemRequest struct {
	SerialNumber  string  `json:"serial_number"`
	Condition     string  `json:"condition"`
	PurchasePrice float64 `json:"purchase_price"`
	PurchaseDate  string  `json:"purchase_date"`
	Notes         string  `json:"notes"`
}

func (h *StockItemHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateStockItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.stockItemService.Update(r.Context(), service.UpdateStockItemInput{
		PublicID:      id,
		SerialNumber:  req.SerialNumber,
		Condition:     req.Condition,
		PurchasePrice: req.PurchasePrice,
		PurchaseDate:  req.PurchaseDate,
		Notes:         req.Notes,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toStockItemResponse(result))
}

// Delete hard-deletes the target stock item — unlike every other resource
// in this API, there is no soft-delete here; the guard (only AVAILABLE
// units can be removed) lives at the SQL level in the repository.
func (h *StockItemHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.stockItemService.Delete(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "unit stok dihapus"})
}

// productIDParam reads chi's "productId" path param — distinct from the
// package-level publicIDParam (which always reads "id") because
// /products/{productId}/stock-items nests under a different param name
// than a stock item's own {id}.
func productIDParam(r *http.Request) (string, error) {
	id := chi.URLParam(r, "productId")
	if !uuidPattern.MatchString(id) {
		return "", apperror.BadRequest("product id tidak valid", nil)
	}
	return id, nil
}
