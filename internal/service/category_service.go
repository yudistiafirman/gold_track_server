package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

// CategorySummary is the public-facing view of a category: only PublicID
// (UUID) ever leaves this layer, the internal bigint PK stays in
// model.Category/the repository.
type CategorySummary struct {
	PublicID  string
	Name      string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateCategoryInput struct {
	Name string
}

type UpdateCategoryInput struct {
	PublicID string
	Name     string
	IsActive bool
}

type CategoryService interface {
	Create(ctx context.Context, input CreateCategoryInput) (CategorySummary, error)
	List(ctx context.Context) ([]CategorySummary, error)
	Get(ctx context.Context, publicID string) (CategorySummary, error)
	Update(ctx context.Context, input UpdateCategoryInput) (CategorySummary, error)
	Deactivate(ctx context.Context, publicID string) error
}

type categoryService struct {
	categoryRepo repository.CategoryRepository
}

func NewCategoryService(categoryRepo repository.CategoryRepository) CategoryService {
	return &categoryService{categoryRepo: categoryRepo}
}

func (s *categoryService) Create(ctx context.Context, input CreateCategoryInput) (CategorySummary, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateCategoryName(name); err != nil {
		return CategorySummary{}, err
	}

	category := &model.Category{Name: name, IsActive: true}

	created, err := s.categoryRepo.Create(ctx, category)
	if err != nil {
		if errors.Is(err, repository.ErrCategoryNameTaken) {
			return CategorySummary{}, apperror.Conflict("nama kategori sudah dipakai", nil)
		}
		return CategorySummary{}, apperror.Internal("failed to create category", err)
	}

	return toCategorySummary(created), nil
}

func (s *categoryService) List(ctx context.Context) ([]CategorySummary, error) {
	categories, err := s.categoryRepo.List(ctx)
	if err != nil {
		return nil, apperror.Internal("failed to list categories", err)
	}

	summaries := make([]CategorySummary, 0, len(categories))
	for i := range categories {
		summaries = append(summaries, toCategorySummary(&categories[i]))
	}
	return summaries, nil
}

func (s *categoryService) Get(ctx context.Context, publicID string) (CategorySummary, error) {
	category, err := s.categoryRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrCategoryNotFound) {
			return CategorySummary{}, apperror.NotFound("kategori tidak ditemukan", nil)
		}
		return CategorySummary{}, apperror.Internal("failed to fetch category", err)
	}
	return toCategorySummary(category), nil
}

func (s *categoryService) Update(ctx context.Context, input UpdateCategoryInput) (CategorySummary, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateCategoryName(name); err != nil {
		return CategorySummary{}, err
	}

	existing, err := s.categoryRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrCategoryNotFound) {
			return CategorySummary{}, apperror.NotFound("kategori tidak ditemukan", nil)
		}
		return CategorySummary{}, apperror.Internal("failed to fetch category", err)
	}

	existing.Name = name
	existing.IsActive = input.IsActive

	if err := s.categoryRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrCategoryNotFound) {
			return CategorySummary{}, apperror.NotFound("kategori tidak ditemukan", nil)
		}
		if errors.Is(err, repository.ErrCategoryNameTaken) {
			return CategorySummary{}, apperror.Conflict("nama kategori sudah dipakai", nil)
		}
		return CategorySummary{}, apperror.Internal("failed to update category", err)
	}

	return toCategorySummary(existing), nil
}

func (s *categoryService) Deactivate(ctx context.Context, publicID string) error {
	if err := s.categoryRepo.SetActive(ctx, publicID, false); err != nil {
		if errors.Is(err, repository.ErrCategoryNotFound) {
			return apperror.NotFound("kategori tidak ditemukan", nil)
		}
		return apperror.Internal("failed to deactivate category", err)
	}
	return nil
}

func validateCategoryName(name string) error {
	if name == "" {
		return apperror.BadRequest("name wajib diisi", nil)
	}
	return nil
}

func toCategorySummary(c *model.Category) CategorySummary {
	return CategorySummary{
		PublicID:  c.PublicID,
		Name:      c.Name,
		IsActive:  c.IsActive,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}
