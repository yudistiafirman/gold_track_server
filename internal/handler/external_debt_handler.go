package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type ExternalDebtHandler struct {
	externalDebtService service.ExternalDebtService
}

func NewExternalDebtHandler(externalDebtService service.ExternalDebtService) *ExternalDebtHandler {
	return &ExternalDebtHandler{externalDebtService: externalDebtService}
}

// externalDebtResponse.ID is the public_id (UUID) — the internal bigint PK
// is never serialized to JSON. No is_active/updated_at/status — this
// resource has no history; partial repayment ("cicilan") is modeled by
// Update lowering amount, and an entry is deleted outright once fully paid.
type externalDebtResponse struct {
	ID         string    `json:"id"`
	DebtorName string    `json:"debtor_name"`
	Amount     float64   `json:"amount"`
	CreatedAt  time.Time `json:"created_at"`
}

func toExternalDebtResponse(d service.ExternalDebtSummary) externalDebtResponse {
	return externalDebtResponse{
		ID:         d.PublicID,
		DebtorName: d.DebtorName,
		Amount:     d.Amount,
		CreatedAt:  d.CreatedAt,
	}
}

type createExternalDebtRequest struct {
	DebtorName string  `json:"debtor_name"`
	Amount     float64 `json:"amount"`
}

func (h *ExternalDebtHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createExternalDebtRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.externalDebtService.Create(r.Context(), service.CreateExternalDebtInput{
		DebtorName: req.DebtorName,
		Amount:     req.Amount,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toExternalDebtResponse(result))
}

func (h *ExternalDebtHandler) List(w http.ResponseWriter, r *http.Request) {
	debts, err := h.externalDebtService.List(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}

	list := make([]externalDebtResponse, 0, len(debts))
	for _, d := range debts {
		list = append(list, toExternalDebtResponse(d))
	}
	response.JSON(w, http.StatusOK, list)
}

func (h *ExternalDebtHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.externalDebtService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toExternalDebtResponse(result))
}

type updateExternalDebtRequest struct {
	DebtorName string  `json:"debtor_name"`
	Amount     float64 `json:"amount"`
}

func (h *ExternalDebtHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateExternalDebtRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.externalDebtService.Update(r.Context(), service.UpdateExternalDebtInput{
		PublicID:   id,
		DebtorName: req.DebtorName,
		Amount:     req.Amount,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toExternalDebtResponse(result))
}

// Delete hard-deletes the entry — no history is kept, an entry is removed
// outright once fully paid off (client requirement).
func (h *ExternalDebtHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.externalDebtService.Delete(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "hutang dihapus"})
}
