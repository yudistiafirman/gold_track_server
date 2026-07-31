package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

// ProductSummary is the public-facing view of a product: only PublicID
// (UUID) ever leaves this layer, the internal bigint PK stays in
// model.Product/the repository. CategoryID/BrandID here are the public_id
// UUIDs the client submitted, echoed back — not the internal bigint FKs.
type ProductSummary struct {
	PublicID    string
	Name        string
	SKU         string
	CategoryID  string
	BrandID     string
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

type ProductService interface {
	Create(ctx context.Context, input CreateProductInput) (ProductSummary, error)
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

	return toProductSummary(created, input.CategoryPublicID, input.BrandPublicID), nil
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

func toProductSummary(p *model.Product, categoryPublicID, brandPublicID string) ProductSummary {
	description := ""
	if p.Description != nil {
		description = *p.Description
	}
	return ProductSummary{
		PublicID:    p.PublicID,
		Name:        p.Name,
		SKU:         p.SKU,
		CategoryID:  categoryPublicID,
		BrandID:     brandPublicID,
		WeightGram:  p.WeightGram,
		Description: description,
		IsActive:    p.IsActive,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}
