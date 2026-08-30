package service

import (
	"context"
	"errors"
	"math"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const dailyClosingDateLayout = "2006-01-02"

const (
	defaultDailyClosingPage  = 1
	defaultDailyClosingLimit = 20
	maxDailyClosingLimit     = 100
)

// DailyClosingSummary is the public-facing view of a daily closing — only
// PublicID (UUID) ever leaves this layer.
type DailyClosingSummary struct {
	PublicID       string
	ClosingDate    string
	TotalBalance   float64
	TotalGoldValue float64
	TotalSaldo     float64
	CreatedAt      time.Time
}

type CloseDailyBalanceInput struct {
	CreatedByPublicID string
}

type ListDailyClosingsInput struct {
	Page  int
	Limit int
}

type DailyClosingListResult struct {
	Items      []DailyClosingSummary
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type DailyClosingService interface {
	Close(ctx context.Context, input CloseDailyBalanceInput) (DailyClosingSummary, error)
	Get(ctx context.Context, publicID string) (DailyClosingSummary, error)
	List(ctx context.Context, input ListDailyClosingsInput) (DailyClosingListResult, error)
}

type dailyClosingService struct {
	dailyClosingRepo repository.DailyClosingRepository
	reportRepo       repository.ReportRepository
	userRepo         repository.UserRepository
}

func NewDailyClosingService(
	dailyClosingRepo repository.DailyClosingRepository,
	reportRepo repository.ReportRepository,
	userRepo repository.UserRepository,
) DailyClosingService {
	return &dailyClosingService{
		dailyClosingRepo: dailyClosingRepo,
		reportRepo:       reportRepo,
		userRepo:         userRepo,
	}
}

// Close snapshots today's (UTC) cash position and records it as the closing
// baseline for the day — 409 if today has already been closed.
func (s *dailyClosingService) Close(ctx context.Context, input CloseDailyBalanceInput) (DailyClosingSummary, error) {
	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return DailyClosingSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	cash, err := s.reportRepo.CashSummary(ctx)
	if err != nil {
		return DailyClosingSummary{}, apperror.Internal("failed to summarize cash", err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	created, err := s.dailyClosingRepo.Create(ctx, today, cash.TotalBalance, cash.TotalGoldValue, creator.ID)
	if err != nil {
		if errors.Is(err, repository.ErrDailyClosingDateTaken) {
			return DailyClosingSummary{}, apperror.Conflict("hari ini sudah ditutup", nil)
		}
		return DailyClosingSummary{}, apperror.Internal("failed to create daily closing", err)
	}

	return toDailyClosingSummary(created), nil
}

func (s *dailyClosingService) Get(ctx context.Context, publicID string) (DailyClosingSummary, error) {
	closing, err := s.dailyClosingRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrDailyClosingNotFound) {
			return DailyClosingSummary{}, apperror.NotFound("penutupan harian tidak ditemukan", nil)
		}
		return DailyClosingSummary{}, apperror.Internal("failed to fetch daily closing", err)
	}
	return toDailyClosingSummary(closing), nil
}

func (s *dailyClosingService) List(ctx context.Context, input ListDailyClosingsInput) (DailyClosingListResult, error) {
	page := input.Page
	if page <= 0 {
		page = defaultDailyClosingPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultDailyClosingLimit
	}
	if limit > maxDailyClosingLimit {
		limit = maxDailyClosingLimit
	}

	closings, total, err := s.dailyClosingRepo.List(ctx, page, limit)
	if err != nil {
		return DailyClosingListResult{}, apperror.Internal("failed to list daily closings", err)
	}

	items := make([]DailyClosingSummary, 0, len(closings))
	for i := range closings {
		items = append(items, toDailyClosingSummary(&closings[i]))
	}

	return DailyClosingListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func toDailyClosingSummary(c *model.DailyClosing) DailyClosingSummary {
	return DailyClosingSummary{
		PublicID:       c.PublicID,
		ClosingDate:    c.ClosingDate.Format(dailyClosingDateLayout),
		TotalBalance:   c.TotalBalance,
		TotalGoldValue: c.TotalGoldValue,
		TotalSaldo:     c.TotalSaldo,
		CreatedAt:      c.CreatedAt,
	}
}
