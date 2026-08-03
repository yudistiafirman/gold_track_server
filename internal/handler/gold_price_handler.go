package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/response"
)

type GoldPriceHandler struct {
	goldPriceService service.GoldPriceService
}

func NewGoldPriceHandler(goldPriceService service.GoldPriceService) *GoldPriceHandler {
	return &GoldPriceHandler{goldPriceService: goldPriceService}
}

type goldPriceResponse struct {
	Source         string    `json:"source"`
	PriceBuy       float64   `json:"price_buy"`
	PriceSell      float64   `json:"price_sell"`
	PriceReference float64   `json:"price_reference"`
	FetchedAt      time.Time `json:"fetched_at"`
}

// goldPriceEnvelope mirrors pkg/response.Envelope, except Data is a
// concrete *goldPriceResponse (no omitempty) — AC#4 requires an explicit
// {"data": null} when no price has synced yet, not an omitted field.
type goldPriceEnvelope struct {
	Success bool               `json:"success"`
	Data    *goldPriceResponse `json:"data"`
}

func (h *GoldPriceHandler) GetActive(w http.ResponseWriter, r *http.Request) {
	active, err := h.goldPriceService.GetActive(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}

	var data *goldPriceResponse
	if active != nil {
		data = &goldPriceResponse{
			Source:         active.Source,
			PriceBuy:       active.PriceBuy,
			PriceSell:      active.PriceSell,
			PriceReference: active.PriceReference,
			FetchedAt:      active.FetchedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(goldPriceEnvelope{Success: true, Data: data})
}
