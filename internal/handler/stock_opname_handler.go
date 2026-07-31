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

type StockOpnameHandler struct {
	stockOpnameService service.StockOpnameService
}

func NewStockOpnameHandler(stockOpnameService service.StockOpnameService) *StockOpnameHandler {
	return &StockOpnameHandler{stockOpnameService: stockOpnameService}
}

type stockOpnameItemResponse struct {
	ID             string `json:"id"`
	StockItemID    string `json:"stock_item_id"`
	Barcode        string `json:"barcode"`
	ProductName    string `json:"product_name"`
	SystemStatus   string `json:"system_status"`
	PhysicalStatus string `json:"physical_status"`
	Result         string `json:"result"`
}

func toStockOpnameItemResponse(it service.StockOpnameItemSummary) stockOpnameItemResponse {
	return stockOpnameItemResponse{
		ID:             it.PublicID,
		StockItemID:    it.StockItemPublicID,
		Barcode:        it.Barcode,
		ProductName:    it.ProductName,
		SystemStatus:   it.SystemStatus,
		PhysicalStatus: it.PhysicalStatus,
		Result:         it.Result,
	}
}

type stockOpnameSummaryResponse struct {
	Match      int `json:"match"`
	Missing    int `json:"missing"`
	Unexpected int `json:"unexpected"`
}

// stockOpnameResponse.Items is omitted (empty) right after Create — no
// scans yet. Get/Complete always populate it.
type stockOpnameResponse struct {
	ID         string                     `json:"id"`
	OpnameCode string                     `json:"opname_code"`
	OpnameDate string                     `json:"opname_date"`
	Status     string                     `json:"status"`
	Notes      string                     `json:"notes"`
	Items      []stockOpnameItemResponse  `json:"items,omitempty"`
	Summary    stockOpnameSummaryResponse `json:"summary"`
	CreatedAt  time.Time                  `json:"created_at"`
}

func toStockOpnameResponse(o service.StockOpnameSummary) stockOpnameResponse {
	items := make([]stockOpnameItemResponse, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, toStockOpnameItemResponse(it))
	}
	return stockOpnameResponse{
		ID:         o.PublicID,
		OpnameCode: o.OpnameCode,
		OpnameDate: o.OpnameDate,
		Status:     o.Status,
		Notes:      o.Notes,
		Items:      items,
		Summary: stockOpnameSummaryResponse{
			Match:      o.Summary.Match,
			Missing:    o.Summary.Missing,
			Unexpected: o.Summary.Unexpected,
		},
		CreatedAt: o.CreatedAt,
	}
}

type createStockOpnameRequest struct {
	Notes string `json:"notes"`
}

func (h *StockOpnameHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createStockOpnameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	result, err := h.stockOpnameService.Create(r.Context(), service.CreateStockOpnameInput{
		Notes:             req.Notes,
		CreatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toStockOpnameResponse(result))
}

func (h *StockOpnameHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.stockOpnameService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toStockOpnameResponse(result))
}

type scanStockOpnameRequest struct {
	Barcode string `json:"barcode"`
}

// Scan returns only the single scanned item, not the whole session — a
// cashier scanning one unit at a time wants immediate feedback on that
// scan, not a full re-fetch of every item so far.
func (h *StockOpnameHandler) Scan(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req scanStockOpnameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.stockOpnameService.Scan(r.Context(), id, req.Barcode)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toStockOpnameItemResponse(result))
}

func (h *StockOpnameHandler) Complete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.stockOpnameService.Complete(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toStockOpnameResponse(result))
}
