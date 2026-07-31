package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const expenseDateLayout = "2006-01-02"

const (
	defaultExpensePage  = 1
	defaultExpenseLimit = 20
	maxExpenseLimit     = 100
)

// ExpenseSummary is the public-facing view of an expense: only PublicID
// (UUID) ever leaves this layer.
type ExpenseSummary struct {
	PublicID    string
	Category    ProductRef
	Amount      float64
	Description string
	ExpenseDate string
	CreatedAt   time.Time
}

type CreateExpenseInput struct {
	CategoryPublicID  string
	Amount            float64
	Description       string
	ExpenseDate       string
	CreatedByPublicID string
}

type UpdateExpenseInput struct {
	PublicID         string
	CategoryPublicID string
	Amount           float64
	Description      string
	ExpenseDate      string
}

type ListExpensesInput struct {
	CategoryPublicID string
	DateFrom         string
	DateTo           string
	Page             int
	Limit            int
}

type ExpenseListResult struct {
	Items      []ExpenseSummary
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type ExpenseService interface {
	Create(ctx context.Context, input CreateExpenseInput) (ExpenseSummary, error)
	Get(ctx context.Context, publicID string) (ExpenseSummary, error)
	List(ctx context.Context, input ListExpensesInput) (ExpenseListResult, error)
	Update(ctx context.Context, input UpdateExpenseInput) (ExpenseSummary, error)
	Delete(ctx context.Context, publicID string) error
}

type expenseService struct {
	expenseRepo         repository.ExpenseRepository
	expenseCategoryRepo repository.ExpenseCategoryRepository
	userRepo            repository.UserRepository
}

func NewExpenseService(
	expenseRepo repository.ExpenseRepository,
	expenseCategoryRepo repository.ExpenseCategoryRepository,
	userRepo repository.UserRepository,
) ExpenseService {
	return &expenseService{
		expenseRepo:         expenseRepo,
		expenseCategoryRepo: expenseCategoryRepo,
		userRepo:            userRepo,
	}
}

func (s *expenseService) Create(ctx context.Context, input CreateExpenseInput) (ExpenseSummary, error) {
	category, expenseDate, err := s.validateExpenseFields(ctx, input.CategoryPublicID, input.Amount, input.ExpenseDate)
	if err != nil {
		return ExpenseSummary{}, err
	}

	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return ExpenseSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	created, err := s.expenseRepo.Create(ctx, &model.Expense{
		CategoryID:  category.ID,
		Amount:      input.Amount,
		Description: nilIfEmpty(input.Description),
		ExpenseDate: expenseDate,
		CreatedBy:   creator.ID,
	})
	if err != nil {
		return ExpenseSummary{}, apperror.Internal("failed to create expense", err)
	}

	return toExpenseSummary(&repository.ExpenseWithCategory{
		Expense:          *created,
		CategoryPublicID: category.PublicID,
		CategoryName:     category.Name,
	}), nil
}

func (s *expenseService) Get(ctx context.Context, publicID string) (ExpenseSummary, error) {
	expense, err := s.expenseRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrExpenseNotFound) {
			return ExpenseSummary{}, apperror.NotFound("pengeluaran tidak ditemukan", nil)
		}
		return ExpenseSummary{}, apperror.Internal("failed to fetch expense", err)
	}
	return toExpenseSummary(expense), nil
}

func (s *expenseService) List(ctx context.Context, input ListExpensesInput) (ExpenseListResult, error) {
	page := input.Page
	if page <= 0 {
		page = defaultExpensePage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultExpenseLimit
	}
	if limit > maxExpenseLimit {
		limit = maxExpenseLimit
	}

	filter := repository.ExpenseFilter{Page: page, Limit: limit}

	if input.CategoryPublicID != "" {
		category, err := s.expenseCategoryRepo.FindByPublicID(ctx, input.CategoryPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrExpenseCategoryNotFound) {
				return ExpenseListResult{}, apperror.NotFound("kategori pengeluaran tidak ditemukan", nil)
			}
			return ExpenseListResult{}, apperror.Internal("failed to fetch expense category", err)
		}
		filter.CategoryID = &category.ID
	}

	if input.DateFrom != "" {
		dateFrom, err := time.Parse(expenseDateLayout, strings.TrimSpace(input.DateFrom))
		if err != nil {
			return ExpenseListResult{}, apperror.BadRequest("date_from harus format YYYY-MM-DD", nil)
		}
		filter.DateFrom = &dateFrom
	}
	if input.DateTo != "" {
		dateTo, err := time.Parse(expenseDateLayout, strings.TrimSpace(input.DateTo))
		if err != nil {
			return ExpenseListResult{}, apperror.BadRequest("date_to harus format YYYY-MM-DD", nil)
		}
		filter.DateTo = &dateTo
	}

	expenses, total, err := s.expenseRepo.List(ctx, filter)
	if err != nil {
		return ExpenseListResult{}, apperror.Internal("failed to list expenses", err)
	}

	items := make([]ExpenseSummary, 0, len(expenses))
	for i := range expenses {
		items = append(items, toExpenseSummary(&expenses[i]))
	}

	return ExpenseListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *expenseService) Update(ctx context.Context, input UpdateExpenseInput) (ExpenseSummary, error) {
	category, expenseDate, err := s.validateExpenseFields(ctx, input.CategoryPublicID, input.Amount, input.ExpenseDate)
	if err != nil {
		return ExpenseSummary{}, err
	}

	existing, err := s.expenseRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrExpenseNotFound) {
			return ExpenseSummary{}, apperror.NotFound("pengeluaran tidak ditemukan", nil)
		}
		return ExpenseSummary{}, apperror.Internal("failed to fetch expense", err)
	}

	existing.CategoryID = category.ID
	existing.Amount = input.Amount
	existing.Description = nilIfEmpty(input.Description)
	existing.ExpenseDate = expenseDate

	if err := s.expenseRepo.Update(ctx, &existing.Expense); err != nil {
		if errors.Is(err, repository.ErrExpenseNotFound) {
			return ExpenseSummary{}, apperror.NotFound("pengeluaran tidak ditemukan", nil)
		}
		return ExpenseSummary{}, apperror.Internal("failed to update expense", err)
	}

	existing.CategoryPublicID = category.PublicID
	existing.CategoryName = category.Name
	return toExpenseSummary(existing), nil
}

func (s *expenseService) Delete(ctx context.Context, publicID string) error {
	deleted, err := s.expenseRepo.Delete(ctx, publicID)
	if err != nil {
		return apperror.Internal("failed to delete expense", err)
	}
	if !deleted {
		return apperror.NotFound("pengeluaran tidak ditemukan", nil)
	}
	return nil
}

// validateExpenseFields resolves+validates the fields shared by Create and
// Update: category_id/amount/expense_date are all required (BE-1202 AC#2).
func (s *expenseService) validateExpenseFields(ctx context.Context, categoryPublicID string, amount float64, expenseDateRaw string) (*model.ExpenseCategory, time.Time, error) {
	if categoryPublicID == "" {
		return nil, time.Time{}, apperror.BadRequest("category_id wajib diisi", nil)
	}
	if amount <= 0 {
		return nil, time.Time{}, apperror.BadRequest("amount wajib diisi lebih besar dari 0", nil)
	}
	if strings.TrimSpace(expenseDateRaw) == "" {
		return nil, time.Time{}, apperror.BadRequest("expense_date wajib diisi", nil)
	}
	expenseDate, err := time.Parse(expenseDateLayout, strings.TrimSpace(expenseDateRaw))
	if err != nil {
		return nil, time.Time{}, apperror.BadRequest("expense_date harus format YYYY-MM-DD", nil)
	}

	category, err := s.expenseCategoryRepo.FindByPublicID(ctx, categoryPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrExpenseCategoryNotFound) {
			return nil, time.Time{}, apperror.NotFound("kategori pengeluaran tidak ditemukan", nil)
		}
		return nil, time.Time{}, apperror.Internal("failed to fetch expense category", err)
	}

	return category, expenseDate, nil
}

func toExpenseSummary(e *repository.ExpenseWithCategory) ExpenseSummary {
	description := ""
	if e.Description != nil {
		description = *e.Description
	}
	return ExpenseSummary{
		PublicID:    e.PublicID,
		Category:    ProductRef{PublicID: e.CategoryPublicID, Name: e.CategoryName},
		Amount:      e.Amount,
		Description: description,
		ExpenseDate: e.ExpenseDate.Format(expenseDateLayout),
		CreatedAt:   e.CreatedAt,
	}
}
