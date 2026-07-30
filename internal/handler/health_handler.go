package handler

import (
	"net/http"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/response"
)

type HealthHandler struct {
	healthService service.HealthService
}

func NewHealthHandler(healthService service.HealthService) *HealthHandler {
	return &HealthHandler{healthService: healthService}
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	status, err := h.healthService.Check(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, status)
}
