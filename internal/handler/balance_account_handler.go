package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type BalanceAccountHandler struct {
	balanceAccountService service.BalanceAccountService
}

func NewBalanceAccountHandler(balanceAccountService service.BalanceAccountService) *BalanceAccountHandler {
	return &BalanceAccountHandler{balanceAccountService: balanceAccountService}
}

// balanceAccountResponse.ID is the public_id (UUID) — the internal bigint PK
// is never serialized to JSON. No is_active/updated_at — this resource has
// no history/audit trail (client requirement).
type balanceAccountResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Balance   float64   `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
}

func toBalanceAccountResponse(a service.BalanceAccountSummary) balanceAccountResponse {
	return balanceAccountResponse{
		ID:        a.PublicID,
		Name:      a.Name,
		Balance:   a.Balance,
		CreatedAt: a.CreatedAt,
	}
}

type createBalanceAccountRequest struct {
	Name    string  `json:"name"`
	Balance float64 `json:"balance"`
}

func (h *BalanceAccountHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createBalanceAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.balanceAccountService.Create(r.Context(), service.CreateBalanceAccountInput{
		Name:    req.Name,
		Balance: req.Balance,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toBalanceAccountResponse(result))
}

func (h *BalanceAccountHandler) List(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.balanceAccountService.List(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}

	list := make([]balanceAccountResponse, 0, len(accounts))
	for _, a := range accounts {
		list = append(list, toBalanceAccountResponse(a))
	}
	response.JSON(w, http.StatusOK, list)
}

func (h *BalanceAccountHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.balanceAccountService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toBalanceAccountResponse(result))
}

type updateBalanceAccountRequest struct {
	Name    string  `json:"name"`
	Balance float64 `json:"balance"`
}

func (h *BalanceAccountHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateBalanceAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.balanceAccountService.Update(r.Context(), service.UpdateBalanceAccountInput{
		PublicID: id,
		Name:     req.Name,
		Balance:  req.Balance,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toBalanceAccountResponse(result))
}

// Delete hard-deletes the target account — there is no is_active column to
// soft-deactivate, and no other resource can reference a balance account, so
// there is no "in use" conflict case.
func (h *BalanceAccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.balanceAccountService.Delete(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "saldo dihapus"})
}
