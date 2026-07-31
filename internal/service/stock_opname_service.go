package service

import (
	"context"
	"errors"
	"time"

	"gold-track-be/internal/model"
	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const stockOpnameDateLayout = "2006-01-02"

type StockOpnameItemSummary struct {
	PublicID          string
	StockItemPublicID string
	Barcode           string
	ProductName       string
	SystemStatus      string
	PhysicalStatus    string
	Result            string
}

type StockOpnameSummaryCounts struct {
	Match      int
	Missing    int
	Unexpected int
}

// StockOpnameSummary is the public-facing view of an opname session — only
// PublicID (UUID) ever leaves this layer. Items is empty right after
// Create (no scans yet); Summary is always computed fresh from Items.
type StockOpnameSummary struct {
	PublicID   string
	OpnameCode string
	OpnameDate string
	Status     string
	Notes      string
	Items      []StockOpnameItemSummary
	Summary    StockOpnameSummaryCounts
	CreatedAt  time.Time
}

type CreateStockOpnameInput struct {
	Notes             string
	CreatedByPublicID string
}

type StockOpnameService interface {
	Create(ctx context.Context, input CreateStockOpnameInput) (StockOpnameSummary, error)
	Get(ctx context.Context, publicID string) (StockOpnameSummary, error)
	Scan(ctx context.Context, opnamePublicID, barcode string) (StockOpnameItemSummary, error)
	Complete(ctx context.Context, publicID string) (StockOpnameSummary, error)
}

type stockOpnameService struct {
	stockOpnameRepo repository.StockOpnameRepository
	userRepo        repository.UserRepository
}

func NewStockOpnameService(
	stockOpnameRepo repository.StockOpnameRepository,
	userRepo repository.UserRepository,
) StockOpnameService {
	return &stockOpnameService{
		stockOpnameRepo: stockOpnameRepo,
		userRepo:        userRepo,
	}
}

func (s *stockOpnameService) Create(ctx context.Context, input CreateStockOpnameInput) (StockOpnameSummary, error) {
	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return StockOpnameSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	opname, err := s.stockOpnameRepo.CreateWithGeneratedCode(ctx, repository.CreateOpnameInput{
		Notes:     nilIfEmpty(input.Notes),
		CreatedBy: creator.ID,
	})
	if err != nil {
		if errors.Is(err, repository.ErrOpnameCodeGenerationFailed) {
			return StockOpnameSummary{}, apperror.Conflict("gagal membuat kode opname unik, coba lagi", nil)
		}
		return StockOpnameSummary{}, apperror.Internal("failed to create stock opname", err)
	}

	return toStockOpnameSummary(opname, nil), nil
}

func (s *stockOpnameService) Get(ctx context.Context, publicID string) (StockOpnameSummary, error) {
	opname, items, err := s.stockOpnameRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrStockOpnameNotFound) {
			return StockOpnameSummary{}, apperror.NotFound("sesi opname tidak ditemukan", nil)
		}
		return StockOpnameSummary{}, apperror.Internal("failed to fetch stock opname", err)
	}

	return toStockOpnameSummary(opname, items), nil
}

func (s *stockOpnameService) Scan(ctx context.Context, opnamePublicID, barcode string) (StockOpnameItemSummary, error) {
	if barcode == "" {
		return StockOpnameItemSummary{}, apperror.BadRequest("barcode wajib diisi", nil)
	}

	item, err := s.stockOpnameRepo.Scan(ctx, opnamePublicID, barcode)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrStockOpnameNotFound):
			return StockOpnameItemSummary{}, apperror.NotFound("sesi opname tidak ditemukan", nil)
		case errors.Is(err, repository.ErrStockOpnameNotInProgress):
			return StockOpnameItemSummary{}, apperror.Conflict("sesi opname sudah selesai, tidak bisa discan lagi", nil)
		case errors.Is(err, repository.ErrStockItemNotFound):
			return StockOpnameItemSummary{}, apperror.NotFound("barcode tidak ditemukan", nil)
		case errors.Is(err, repository.ErrStockItemAlreadyScanned):
			return StockOpnameItemSummary{}, apperror.Conflict("unit sudah discan di sesi ini", nil)
		default:
			return StockOpnameItemSummary{}, apperror.Internal("failed to scan stock item", err)
		}
	}

	return toStockOpnameItemSummary(*item), nil
}

func (s *stockOpnameService) Complete(ctx context.Context, publicID string) (StockOpnameSummary, error) {
	err := s.stockOpnameRepo.Complete(ctx, publicID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrStockOpnameNotFound):
			return StockOpnameSummary{}, apperror.NotFound("sesi opname tidak ditemukan", nil)
		case errors.Is(err, repository.ErrStockOpnameNotInProgress):
			return StockOpnameSummary{}, apperror.Conflict("sesi opname sudah selesai", nil)
		default:
			return StockOpnameSummary{}, apperror.Internal("failed to complete stock opname", err)
		}
	}

	opname, items, err := s.stockOpnameRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		return StockOpnameSummary{}, apperror.Internal("failed to fetch completed stock opname", err)
	}
	return toStockOpnameSummary(opname, items), nil
}

func toStockOpnameItemSummary(it repository.StockOpnameItemWithStockRef) StockOpnameItemSummary {
	physicalStatus := ""
	if it.PhysicalStatus != nil {
		physicalStatus = *it.PhysicalStatus
	}
	return StockOpnameItemSummary{
		PublicID:          it.PublicID,
		StockItemPublicID: it.StockItemPublicID,
		Barcode:           it.Barcode,
		ProductName:       it.ProductName,
		SystemStatus:      it.SystemStatus,
		PhysicalStatus:    physicalStatus,
		Result:            it.Result,
	}
}

func toStockOpnameSummary(o *model.StockOpname, items []repository.StockOpnameItemWithStockRef) StockOpnameSummary {
	notes := ""
	if o.Notes != nil {
		notes = *o.Notes
	}

	itemSummaries := make([]StockOpnameItemSummary, 0, len(items))
	var counts StockOpnameSummaryCounts
	for _, it := range items {
		itemSummaries = append(itemSummaries, toStockOpnameItemSummary(it))
		switch it.Result {
		case "MATCH":
			counts.Match++
		case "MISSING":
			counts.Missing++
		case "UNEXPECTED":
			counts.Unexpected++
		}
	}

	return StockOpnameSummary{
		PublicID:   o.PublicID,
		OpnameCode: o.OpnameCode,
		OpnameDate: o.OpnameDate.Format(stockOpnameDateLayout),
		Status:     o.Status,
		Notes:      notes,
		Items:      itemSummaries,
		Summary:    counts,
		CreatedAt:  o.CreatedAt,
	}
}
