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
	StockItemID  string  `json:"stock_item_id"`
	Barcode      string  `json:"barcode"`
	SerialNumber string  `json:"serial_number"`
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
	PaymentRef      string                    `json:"payment_ref"`
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
			StockItemID:  it.StockItemPublicID,
			Barcode:      it.Barcode,
			SerialNumber: it.SerialNumber,
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
		PaymentRef:      t.PaymentRef,
		Status:          t.Status,
		Items:           items,
		CreatedAt:       t.CreatedAt,
		CompletedAt:     t.CompletedAt,
	}
}

type createTransactionItemRequest struct {
	StockItemID    string  `json:"stock_item_id"`   // SELL / SELL_SUPPLIER
	ProductID      string  `json:"product_id"`      // BUY
	SerialNumber   string  `json:"serial_number"`   // BUY
	Condition      string  `json:"condition"`       // BUY
	PriceTotal     float64 `json:"price_total"`     // all types
	Confirmed      bool    `json:"confirmed"`       // SELL only
	ProductionYear *int    `json:"production_year"` // BUY, optional
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

	var result service.TransactionSummary
	var err error

	if req.Type == "BUY" {
		items := make([]service.CreateBuyItemInput, 0, len(req.Items))
		for _, it := range req.Items {
			items = append(items, service.CreateBuyItemInput{
				ProductPublicID: it.ProductID,
				SerialNumber:    it.SerialNumber,
				Condition:       it.Condition,
				PriceTotal:      it.PriceTotal,
				ProductionYear:  it.ProductionYear,
			})
		}
		result, err = h.transactionService.CreateBuy(r.Context(), service.CreateBuyInput{
			CustomerPublicID:  req.CustomerID,
			PaymentMethod:     req.PaymentMethod,
			PaymentRef:        req.PaymentRef,
			Notes:             req.Notes,
			Items:             items,
			CreatedByPublicID: claims.UserID,
		})
	} else {
		items := make([]service.CreateSaleItemInput, 0, len(req.Items))
		for _, it := range req.Items {
			items = append(items, service.CreateSaleItemInput{
				StockItemPublicID: it.StockItemID,
				PriceTotal:        it.PriceTotal,
				Confirmed:         it.Confirmed,
			})
		}
		result, err = h.transactionService.CreateSale(r.Context(), service.CreateSaleInput{
			Type:              req.Type,
			CustomerPublicID:  req.CustomerID,
			SupplierPublicID:  req.SupplierID,
			PaymentMethod:     req.PaymentMethod,
			PaymentRef:        req.PaymentRef,
			Notes:             req.Notes,
			Items:             items,
			CreatedByPublicID: claims.UserID,
		})
	}
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toTransactionResponse(result))
}

// transactionSummaryResponse is the header-only row used by the customer
// history list — no nested items, kept light for a paginated view. Full
// detail (with items) is only ever returned by Create and Get.
type transactionSummaryResponse struct {
	ID              string     `json:"id"`
	TransactionCode string     `json:"transaction_code"`
	Type            string     `json:"type"`
	TotalAmount     float64    `json:"total_amount"`
	TotalWeight     float64    `json:"total_weight"`
	PaymentMethod   string     `json:"payment_method"`
	PaymentRef      string     `json:"payment_ref"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	CompletedAt     *time.Time `json:"completed_at"`
}

func toTransactionSummaryResponse(t service.TransactionSummary) transactionSummaryResponse {
	return transactionSummaryResponse{
		ID:              t.PublicID,
		TransactionCode: t.TransactionCode,
		Type:            t.Type,
		TotalAmount:     t.TotalAmount,
		TotalWeight:     t.TotalWeight,
		PaymentMethod:   t.PaymentMethod,
		PaymentRef:      t.PaymentRef,
		Status:          t.Status,
		CreatedAt:       t.CreatedAt,
		CompletedAt:     t.CompletedAt,
	}
}

type transactionListResponse struct {
	Items      []transactionSummaryResponse `json:"items"`
	Pagination paginationResponse           `json:"pagination"`
}

// ListByCustomer returns a customer's SELL/BUY transaction history,
// newest first — BE-602. Lives on TransactionHandler (not CustomerHandler)
// even though it nests under /customers/{id}/transactions, same reasoning
// as StockItemHandler.ListByProduct nesting under /products/{productId}.
func (h *TransactionHandler) ListByCustomer(w http.ResponseWriter, r *http.Request) {
	customerID, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.transactionService.ListByCustomer(r.Context(), service.ListCustomerTransactionsInput{
		CustomerPublicID: customerID,
		Page:             page,
		Limit:            limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]transactionSummaryResponse, 0, len(result.Items))
	for _, t := range result.Items {
		items = append(items, toTransactionSummaryResponse(t))
	}

	response.JSON(w, http.StatusOK, transactionListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

// Get returns one transaction's full detail (with items).
func (h *TransactionHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.transactionService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toTransactionResponse(result))
}

// Cancel reverses a COMPLETED transaction's stock effects and flips it to
// CANCELLED — ADMIN/SUPER_ADMIN only, enforced by RequireRole in router.go.
func (h *TransactionHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.transactionService.Cancel(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toTransactionResponse(result))
}

// receiptPartyResponse is the counterparty (customer or supplier) shown on
// a receipt — nil/omitted when not applicable to the transaction's type.
type receiptPartyResponse struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
}

// receiptStoreResponse is the shop's own display data, sourced from settings.
type receiptStoreResponse struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
}

type receiptResponse struct {
	transactionResponse
	Customer   *receiptPartyResponse `json:"customer,omitempty"`
	Supplier   *receiptPartyResponse `json:"supplier,omitempty"`
	Store      receiptStoreResponse  `json:"store"`
	InvoiceURL string                `json:"invoice_url"`
}

func toReceiptResponse(r service.ReceiptSummary) receiptResponse {
	var customer *receiptPartyResponse
	if r.Customer != nil {
		customer = &receiptPartyResponse{Name: r.Customer.Name, Phone: r.Customer.Phone, Address: r.Customer.Address}
	}
	var supplier *receiptPartyResponse
	if r.Supplier != nil {
		supplier = &receiptPartyResponse{Name: r.Supplier.Name, Phone: r.Supplier.Phone, Address: r.Supplier.Address}
	}

	return receiptResponse{
		transactionResponse: toTransactionResponse(r.TransactionSummary),
		Customer:            customer,
		Supplier:            supplier,
		Store: receiptStoreResponse{
			Name:    r.Store.Name,
			Address: r.Store.Address,
			Phone:   r.Store.Phone,
		},
		InvoiceURL: r.InvoiceURL,
	}
}

// GetReceipt returns a transaction's struk payload (BE-1001) — snapshotted
// items plus counterparty/store display data, for all types (SELL, BUY,
// SELL_SUPPLIER). Rendering (dot matrix/continuous form) and printing are
// entirely the FE print-agent's responsibility; this only returns data.
func (h *TransactionHandler) GetReceipt(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.transactionService.GetReceipt(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toReceiptResponse(result))
}
