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

type SettingsHandler struct {
	settingsService service.SettingsService
}

func NewSettingsHandler(settingsService service.SettingsService) *SettingsHandler {
	return &SettingsHandler{settingsService: settingsService}
}

type settingResponse struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toSettingResponse(s service.SettingSummary) settingResponse {
	return settingResponse{
		Key:         s.Key,
		Value:       s.Value,
		Description: s.Description,
		UpdatedAt:   s.UpdatedAt,
	}
}

func (h *SettingsHandler) List(w http.ResponseWriter, r *http.Request) {
	settings, err := h.settingsService.ListShopSettings(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}

	list := make([]settingResponse, 0, len(settings))
	for _, s := range settings {
		list = append(list, toSettingResponse(s))
	}
	response.JSON(w, http.StatusOK, list)
}

// updateSettingsRequest fields are pointers so an omitted field leaves that
// setting untouched, distinct from an explicit empty string (rejected).
type updateSettingsRequest struct {
	ShopName    *string `json:"shop_name"`
	ShopAddress *string `json:"shop_address"`
	ShopPhone   *string `json:"shop_phone"`
}

func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	settings, err := h.settingsService.UpdateShopSettings(r.Context(), service.UpdateShopSettingsInput{
		ShopName:          req.ShopName,
		ShopAddress:       req.ShopAddress,
		ShopPhone:         req.ShopPhone,
		UpdatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	list := make([]settingResponse, 0, len(settings))
	for _, s := range settings {
		list = append(list, toSettingResponse(s))
	}
	response.JSON(w, http.StatusOK, list)
}
