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

// ExpenseCategorySummary is the public-facing view of an expense category:
// only PublicID (UUID) ever leaves this layer. No IsActive/UpdatedAt —
// unlike categories/brands, expense_categories has neither column, so
// Delete below is a real hard delete, not a deactivate.
type ExpenseCategorySummary struct {
	PublicID  string
	Name      string
	CreatedAt time.Time
}

type CreateExpenseCategoryInput struct {
	Name string
}

type UpdateExpenseCategoryInput struct {
	PublicID string
	Name     string
}

type ExpenseCategoryService interface {
	Create(ctx context.Context, input CreateExpenseCategoryInput) (ExpenseCategorySummary, error)
	List(ctx context.Context) ([]ExpenseCategorySummary, error)
	Get(ctx context.Context, publicID string) (ExpenseCategorySummary, error)
	Update(ctx context.Context, input UpdateExpenseCategoryInput) (ExpenseCategorySummary, error)
	Delete(ctx context.Context, publicID string) error
}

type expenseCategoryService struct {
	expenseCategoryRepo repository.ExpenseCategoryRepository
}

func NewExpenseCategoryService(expenseCategoryRepo repository.ExpenseCategoryRepository) ExpenseCategoryService {
	return &expenseCategoryService{expenseCategoryRepo: expenseCategoryRepo}
}

func (s *expenseCategoryService) Create(ctx context.Context, input CreateExpenseCategoryInput) (ExpenseCategorySummary, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ExpenseCategorySummary{}, apperror.BadRequest("name wajib diisi", nil)
	}

	created, err := s.expenseCategoryRepo.Create(ctx, &model.ExpenseCategory{Name: name})
	if err != nil {
		if errors.Is(err, repository.ErrExpenseCategoryNameTaken) {
			return ExpenseCategorySummary{}, apperror.Conflict("nama kategori pengeluaran sudah dipakai", nil)
		}
		return ExpenseCategorySummary{}, apperror.Internal("failed to create expense category", err)
	}

	return toExpenseCategorySummary(created), nil
}

func (s *expenseCategoryService) List(ctx context.Context) ([]ExpenseCategorySummary, error) {
	categories, err := s.expenseCategoryRepo.List(ctx)
	if err != nil {
		return nil, apperror.Internal("failed to list expense categories", err)
	}

	summaries := make([]ExpenseCategorySummary, 0, len(categories))
	for i := range categories {
		summaries = append(summaries, toExpenseCategorySummary(&categories[i]))
	}
	return summaries, nil
}

func (s *expenseCategoryService) Get(ctx context.Context, publicID string) (ExpenseCategorySummary, error) {
	category, err := s.expenseCategoryRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrExpenseCategoryNotFound) {
			return ExpenseCategorySummary{}, apperror.NotFound("kategori pengeluaran tidak ditemukan", nil)
		}
		return ExpenseCategorySummary{}, apperror.Internal("failed to fetch expense category", err)
	}
	return toExpenseCategorySummary(category), nil
}

func (s *expenseCategoryService) Update(ctx context.Context, input UpdateExpenseCategoryInput) (ExpenseCategorySummary, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return ExpenseCategorySummary{}, apperror.BadRequest("name wajib diisi", nil)
	}

	existing, err := s.expenseCategoryRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrExpenseCategoryNotFound) {
			return ExpenseCategorySummary{}, apperror.NotFound("kategori pengeluaran tidak ditemukan", nil)
		}
		return ExpenseCategorySummary{}, apperror.Internal("failed to fetch expense category", err)
	}

	existing.Name = name

	if err := s.expenseCategoryRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrExpenseCategoryNotFound) {
			return ExpenseCategorySummary{}, apperror.NotFound("kategori pengeluaran tidak ditemukan", nil)
		}
		if errors.Is(err, repository.ErrExpenseCategoryNameTaken) {
			return ExpenseCategorySummary{}, apperror.Conflict("nama kategori pengeluaran sudah dipakai", nil)
		}
		return ExpenseCategorySummary{}, apperror.Internal("failed to update expense category", err)
	}

	return toExpenseCategorySummary(existing), nil
}

func (s *expenseCategoryService) Delete(ctx context.Context, publicID string) error {
	deleted, err := s.expenseCategoryRepo.Delete(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrExpenseCategoryInUse) {
			return apperror.Conflict("kategori masih dipakai oleh pengeluaran, tidak bisa dihapus", nil)
		}
		return apperror.Internal("failed to delete expense category", err)
	}
	if !deleted {
		return apperror.NotFound("kategori pengeluaran tidak ditemukan", nil)
	}
	return nil
}

func toExpenseCategorySummary(c *model.ExpenseCategory) ExpenseCategorySummary {
	return ExpenseCategorySummary{
		PublicID:  c.PublicID,
		Name:      c.Name,
		CreatedAt: c.CreatedAt,
	}
}
