package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type ExternalFundHandler struct {
	externalFundService service.ExternalFundService
}

func NewExternalFundHandler(externalFundService service.ExternalFundService) *ExternalFundHandler {
	return &ExternalFundHandler{externalFundService: externalFundService}
}

// externalFundResponse.ID is the public_id (UUID) — the internal bigint PK
// is never serialized to JSON. No is_active/updated_at/status — this
// resource has no history; an entry is deleted outright once settled.
type externalFundResponse struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Amount      float64   `json:"amount"`
	CreatedAt   time.Time `json:"created_at"`
}

func toExternalFundResponse(f service.ExternalFundSummary) externalFundResponse {
	return externalFundResponse{
		ID:          f.PublicID,
		Description: f.Description,
		Amount:      f.Amount,
		CreatedAt:   f.CreatedAt,
	}
}

type createExternalFundRequest struct {
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
}

func (h *ExternalFundHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createExternalFundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.externalFundService.Create(r.Context(), service.CreateExternalFundInput{
		Description: req.Description,
		Amount:      req.Amount,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toExternalFundResponse(result))
}

func (h *ExternalFundHandler) List(w http.ResponseWriter, r *http.Request) {
	funds, err := h.externalFundService.List(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}

	list := make([]externalFundResponse, 0, len(funds))
	for _, f := range funds {
		list = append(list, toExternalFundResponse(f))
	}
	response.JSON(w, http.StatusOK, list)
}

func (h *ExternalFundHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.externalFundService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toExternalFundResponse(result))
}

type updateExternalFundRequest struct {
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
}

func (h *ExternalFundHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateExternalFundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.externalFundService.Update(r.Context(), service.UpdateExternalFundInput{
		PublicID:    id,
		Description: req.Description,
		Amount:      req.Amount,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toExternalFundResponse(result))
}

// Delete hard-deletes the entry — no history is kept, an entry is removed
// outright once the money is settled (client requirement).
func (h *ExternalFundHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.externalFundService.Delete(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "uang diluar dihapus"})
}
