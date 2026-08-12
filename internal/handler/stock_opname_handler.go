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
	NotScanned int `json:"not_scanned"`
}

// stockOpnameNotScannedItemResponse is an AVAILABLE stock item not yet
// scanned in an IN_PROGRESS session — sku/weight_gram in particular are
// what let the UI list and group these by weight before Complete.
type stockOpnameNotScannedItemResponse struct {
	StockItemID string  `json:"stock_item_id"`
	Barcode     string  `json:"barcode"`
	SKU         string  `json:"sku"`
	ProductName string  `json:"product_name"`
	WeightGram  float64 `json:"weight_gram"`
}

func toStockOpnameNotScannedItemResponse(it service.NotScannedItem) stockOpnameNotScannedItemResponse {
	return stockOpnameNotScannedItemResponse{
		StockItemID: it.StockItemPublicID,
		Barcode:     it.Barcode,
		SKU:         it.SKU,
		ProductName: it.ProductName,
		WeightGram:  it.WeightGram,
	}
}

func toStockOpnameNotScannedItemResponses(items []service.NotScannedItem) []stockOpnameNotScannedItemResponse {
	out := make([]stockOpnameNotScannedItemResponse, 0, len(items))
	for _, it := range items {
		out = append(out, toStockOpnameNotScannedItemResponse(it))
	}
	return out
}

// stockOpnameWeightGroupResponse is not-yet-scanned units grouped by
// weight_gram — e.g. "5 gram: 3 unit" — instead of one grand total across
// every weight, which isn't meaningful for a physical gold count.
type stockOpnameWeightGroupResponse struct {
	WeightGram      float64 `json:"weight_gram"`
	Count           int     `json:"count"`
	TotalWeightGram float64 `json:"total_weight_gram"`
}

func toStockOpnameWeightGroupResponses(groups []service.NotScannedWeightGroup) []stockOpnameWeightGroupResponse {
	out := make([]stockOpnameWeightGroupResponse, 0, len(groups))
	for _, g := range groups {
		out = append(out, stockOpnameWeightGroupResponse{
			WeightGram:      g.WeightGram,
			Count:           g.Count,
			TotalWeightGram: g.TotalWeightGram,
		})
	}
	return out
}

// stockOpnameResponse.Items is omitted (empty) right after Create — no
// scans yet. Get/Complete always populate it. NotScannedItems/
// NotScannedByWeight follow Summary.NotScanned's own rule — only
// non-empty while the session is IN_PROGRESS.
type stockOpnameResponse struct {
	ID                 string                              `json:"id"`
	OpnameCode         string                              `json:"opname_code"`
	OpnameDate         string                              `json:"opname_date"`
	Status             string                              `json:"status"`
	Notes              string                              `json:"notes"`
	Items              []stockOpnameItemResponse           `json:"items,omitempty"`
	Summary            stockOpnameSummaryResponse          `json:"summary"`
	NotScannedItems    []stockOpnameNotScannedItemResponse `json:"not_scanned_items,omitempty"`
	NotScannedByWeight []stockOpnameWeightGroupResponse    `json:"not_scanned_by_weight,omitempty"`
	CreatedAt          time.Time                           `json:"created_at"`
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
			NotScanned: o.Summary.NotScanned,
		},
		NotScannedItems:    toStockOpnameNotScannedItemResponses(o.NotScannedItems),
		NotScannedByWeight: toStockOpnameWeightGroupResponses(o.NotScannedByWeight),
		CreatedAt:          o.CreatedAt,
	}
}

type stockOpnameListResponse struct {
	Items      []stockOpnameResponse `json:"items"`
	Pagination paginationResponse    `json:"pagination"`
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

// List returns opname sessions header-only (no items[]/summary per row,
// same as purchase order list rows) — fetch Get for a session's full
// detail, including whether it's IN_PROGRESS and resumable via Scan.
func (h *StockOpnameHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.stockOpnameService.List(r.Context(), service.ListStockOpnameInput{
		Status: q.Get("status"),
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]stockOpnameResponse, 0, len(result.Items))
	for _, o := range result.Items {
		items = append(items, toStockOpnameResponse(o))
	}

	response.JSON(w, http.StatusOK, stockOpnameListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
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

// scanStockOpnameResponse embeds the scanned item's fields plus how many
// AVAILABLE units are still unscanned in this session (and what those are,
// grouped by weight), so the scanning screen can show a live "N belum
// discan" — broken down by gramasi — without a separate fetch.
type scanStockOpnameResponse struct {
	stockOpnameItemResponse
	NotScanned         int                                 `json:"not_scanned"`
	NotScannedItems    []stockOpnameNotScannedItemResponse `json:"not_scanned_items,omitempty"`
	NotScannedByWeight []stockOpnameWeightGroupResponse    `json:"not_scanned_by_weight,omitempty"`
}

// Scan returns only the single scanned item (plus the running not-scanned
// count), not the whole session — a cashier scanning one unit at a time
// wants immediate feedback on that scan, not a full re-fetch of every item
// so far.
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

	response.JSON(w, http.StatusOK, scanStockOpnameResponse{
		stockOpnameItemResponse: toStockOpnameItemResponse(result.Item),
		NotScanned:              result.NotScanned,
		NotScannedItems:         toStockOpnameNotScannedItemResponses(result.NotScannedItems),
		NotScannedByWeight:      toStockOpnameWeightGroupResponses(result.NotScannedByWeight),
	})
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
