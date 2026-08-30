package handler

import (
	"net/http"
	"strconv"
	"time"

	appmw "gold-track-be/internal/middleware"
	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type DailyClosingHandler struct {
	dailyClosingService service.DailyClosingService
}

func NewDailyClosingHandler(dailyClosingService service.DailyClosingService) *DailyClosingHandler {
	return &DailyClosingHandler{dailyClosingService: dailyClosingService}
}

type dailyClosingResponse struct {
	ID             string    `json:"id"`
	ClosingDate    string    `json:"closing_date"`
	TotalBalance   float64   `json:"total_balance"`
	TotalGoldValue float64   `json:"total_gold_value"`
	TotalSaldo     float64   `json:"total_saldo"`
	CreatedAt      time.Time `json:"created_at"`
}

func toDailyClosingResponse(c service.DailyClosingSummary) dailyClosingResponse {
	return dailyClosingResponse{
		ID:             c.PublicID,
		ClosingDate:    c.ClosingDate,
		TotalBalance:   c.TotalBalance,
		TotalGoldValue: c.TotalGoldValue,
		TotalSaldo:     c.TotalSaldo,
		CreatedAt:      c.CreatedAt,
	}
}

// Create snapshots today's cash position and records it as the closing
// baseline for the day — 409 if today has already been closed.
func (h *DailyClosingHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	result, err := h.dailyClosingService.Close(r.Context(), service.CloseDailyBalanceInput{
		CreatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toDailyClosingResponse(result))
}

type dailyClosingListResponse struct {
	Items      []dailyClosingResponse `json:"items"`
	Pagination paginationResponse     `json:"pagination"`
}

func (h *DailyClosingHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.dailyClosingService.List(r.Context(), service.ListDailyClosingsInput{
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]dailyClosingResponse, 0, len(result.Items))
	for _, c := range result.Items {
		items = append(items, toDailyClosingResponse(c))
	}

	response.JSON(w, http.StatusOK, dailyClosingListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

func (h *DailyClosingHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.dailyClosingService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toDailyClosingResponse(result))
}
