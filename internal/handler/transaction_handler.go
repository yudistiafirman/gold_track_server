package handler

import (
	"encoding/json"
	"net/http"
	"time"

	appmw "gold-track-be/internal/middleware"
	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type TransactionHandler struct {
	transactionService service.TransactionService
}

func NewTransactionHandler(transactionService service.TransactionService) *TransactionHandler {
	return &TransactionHandler{transactionService: transactionService}
}

// transactionItemResponse deliberately omits cogs — it's the unit's
// wholesale cost (margin data), not something a checkout response should
// leak to a cashier-facing client.
type transactionItemResponse struct {
	ID           string  `json:"id"`
	ProductName  string  `json:"product_name"`
	WeightGram   float64 `json:"weight_gram"`
	PricePerGram float64 `json:"price_per_gram"`
	PriceTotal   float64 `json:"price_total"`
}

type transactionResponse struct {
	ID              string                    `json:"id"`
	TransactionCode string                    `json:"transaction_code"`
	Type            string                    `json:"type"`
	TotalAmount     float64                   `json:"total_amount"`
	TotalWeight     float64                   `json:"total_weight"`
	PaymentMethod   string                    `json:"payment_method"`
	Status          string                    `json:"status"`
	Items           []transactionItemResponse `json:"items"`
	CreatedAt       time.Time                 `json:"created_at"`
	CompletedAt     *time.Time                `json:"completed_at"`
}

func toTransactionResponse(t service.TransactionSummary) transactionResponse {
	items := make([]transactionItemResponse, 0, len(t.Items))
	for _, it := range t.Items {
		items = append(items, transactionItemResponse{
			ID:           it.PublicID,
			ProductName:  it.ProductName,
			WeightGram:   it.WeightGram,
			PricePerGram: it.PricePerGram,
			PriceTotal:   it.PriceTotal,
		})
	}
	return transactionResponse{
		ID:              t.PublicID,
		TransactionCode: t.TransactionCode,
		Type:            t.Type,
		TotalAmount:     t.TotalAmount,
		TotalWeight:     t.TotalWeight,
		PaymentMethod:   t.PaymentMethod,
		Status:          t.Status,
		Items:           items,
		CreatedAt:       t.CreatedAt,
		CompletedAt:     t.CompletedAt,
	}
}

type createTransactionItemRequest struct {
	StockItemID string  `json:"stock_item_id"`
	PriceTotal  float64 `json:"price_total"`
	Confirmed   bool    `json:"confirmed"`
}

type createTransactionRequest struct {
	Type          string                         `json:"type"`
	CustomerID    string                         `json:"customer_id"`
	SupplierID    string                         `json:"supplier_id"`
	PaymentMethod string                         `json:"payment_method"`
	PaymentRef    string                         `json:"payment_ref"`
	Notes         string                         `json:"notes"`
	Items         []createTransactionItemRequest `json:"items"`
}

func (h *TransactionHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	items := make([]service.CreateSaleItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, service.CreateSaleItemInput{
			StockItemPublicID: it.StockItemID,
			PriceTotal:        it.PriceTotal,
			Confirmed:         it.Confirmed,
		})
	}

	result, err := h.transactionService.CreateSale(r.Context(), service.CreateSaleInput{
		Type:              req.Type,
		CustomerPublicID:  req.CustomerID,
		SupplierPublicID:  req.SupplierID,
		PaymentMethod:     req.PaymentMethod,
		PaymentRef:        req.PaymentRef,
		Notes:             req.Notes,
		Items:             items,
		CreatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toTransactionResponse(result))
}
