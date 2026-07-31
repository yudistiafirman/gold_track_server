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

type ProductHandler struct {
	productService service.ProductService
}

func NewProductHandler(productService service.ProductService) *ProductHandler {
	return &ProductHandler{productService: productService}
}

type productRefResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func toProductRefResponse(r service.ProductRef) productRefResponse {
	return productRefResponse{ID: r.PublicID, Name: r.Name}
}

// productResponse.ID is the public_id (UUID) — the internal bigint PK is
// never serialized to JSON. Category/Brand are nested {id, name} so a
// browse/search client doesn't need a second lookup per row.
type productResponse struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	SKU         string             `json:"sku"`
	Category    productRefResponse `json:"category"`
	Brand       productRefResponse `json:"brand"`
	WeightGram  float64            `json:"weight_gram"`
	Description string             `json:"description"`
	IsActive    bool               `json:"is_active"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

func toProductResponse(p service.ProductSummary) productResponse {
	return productResponse{
		ID:          p.PublicID,
		Name:        p.Name,
		SKU:         p.SKU,
		Category:    toProductRefResponse(p.Category),
		Brand:       toProductRefResponse(p.Brand),
		WeightGram:  p.WeightGram,
		Description: p.Description,
		IsActive:    p.IsActive,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

type paginationResponse struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type productListResponse struct {
	Items      []productResponse  `json:"items"`
	Pagination paginationResponse `json:"pagination"`
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

func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	result, err := h.productService.List(r.Context(), service.ListProductsInput{
		Search:           q.Get("search"),
		CategoryPublicID: q.Get("category_id"),
		BrandPublicID:    q.Get("brand_id"),
		Page:             page,
		Limit:            limit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]productResponse, 0, len(result.Items))
	for _, p := range result.Items {
		items = append(items, toProductResponse(p))
	}

	response.JSON(w, http.StatusOK, productListResponse{
		Items: items,
		Pagination: paginationResponse{
			Page:       result.Page,
			Limit:      result.Limit,
			Total:      result.Total,
			TotalPages: result.TotalPages,
		},
	})
}

func (h *ProductHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := publicIDParam(r)
	if err != nil {
		response.Error(w, err)
		return
	}

	result, err := h.productService.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, toProductResponse(result))
}
