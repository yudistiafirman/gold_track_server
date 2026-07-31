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

type ProductHandler struct {
	productService service.ProductService
}

func NewProductHandler(productService service.ProductService) *ProductHandler {
	return &ProductHandler{productService: productService}
}

// productResponse.ID is the public_id (UUID) — the internal bigint PK is
// never serialized to JSON. CategoryID/BrandID are the same public_id UUIDs
// the client submitted, echoed back.
type productResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	SKU         string    `json:"sku"`
	CategoryID  string    `json:"category_id"`
	BrandID     string    `json:"brand_id"`
	WeightGram  float64   `json:"weight_gram"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func toProductResponse(p service.ProductSummary) productResponse {
	return productResponse{
		ID:          p.PublicID,
		Name:        p.Name,
		SKU:         p.SKU,
		CategoryID:  p.CategoryID,
		BrandID:     p.BrandID,
		WeightGram:  p.WeightGram,
		Description: p.Description,
		IsActive:    p.IsActive,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

type createProductRequest struct {
	Name        string  `json:"name"`
	CategoryID  string  `json:"category_id"`
	BrandID     string  `json:"brand_id"`
	WeightGram  float64 `json:"weight_gram"`
	Description string  `json:"description"`
}

func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperror.BadRequest("invalid request body", err))
		return
	}

	claims, ok := appmw.ClaimsFromContext(r.Context())
	if !ok {
		response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
		return
	}

	result, err := h.productService.Create(r.Context(), service.CreateProductInput{
		Name:              req.Name,
		CategoryPublicID:  req.CategoryID,
		BrandPublicID:     req.BrandID,
		WeightGram:        req.WeightGram,
		Description:       req.Description,
		CreatedByPublicID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusCreated, toProductResponse(result))
}
