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

type PurchaseOrderHandler struct {
	purchaseOrderService service.PurchaseOrderService
}

func NewPurchaseOrderHandler(purchaseOrderService service.PurchaseOrderService) *PurchaseOrderHandler {
	return &PurchaseOrderHandler{purchaseOrderService: purchaseOrderService}
}

type purchaseOrderItemResponse struct {
	ID            string             `json:"id"`
	Product       productRefResponse `json:"product"`
	Quantity      int                `json:"quantity"`
	PurchasePrice float64            `json:"purchase_price"`
}

// receivedUnitResponse is a newly created stock item from a receive call —
// gives the admin the barcode right away to print a label.
type receivedUnitResponse struct {
	StockItemID  string `json:"stock_item_id"`
	Barcode      string `json:"barcode"`
	ProductName  string `json:"product_name"`
	SerialNumber string `json:"serial_number"`
}

// purchaseOrderResponse.Items is omitted (empty) for list rows — only
// Create/Get/Receive populate it. ReceivedUnits is only ever populated by
// Receive's response.
type purchaseOrderResponse struct {
	ID            string                      `json:"id"`
	POCode        string                      `json:"po_code"`
	Supplier      productRefResponse          `json:"supplier"`
	TotalAmount   float64                     `json:"total_amount"`
	Status        string                      `json:"status"`
	Notes         string                      `json:"notes"`
	Items         []purchaseOrderItemResponse `json:"items,omitempty"`
	ReceivedUnits []receivedUnitResponse      `json:"received_units,omitempty"`
	CreatedAt     time.Time                   `json:"created_at"`
	ReceivedAt    *time.Time                  `json:"received_at"`
}

func toPurchaseOrderResponse(po service.PurchaseOrderSummary) purchaseOrderResponse {
	items := make([]purchaseOrderItemResponse, 0, len(po.Items))
	for _, it := range po.Items {
		items = append(items, purchaseOrderItemResponse{
			ID:            it.PublicID,
			Product:       toProductRefResponse(it.Product),
			Quantity:      it.Quantity,
			PurchasePrice: it.PurchasePrice,
		})
	}

	receivedUnits := make([]receivedUnitResponse, 0, len(po.ReceivedUnits))
	for _, u := range po.ReceivedUnits {
		receivedUnits = append(receivedUnits, receivedUnitResponse{
			StockItemID:  u.StockItemPublicID,
			Barcode:      u.Barcode,
			ProductName:  u.ProductName,
			SerialNumber: u.SerialNumber,
		})
	}

	return purchaseOrderResponse{
		ID:            po.PublicID,
		POCode:        po.POCode,
		Supplier:      toProductRefResponse(po.Supplier),
		TotalAmount:   po.TotalAmount,
		Status:        po.Status,
		Notes:         po.Notes,
		Items:         items,
		ReceivedUnits: receivedUnits,
		CreatedAt:     po.CreatedAt,
		ReceivedAt:    po.ReceivedAt,
	}
}

type purchaseOrderListResponse struct {
	Items      []purchaseOrderResponse `json:"items"`
	Pagination paginationResponse      `json:"pagination"`
}

type createPOItemRequest struct {
	ProductID     string  `json:"product_id"`
	Quantity      int     `json:"quantity"`
	PurchasePrice float64 `json:"purchase_price"`
}

type createPORequest struct {
	SupplierID string                `json:"supplier_id"`
	Notes      string                `json:"notes"`
	Items      []createPOItemRequest `json:"items"`
}

func (h *PurchaseOrderHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createPORequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	items := make([]service.CreatePOItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, service.CreatePOItemInput{
			ProductPublicID: it.ProductID,
			Quantity:        it.Quantity,
			PurchasePrice:   it.PurchasePrice,
		})
	}

	result, err := h.purchaseOrderService.Create(r.Context(), service.CreatePOInput{
		SupplierPublicID:  req.SupplierID,
		Notes:             req.Notes,
		Items:             items,
		CreatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toPurchaseOrderResponse(result))
}

func (h *PurchaseOrderHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.purchaseOrderService.List(r.Context(), service.ListPOInput{
		Status: q.Get("status"),
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]purchaseOrderResponse, 0, len(result.Items))
	for _, po := range result.Items {
		items = append(items, toPurchaseOrderResponse(po))
	}

	response.JSON(w, http.StatusOK, purchaseOrderListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

func (h *PurchaseOrderHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.purchaseOrderService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toPurchaseOrderResponse(result))
}

type receivePOItemRequest struct {
	ProductID string   `json:"product_id"`
	Serials   []string `json:"serials"`
	Condition string   `json:"condition"`
}

type receivePORequest struct {
	Items []receivePOItemRequest `json:"items"`
}

func (h *PurchaseOrderHandler) Receive(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req receivePORequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	items := make([]service.ReceivePOItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, service.ReceivePOItemInput{
			ProductPublicID: it.ProductID,
			Serials:         it.Serials,
			Condition:       it.Condition,
		})
	}

	result, err := h.purchaseOrderService.Receive(r.Context(), service.ReceivePOInput{
		PublicID:          id,
		Items:             items,
		CreatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toPurchaseOrderResponse(result))
}

// Cancel transitions a BELUM_DITERIMA PO to DIBATALKAN — guarded at the
// SQL level in the repository (only succeeds if it's still BELUM_DITERIMA).
func (h *PurchaseOrderHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.purchaseOrderService.Cancel(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "purchase order dibatalkan"})
}
