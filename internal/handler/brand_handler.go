package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type BrandHandler struct {
	brandService service.BrandService
}

func NewBrandHandler(brandService service.BrandService) *BrandHandler {
	return &BrandHandler{brandService: brandService}
}

// brandResponse.ID is the public_id (UUID) — the internal bigint PK is
// never serialized to JSON.
type brandResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toBrandResponse(b service.BrandSummary) brandResponse {
	return brandResponse{
		ID:        b.PublicID,
		Name:      b.Name,
		IsActive:  b.IsActive,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}

type createBrandRequest struct {
	Name string `json:"name"`
}

func (h *BrandHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createBrandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.brandService.Create(r.Context(), service.CreateBrandInput{Name: req.Name})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toBrandResponse(result))
}

func (h *BrandHandler) List(w http.ResponseWriter, r *http.Request) {
	brands, err := h.brandService.List(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}

	list := make([]brandResponse, 0, len(brands))
	for _, b := range brands {
		list = append(list, toBrandResponse(b))
	}
	response.JSON(w, http.StatusOK, list)
}

func (h *BrandHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.brandService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toBrandResponse(result))
}

type updateBrandRequest struct {
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

func (h *BrandHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	var req updateBrandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	result, err := h.brandService.Update(r.Context(), service.UpdateBrandInput{
		PublicID: id,
		Name:     req.Name,
		IsActive: req.IsActive,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toBrandResponse(result))
}

// Delete soft-deletes (deactivates) the target brand; it never removes the
// row.
func (h *BrandHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	if err := h.brandService.Deactivate(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "brand dinonaktifkan"})
}
