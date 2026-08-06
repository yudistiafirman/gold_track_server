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

// ExternalFundSummary is the public-facing view of an external fund entry
// ("Uang Diluar"): only PublicID (UUID) ever leaves this layer. No
// IsActive/UpdatedAt/status — there is no history for this resource; an
// entry is deleted outright once the money is settled (client requirement).
type ExternalFundSummary struct {
	PublicID    string
	Description string
	Amount      float64
	CreatedAt   time.Time
}

type CreateExternalFundInput struct {
	Description string
	Amount      float64
}

type UpdateExternalFundInput struct {
	PublicID    string
	Description string
	Amount      float64
}

type ExternalFundService interface {
	Create(ctx context.Context, input CreateExternalFundInput) (ExternalFundSummary, error)
	List(ctx context.Context) ([]ExternalFundSummary, error)
	Get(ctx context.Context, publicID string) (ExternalFundSummary, error)
	Update(ctx context.Context, input UpdateExternalFundInput) (ExternalFundSummary, error)
	Delete(ctx context.Context, publicID string) error
}

type externalFundService struct {
	externalFundRepo repository.ExternalFundRepository
}

func NewExternalFundService(externalFundRepo repository.ExternalFundRepository) ExternalFundService {
	return &externalFundService{externalFundRepo: externalFundRepo}
}

func (s *externalFundService) Create(ctx context.Context, input CreateExternalFundInput) (ExternalFundSummary, error) {
	description := strings.TrimSpace(input.Description)
	if description == "" {
		return ExternalFundSummary{}, apperror.BadRequest("description wajib diisi", nil)
	}
	if input.Amount <= 0 {
		return ExternalFundSummary{}, apperror.BadRequest("amount wajib diisi lebih besar dari 0", nil)
	}

	created, err := s.externalFundRepo.Create(ctx, &model.ExternalFund{Description: description, Amount: input.Amount})
	if err != nil {
		return ExternalFundSummary{}, apperror.Internal("failed to create external fund", err)
	}

	return toExternalFundSummary(created), nil
}

func (s *externalFundService) List(ctx context.Context) ([]ExternalFundSummary, error) {
	funds, err := s.externalFundRepo.List(ctx)
	if err != nil {
		return nil, apperror.Internal("failed to list external funds", err)
	}

	summaries := make([]ExternalFundSummary, 0, len(funds))
	for i := range funds {
		summaries = append(summaries, toExternalFundSummary(&funds[i]))
	}
	return summaries, nil
}

func (s *externalFundService) Get(ctx context.Context, publicID string) (ExternalFundSummary, error) {
	fund, err := s.externalFundRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrExternalFundNotFound) {
			return ExternalFundSummary{}, apperror.NotFound("uang diluar tidak ditemukan", nil)
		}
		return ExternalFundSummary{}, apperror.Internal("failed to fetch external fund", err)
	}
	return toExternalFundSummary(fund), nil
}

func (s *externalFundService) Update(ctx context.Context, input UpdateExternalFundInput) (ExternalFundSummary, error) {
	description := strings.TrimSpace(input.Description)
	if description == "" {
		return ExternalFundSummary{}, apperror.BadRequest("description wajib diisi", nil)
	}
	if input.Amount <= 0 {
		return ExternalFundSummary{}, apperror.BadRequest("amount wajib diisi lebih besar dari 0", nil)
	}

	existing, err := s.externalFundRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrExternalFundNotFound) {
			return ExternalFundSummary{}, apperror.NotFound("uang diluar tidak ditemukan", nil)
		}
		return ExternalFundSummary{}, apperror.Internal("failed to fetch external fund", err)
	}

	existing.Description = description
	existing.Amount = input.Amount

	if err := s.externalFundRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrExternalFundNotFound) {
			return ExternalFundSummary{}, apperror.NotFound("uang diluar tidak ditemukan", nil)
		}
		return ExternalFundSummary{}, apperror.Internal("failed to update external fund", err)
	}

	return toExternalFundSummary(existing), nil
}

func (s *externalFundService) Delete(ctx context.Context, publicID string) error {
	deleted, err := s.externalFundRepo.Delete(ctx, publicID)
	if err != nil {
		return apperror.Internal("failed to delete external fund", err)
	}
	if !deleted {
		return apperror.NotFound("uang diluar tidak ditemukan", nil)
	}
	return nil
}

func toExternalFundSummary(f *model.ExternalFund) ExternalFundSummary {
	return ExternalFundSummary{
		PublicID:    f.PublicID,
		Description: f.Description,
		Amount:      f.Amount,
		CreatedAt:   f.CreatedAt,
	}
}
