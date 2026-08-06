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

// ExternalDebtSummary is the public-facing view of an external debt entry
// ("Hutang Diluar"): only PublicID (UUID) ever leaves this layer. No
// IsActive/UpdatedAt/status — there is no history for this resource.
// Partial repayment ("cicilan") is modeled by Update lowering Amount, and an
// entry is deleted outright once fully paid off (client requirement).
type ExternalDebtSummary struct {
	PublicID   string
	DebtorName string
	Amount     float64
	CreatedAt  time.Time
}

type CreateExternalDebtInput struct {
	DebtorName string
	Amount     float64
}

type UpdateExternalDebtInput struct {
	PublicID   string
	DebtorName string
	Amount     float64
}

type ExternalDebtService interface {
	Create(ctx context.Context, input CreateExternalDebtInput) (ExternalDebtSummary, error)
	List(ctx context.Context) ([]ExternalDebtSummary, error)
	Get(ctx context.Context, publicID string) (ExternalDebtSummary, error)
	Update(ctx context.Context, input UpdateExternalDebtInput) (ExternalDebtSummary, error)
	Delete(ctx context.Context, publicID string) error
}

type externalDebtService struct {
	externalDebtRepo repository.ExternalDebtRepository
}

func NewExternalDebtService(externalDebtRepo repository.ExternalDebtRepository) ExternalDebtService {
	return &externalDebtService{externalDebtRepo: externalDebtRepo}
}

func (s *externalDebtService) Create(ctx context.Context, input CreateExternalDebtInput) (ExternalDebtSummary, error) {
	debtorName := strings.TrimSpace(input.DebtorName)
	if debtorName == "" {
		return ExternalDebtSummary{}, apperror.BadRequest("debtor_name wajib diisi", nil)
	}
	if input.Amount <= 0 {
		return ExternalDebtSummary{}, apperror.BadRequest("amount wajib diisi lebih besar dari 0", nil)
	}

	created, err := s.externalDebtRepo.Create(ctx, &model.ExternalDebt{DebtorName: debtorName, Amount: input.Amount})
	if err != nil {
		return ExternalDebtSummary{}, apperror.Internal("failed to create external debt", err)
	}

	return toExternalDebtSummary(created), nil
}

func (s *externalDebtService) List(ctx context.Context) ([]ExternalDebtSummary, error) {
	debts, err := s.externalDebtRepo.List(ctx)
	if err != nil {
		return nil, apperror.Internal("failed to list external debts", err)
	}

	summaries := make([]ExternalDebtSummary, 0, len(debts))
	for i := range debts {
		summaries = append(summaries, toExternalDebtSummary(&debts[i]))
	}
	return summaries, nil
}

func (s *externalDebtService) Get(ctx context.Context, publicID string) (ExternalDebtSummary, error) {
	debt, err := s.externalDebtRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrExternalDebtNotFound) {
			return ExternalDebtSummary{}, apperror.NotFound("hutang tidak ditemukan", nil)
		}
		return ExternalDebtSummary{}, apperror.Internal("failed to fetch external debt", err)
	}
	return toExternalDebtSummary(debt), nil
}

func (s *externalDebtService) Update(ctx context.Context, input UpdateExternalDebtInput) (ExternalDebtSummary, error) {
	debtorName := strings.TrimSpace(input.DebtorName)
	if debtorName == "" {
		return ExternalDebtSummary{}, apperror.BadRequest("debtor_name wajib diisi", nil)
	}
	if input.Amount <= 0 {
		return ExternalDebtSummary{}, apperror.BadRequest("amount wajib diisi lebih besar dari 0", nil)
	}

	existing, err := s.externalDebtRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrExternalDebtNotFound) {
			return ExternalDebtSummary{}, apperror.NotFound("hutang tidak ditemukan", nil)
		}
		return ExternalDebtSummary{}, apperror.Internal("failed to fetch external debt", err)
	}

	existing.DebtorName = debtorName
	existing.Amount = input.Amount

	if err := s.externalDebtRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrExternalDebtNotFound) {
			return ExternalDebtSummary{}, apperror.NotFound("hutang tidak ditemukan", nil)
		}
		return ExternalDebtSummary{}, apperror.Internal("failed to update external debt", err)
	}

	return toExternalDebtSummary(existing), nil
}

func (s *externalDebtService) Delete(ctx context.Context, publicID string) error {
	deleted, err := s.externalDebtRepo.Delete(ctx, publicID)
	if err != nil {
		return apperror.Internal("failed to delete external debt", err)
	}
	if !deleted {
		return apperror.NotFound("hutang tidak ditemukan", nil)
	}
	return nil
}

func toExternalDebtSummary(d *model.ExternalDebt) ExternalDebtSummary {
	return ExternalDebtSummary{
		PublicID:   d.PublicID,
		DebtorName: d.DebtorName,
		Amount:     d.Amount,
		CreatedAt:  d.CreatedAt,
	}
}
