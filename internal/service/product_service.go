package service

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const (
	defaultProductPage  = 1
	defaultProductLimit = 20
	maxProductLimit     = 100
)

// ProductRef is the nested category/brand identity embedded in
// ProductSummary — a browse/search view needs names, not opaque ids, to be
// useful to a human without a second round trip per row.
type ProductRef struct {
	PublicID string
	Name     string
}

// ProductSummary is the public-facing view of a product: only PublicID
// (UUID) ever leaves this layer, the internal bigint PK stays in
// model.Product/the repository.
type ProductSummary struct {
	PublicID    string
	Name        string
	SKU         string
	Category    ProductRef
	Brand       ProductRef
	WeightGram  float64
	Description string
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CreateProductInput struct {
	Name              string
	CategoryPublicID  string
	BrandPublicID     string
	WeightGram        float64
	Description       string
	CreatedByPublicID string
}

type ListProductsInput struct {
	Search           string
	CategoryPublicID string
	BrandPublicID    string
	Page             int
	Limit            int
}

type ProductListResult struct {
	Items      []ProductSummary
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type UpdateProductInput struct {
	PublicID         string
	Name             string
	CategoryPublicID string
	BrandPublicID    string
	WeightGram       float64
	Description      string
	IsActive         bool
}

type ProductService interface {
	Create(ctx context.Context, input CreateProductInput) (ProductSummary, error)
	List(ctx context.Context, input ListProductsInput) (ProductListResult, error)
	Get(ctx context.Context, publicID string) (ProductSummary, error)
	Update(ctx context.Context, input UpdateProductInput) (ProductSummary, error)
	Deactivate(ctx context.Context, publicID string) error
}

type productService struct {
	productRepo  repository.ProductRepository
	categoryRepo repository.CategoryRepository
	brandRepo    repository.BrandRepository
	userRepo     repository.UserRepository
}

func NewProductService(
	productRepo repository.ProductRepository,
	categoryRepo repository.CategoryRepository,
	brandRepo repository.BrandRepository,
	userRepo repository.UserRepository,
) ProductService {
	return &productService{
		productRepo:  productRepo,
		categoryRepo: categoryRepo,
		brandRepo:    brandRepo,
		userRepo:     userRepo,
	}
}

func (s *productService) Create(ctx context.Context, input CreateProductInput) (ProductSummary, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateProductFields(name, input.CategoryPublicID, input.BrandPublicID, input.WeightGram); err != nil {
		return ProductSummary{}, err
	}

	category, err := s.categoryRepo.FindByPublicID(ctx, input.CategoryPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrCategoryNotFound) {
			return ProductSummary{}, apperror.NotFound("kategori tidak ditemukan", nil)
		}
		return ProductSummary{}, apperror.Internal("failed to fetch category", err)
	}
	if !category.IsActive {
		return ProductSummary{}, apperror.BadRequest("kategori tidak aktif", nil)
	}

	brand, err := s.brandRepo.FindByPublicID(ctx, input.BrandPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrBrandNotFound) {
			return ProductSummary{}, apperror.NotFound("brand tidak ditemukan", nil)
		}
		return ProductSummary{}, apperror.Internal("failed to fetch brand", err)
	}
	if !brand.IsActive {
		return ProductSummary{}, apperror.BadRequest("brand tidak aktif", nil)
	}

	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return ProductSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	var description *string
	if trimmed := strings.TrimSpace(input.Description); trimmed != "" {
		description = &trimmed
	}

	skuPrefix := skuSegment(category.Name) + "-" + skuSegment(brand.Name) + "-" + formatWeight(input.WeightGram) + "-"

	product := &model.Product{
		Name:        name,
		CategoryID:  category.ID,
		BrandID:     brand.ID,
		WeightGram:  input.WeightGram,
		Description: description,
		IsActive:    true,
		CreatedBy:   creator.ID,
	}

	created, err := s.productRepo.CreateWithGeneratedSKU(ctx, product, skuPrefix)
	if err != nil {
		if errors.Is(err, repository.ErrSKUGenerationFailed) {
			return ProductSummary{}, apperror.Conflict("gagal membuat SKU unik, coba lagi", nil)
		}
		return ProductSummary{}, apperror.Internal("failed to create product", err)
	}

	return toProductSummary(created, ProductRef{PublicID: category.PublicID, Name: category.Name}, ProductRef{PublicID: brand.PublicID, Name: brand.Name}), nil
}

func (s *productService) List(ctx context.Context, input ListProductsInput) (ProductListResult, error) {
	page := input.Page
	if page <= 0 {
		page = defaultProductPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultProductLimit
	}
	if limit > maxProductLimit {
		limit = maxProductLimit
	}

	filter := repository.ProductFilter{
		Search: strings.TrimSpace(input.Search),
		Page:   page,
		Limit:  limit,
	}

	if input.CategoryPublicID != "" {
		category, err := s.categoryRepo.FindByPublicID(ctx, input.CategoryPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrCategoryNotFound) {
				return emptyProductList(page, limit), nil
			}
			return ProductListResult{}, apperror.Internal("failed to fetch category", err)
		}
		filter.CategoryID = &category.ID
	}

	if input.BrandPublicID != "" {
		brand, err := s.brandRepo.FindByPublicID(ctx, input.BrandPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrBrandNotFound) {
				return emptyProductList(page, limit), nil
			}
			return ProductListResult{}, apperror.Internal("failed to fetch brand", err)
		}
		filter.BrandID = &brand.ID
	}

	products, total, err := s.productRepo.List(ctx, filter)
	if err != nil {
		return ProductListResult{}, apperror.Internal("failed to list products", err)
	}

	items := make([]ProductSummary, 0, len(products))
	for i := range products {
		items = append(items, toProductSummaryFromRefs(&products[i]))
	}

	return ProductListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *productService) Get(ctx context.Context, publicID string) (ProductSummary, error) {
	product, err := s.productRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrProductNotFound) {
			return ProductSummary{}, apperror.NotFound("produk tidak ditemukan", nil)
		}
		return ProductSummary{}, apperror.Internal("failed to fetch product", err)
	}
	return toProductSummaryFromRefs(product), nil
}

func (s *productService) Update(ctx context.Context, input UpdateProductInput) (ProductSummary, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateProductFields(name, input.CategoryPublicID, input.BrandPublicID, input.WeightGram); err != nil {
		return ProductSummary{}, err
	}

	existing, err := s.productRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrProductNotFound) {
			return ProductSummary{}, apperror.NotFound("produk tidak ditemukan", nil)
		}
		return ProductSummary{}, apperror.Internal("failed to fetch product", err)
	}

	category, err := s.categoryRepo.FindByPublicID(ctx, input.CategoryPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrCategoryNotFound) {
			return ProductSummary{}, apperror.NotFound("kategori tidak ditemukan", nil)
		}
		return ProductSummary{}, apperror.Internal("failed to fetch category", err)
	}
	if !category.IsActive {
		return ProductSummary{}, apperror.BadRequest("kategori tidak aktif", nil)
	}

	brand, err := s.brandRepo.FindByPublicID(ctx, input.BrandPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrBrandNotFound) {
			return ProductSummary{}, apperror.NotFound("brand tidak ditemukan", nil)
		}
		return ProductSummary{}, apperror.Internal("failed to fetch brand", err)
	}
	if !brand.IsActive {
		return ProductSummary{}, apperror.BadRequest("brand tidak aktif", nil)
	}

	var description *string
	if trimmed := strings.TrimSpace(input.Description); trimmed != "" {
		description = &trimmed
	}

	existing.Name = name
	existing.CategoryID = category.ID
	existing.BrandID = brand.ID
	existing.WeightGram = input.WeightGram
	existing.Description = description
	existing.IsActive = input.IsActive

	if err := s.productRepo.Update(ctx, &existing.Product); err != nil {
		if errors.Is(err, repository.ErrProductNotFound) {
			return ProductSummary{}, apperror.NotFound("produk tidak ditemukan", nil)
		}
		return ProductSummary{}, apperror.Internal("failed to update product", err)
	}

	return toProductSummary(
		&existing.Product,
		ProductRef{PublicID: category.PublicID, Name: category.Name},
		ProductRef{PublicID: brand.PublicID, Name: brand.Name},
	), nil
}

func (s *productService) Deactivate(ctx context.Context, publicID string) error {
	existing, err := s.productRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrProductNotFound) {
			return apperror.NotFound("produk tidak ditemukan", nil)
		}
		return apperror.Internal("failed to fetch product", err)
	}

	hasStock, err := s.productRepo.HasAvailableStock(ctx, existing.ID)
	if err != nil {
		return apperror.Internal("failed to check product stock", err)
	}
	if hasStock {
		return apperror.Conflict("produk masih memiliki stok tersedia (AVAILABLE), tidak bisa diarsipkan", nil)
	}

	if err := s.productRepo.SetActive(ctx, publicID, false); err != nil {
		if errors.Is(err, repository.ErrProductNotFound) {
			return apperror.NotFound("produk tidak ditemukan", nil)
		}
		return apperror.Internal("failed to archive product", err)
	}
	return nil
}

func emptyProductList(page, limit int) ProductListResult {
	return ProductListResult{Items: []ProductSummary{}, Page: page, Limit: limit, Total: 0, TotalPages: 0}
}

func validateProductFields(name, categoryPublicID, brandPublicID string, weightGram float64) error {
	if name == "" || categoryPublicID == "" || brandPublicID == "" {
		return apperror.BadRequest("name, category_id, dan brand_id wajib diisi", nil)
	}
	if weightGram <= 0 {
		return apperror.BadRequest("weight_gram harus lebih besar dari 0", nil)
	}
	return nil
}

// skuSegment uppercases name, strips non-alphanumeric characters, and takes
// the first 3 characters — e.g. "Batangan" -> "BAT", "Antam" -> "ANT".
func skuSegment(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() == 3 {
			break
		}
	}
	return b.String()
}

// formatWeight renders weight_gram in its shortest round-trip decimal form
// so trailing zero decimals are trimmed: 10.000 -> "10", 10.500 -> "10.5".
func formatWeight(w float64) string {
	return strconv.FormatFloat(w, 'f', -1, 64)
}

func toProductSummary(p *model.Product, category, brand ProductRef) ProductSummary {
	description := ""
	if p.Description != nil {
		description = *p.Description
	}
	return ProductSummary{
		PublicID:    p.PublicID,
		Name:        p.Name,
		SKU:         p.SKU,
		Category:    category,
		Brand:       brand,
		WeightGram:  p.WeightGram,
		Description: description,
		IsActive:    p.IsActive,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func toProductSummaryFromRefs(p *repository.ProductWithRefs) ProductSummary {
	return toProductSummary(
		&p.Product,
		ProductRef{PublicID: p.CategoryPublicID, Name: p.CategoryName},
		ProductRef{PublicID: p.BrandPublicID, Name: p.BrandName},
	)
}
