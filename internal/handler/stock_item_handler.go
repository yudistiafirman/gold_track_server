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

// stockItemProductResponse nests weight_gram under product since it's a
// product attribute (every unit of the same product weighs the same),
// distinct from the bare {id, name} productRefResponse shared by
// category/brand/supplier refs elsewhere.
type stockItemProductResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	WeightGram float64 `json:"weight_gram"`
}

// stockItemSoldToResponse is the counterparty who bought a SOLD unit —
// omitted entirely (nil) for units that aren't currently sold.
type stockItemSoldToResponse struct {
	Type string `json:"type"` // CUSTOMER | SUPPLIER
	ID   string `json:"id"`
	Name string `json:"name"`
}

// stockItemResponse.ID is the public_id (UUID) — the internal bigint PK is
// never serialized to JSON. purchase_date is a plain "2006-01-02" string
// (the column is a DATE, not a TIMESTAMPTZ).
type stockItemResponse struct {
	ID             string                   `json:"id"`
	Product        stockItemProductResponse `json:"product"`
	Barcode        string                   `json:"barcode"`
	SerialNumber   string                   `json:"serial_number"`
	Condition      string                   `json:"condition"`
	PurchasePrice  float64                  `json:"purchase_price"`
	PurchaseDate   string                   `json:"purchase_date"`
	ProductionYear *int                     `json:"production_year"`
	Status         string                   `json:"status"`
	SoldAt         *time.Time               `json:"sold_at"`
	SoldTo         *stockItemSoldToResponse `json:"sold_to,omitempty"`
	Notes          string                   `json:"notes"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
}

func toStockItemResponse(s service.StockItemSummary) stockItemResponse {
	var soldTo *stockItemSoldToResponse
	if s.SoldTo != nil {
		soldTo = &stockItemSoldToResponse{
			Type: s.SoldTo.Type,
			ID:   s.SoldTo.PublicID,
			Name: s.SoldTo.Name,
		}
	}
	return stockItemResponse{
		ID: s.PublicID,
		Product: stockItemProductResponse{
			ID:         s.Product.PublicID,
			Name:       s.Product.Name,
			WeightGram: s.WeightGram,
		},
		Barcode:        s.Barcode,
		SerialNumber:   s.SerialNumber,
		Condition:      s.Condition,
		PurchasePrice:  s.PurchasePrice,
		PurchaseDate:   s.PurchaseDate,
		ProductionYear: s.ProductionYear,
		Status:         s.Status,
		SoldAt:         s.SoldAt,
		SoldTo:         soldTo,
		Notes:          s.Notes,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

type stockItemListResponse struct {
	Items      []stockItemResponse `json:"items"`
	Pagination paginationResponse  `json:"pagination"`
}

type stockItemLookupResponse struct {
	stockItemResponse
	RequiresConfirmation bool `json:"requires_confirmation"`
}

type stockItemLabelResponse struct {
	Barcode      string  `json:"barcode"`
	ProductName  string  `json:"product_name"`
	WeightGram   float64 `json:"weight_gram"`
	SerialNumber string  `json:"serial_number"`
}

type createStockItemRequest struct {
	SerialNumber   string  `json:"serial_number"`
	Condition      string  `json:"condition"`
	PurchasePrice  float64 `json:"purchase_price"`
	PurchaseDate   string  `json:"purchase_date"`
	ProductionYear *int    `json:"production_year"`
	Notes          string  `json:"notes"`
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
		ProductionYear:    req.ProductionYear,
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

// List returns stock items across every product (unlike ListByProduct,
// which is always scoped to one) — backs global browsing views like a
// "Barang Terjual" page (?status=SOLD). ?product_id= optionally narrows to
// one product, same as ListByProduct's path param would.
func (h *StockItemHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.stockItemService.ListAll(r.Context(), service.ListAllStockItemsInput{
		ProductPublicID: q.Get("product_id"),
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

// Lookup finds a stock item by its physical barcode (BE-701), for adding to
// a sale cart. ?type= is optional (BE-703) — when it's "SELL", a BAD
// condition unit sets requires_confirmation so the client can prompt
// before adding it to the cart.
func (h *StockItemHandler) Lookup(w http.ResponseWriter, r *http.Request) {
	barcode := r.URL.Query().Get("barcode")
	if barcode == "" {
		response.Error(w, apperror.BadRequest("barcode wajib diisi", nil))
		return
	}

	result, requiresConfirmation, err := h.stockItemService.Lookup(r.Context(), barcode, r.URL.Query().Get("type"))
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, stockItemLookupResponse{
		stockItemResponse:    toStockItemResponse(result),
		RequiresConfirmation: requiresConfirmation,
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
	SerialNumber   string  `json:"serial_number"`
	Condition      string  `json:"condition"`
	PurchasePrice  float64 `json:"purchase_price"`
	PurchaseDate   string  `json:"purchase_date"`
	ProductionYear *int    `json:"production_year"`
	Notes          string  `json:"notes"`
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
		PublicID:       id,
		SerialNumber:   req.SerialNumber,
		Condition:      req.Condition,
		PurchasePrice:  req.PurchasePrice,
		PurchaseDate:   req.PurchaseDate,
		ProductionYear: req.ProductionYear,
		Notes:          req.Notes,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toStockItemResponse(result))
}

// Delete archives the target stock item (status -> ARCHIVED) rather than
// removing the row — the guard (only AVAILABLE units can be archived) lives
// at the SQL level in the repository. Unlike a hard delete, the row (and
// any transaction_items/stock_opname_items referencing it) stays intact.
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

	response.JSON(w, http.StatusOK, map[string]string{"message": "unit stok diarsipkan"})
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
