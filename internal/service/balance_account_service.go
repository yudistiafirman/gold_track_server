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

// BalanceAccountSummary is the public-facing view of a balance account: only
// PublicID (UUID) ever leaves this layer. No IsActive/UpdatedAt — there is
// no history/audit trail for this resource (client requirement); balance is
// simply overwritten in place by Update.
type BalanceAccountSummary struct {
	PublicID  string
	Name      string
	Balance   float64
	CreatedAt time.Time
}

type CreateBalanceAccountInput struct {
	Name    string
	Balance float64
}

type UpdateBalanceAccountInput struct {
	PublicID string
	Name     string
	Balance  float64
}

type BalanceAccountService interface {
	Create(ctx context.Context, input CreateBalanceAccountInput) (BalanceAccountSummary, error)
	List(ctx context.Context) ([]BalanceAccountSummary, error)
	Get(ctx context.Context, publicID string) (BalanceAccountSummary, error)
	Update(ctx context.Context, input UpdateBalanceAccountInput) (BalanceAccountSummary, error)
	Delete(ctx context.Context, publicID string) error
}

type balanceAccountService struct {
	balanceAccountRepo repository.BalanceAccountRepository
}

func NewBalanceAccountService(balanceAccountRepo repository.BalanceAccountRepository) BalanceAccountService {
	return &balanceAccountService{balanceAccountRepo: balanceAccountRepo}
}

func (s *balanceAccountService) Create(ctx context.Context, input CreateBalanceAccountInput) (BalanceAccountSummary, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return BalanceAccountSummary{}, apperror.BadRequest("name wajib diisi", nil)
	}
	if input.Balance < 0 {
		return BalanceAccountSummary{}, apperror.BadRequest("balance tidak boleh negatif", nil)
	}

	created, err := s.balanceAccountRepo.Create(ctx, &model.BalanceAccount{Name: name, Balance: input.Balance})
	if err != nil {
		if errors.Is(err, repository.ErrBalanceAccountNameTaken) {
			return BalanceAccountSummary{}, apperror.Conflict("nama saldo sudah dipakai", nil)
		}
		return BalanceAccountSummary{}, apperror.Internal("failed to create balance account", err)
	}

	return toBalanceAccountSummary(created), nil
}

func (s *balanceAccountService) List(ctx context.Context) ([]BalanceAccountSummary, error) {
	accounts, err := s.balanceAccountRepo.List(ctx)
	if err != nil {
		return nil, apperror.Internal("failed to list balance accounts", err)
	}

	summaries := make([]BalanceAccountSummary, 0, len(accounts))
	for i := range accounts {
		summaries = append(summaries, toBalanceAccountSummary(&accounts[i]))
	}
	return summaries, nil
}

func (s *balanceAccountService) Get(ctx context.Context, publicID string) (BalanceAccountSummary, error) {
	account, err := s.balanceAccountRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrBalanceAccountNotFound) {
			return BalanceAccountSummary{}, apperror.NotFound("saldo tidak ditemukan", nil)
		}
		return BalanceAccountSummary{}, apperror.Internal("failed to fetch balance account", err)
	}
	return toBalanceAccountSummary(account), nil
}

func (s *balanceAccountService) Update(ctx context.Context, input UpdateBalanceAccountInput) (BalanceAccountSummary, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return BalanceAccountSummary{}, apperror.BadRequest("name wajib diisi", nil)
	}
	if input.Balance < 0 {
		return BalanceAccountSummary{}, apperror.BadRequest("balance tidak boleh negatif", nil)
	}

	existing, err := s.balanceAccountRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrBalanceAccountNotFound) {
			return BalanceAccountSummary{}, apperror.NotFound("saldo tidak ditemukan", nil)
		}
		return BalanceAccountSummary{}, apperror.Internal("failed to fetch balance account", err)
	}

	existing.Name = name
	existing.Balance = input.Balance

	if err := s.balanceAccountRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, repository.ErrBalanceAccountNotFound) {
			return BalanceAccountSummary{}, apperror.NotFound("saldo tidak ditemukan", nil)
		}
		if errors.Is(err, repository.ErrBalanceAccountNameTaken) {
			return BalanceAccountSummary{}, apperror.Conflict("nama saldo sudah dipakai", nil)
		}
		return BalanceAccountSummary{}, apperror.Internal("failed to update balance account", err)
	}

	return toBalanceAccountSummary(existing), nil
}

func (s *balanceAccountService) Delete(ctx context.Context, publicID string) error {
	deleted, err := s.balanceAccountRepo.Delete(ctx, publicID)
	if err != nil {
		return apperror.Internal("failed to delete balance account", err)
	}
	if !deleted {
		return apperror.NotFound("saldo tidak ditemukan", nil)
	}
	return nil
}

func toBalanceAccountSummary(a *model.BalanceAccount) BalanceAccountSummary {
	return BalanceAccountSummary{
		PublicID:  a.PublicID,
		Name:      a.Name,
		Balance:   a.Balance,
		CreatedAt: a.CreatedAt,
	}
}
