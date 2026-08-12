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

const stockOpnameDateLayout = "2006-01-02"

const (
	defaultStockOpnamePage  = 1
	defaultStockOpnameLimit = 20
	maxStockOpnameLimit     = 100
)

var allowedStockOpnameStatuses = map[string]struct{}{
	"IN_PROGRESS": {},
	"COMPLETED":   {},
	"CANCELLED":   {},
}

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
	// NotScanned is how many AVAILABLE stock items are still unscanned in
	// this session — 0 once the session is COMPLETED (those units already
	// landed as MISSING items by then). Only populated for IN_PROGRESS
	// sessions; List's header rows leave it 0 like the other counts.
	NotScanned int
}

// ScanStockOpnameResult pairs the just-scanned item with the session's
// current NotScanned count (plus the detail behind it), so the scanning
// screen can show "still N left" — and what those N are — live after every
// scan instead of only surfacing it once Complete runs.
type ScanStockOpnameResult struct {
	Item               StockOpnameItemSummary
	NotScanned         int
	NotScannedItems    []NotScannedItem
	NotScannedByWeight []NotScannedWeightGroup
}

// NotScannedItem is an AVAILABLE stock item still unscanned in a session,
// with the fields the UI needs to list it (sku/weight_gram in particular,
// for grouping).
type NotScannedItem struct {
	StockItemPublicID string
	Barcode           string
	SKU               string
	ProductName       string
	WeightGram        float64
}

// NotScannedWeightGroup aggregates NotScannedItems by weight_gram — a
// single grand total across every weight isn't meaningful for a physical
// gold count, so unscanned units are grouped and counted per weight
// instead (e.g. "5 gram: 3 unit").
type NotScannedWeightGroup struct {
	WeightGram      float64
	Count           int
	TotalWeightGram float64
}

// StockOpnameSummary is the public-facing view of an opname session — only
// PublicID (UUID) ever leaves this layer. Items is empty right after
// Create (no scans yet); Summary is always computed fresh from Items.
// NotScannedItems/NotScannedByWeight follow the same rule as
// Summary.NotScanned — only populated while IN_PROGRESS.
type StockOpnameSummary struct {
	PublicID           string
	OpnameCode         string
	OpnameDate         string
	Status             string
	Notes              string
	Items              []StockOpnameItemSummary
	Summary            StockOpnameSummaryCounts
	NotScannedItems    []NotScannedItem
	NotScannedByWeight []NotScannedWeightGroup
	CreatedAt          time.Time
}

type CreateStockOpnameInput struct {
	Notes             string
	CreatedByPublicID string
}

type ListStockOpnameInput struct {
	Status string
	Page   int
	Limit  int
}

// StockOpnameListResult rows are header-only (Items left nil, Summary left
// zero) — same pattern as PurchaseOrderListResult's list rows. Fetch
// GET /api/stock-opnames/{id} for a session's full detail.
type StockOpnameListResult struct {
	Items      []StockOpnameSummary
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type StockOpnameService interface {
	Create(ctx context.Context, input CreateStockOpnameInput) (StockOpnameSummary, error)
	List(ctx context.Context, input ListStockOpnameInput) (StockOpnameListResult, error)
	Get(ctx context.Context, publicID string) (StockOpnameSummary, error)
	Scan(ctx context.Context, opnamePublicID, barcode string) (ScanStockOpnameResult, error)
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
		switch {
		case errors.Is(err, repository.ErrStockOpnameAlreadyInProgress):
			return StockOpnameSummary{}, apperror.Conflict("masih ada sesi opname yang belum selesai (IN_PROGRESS), selesaikan atau lanjutkan sesi itu dulu sebelum membuat sesi baru", nil)
		case errors.Is(err, repository.ErrOpnameCodeGenerationFailed):
			return StockOpnameSummary{}, apperror.Conflict("gagal membuat kode opname unik, coba lagi", nil)
		default:
			return StockOpnameSummary{}, apperror.Internal("failed to create stock opname", err)
		}
	}

	return toStockOpnameSummary(opname, nil), nil
}

func (s *stockOpnameService) List(ctx context.Context, input ListStockOpnameInput) (StockOpnameListResult, error) {
	page := input.Page
	if page <= 0 {
		page = defaultStockOpnamePage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultStockOpnameLimit
	}
	if limit > maxStockOpnameLimit {
		limit = maxStockOpnameLimit
	}

	filter := repository.StockOpnameFilter{Page: page, Limit: limit}
	if input.Status != "" {
		if _, ok := allowedStockOpnameStatuses[input.Status]; !ok {
			return StockOpnameListResult{}, apperror.BadRequest("status harus IN_PROGRESS, COMPLETED, atau CANCELLED", nil)
		}
		status := input.Status
		filter.Status = &status
	}

	opnames, total, err := s.stockOpnameRepo.List(ctx, filter)
	if err != nil {
		return StockOpnameListResult{}, apperror.Internal("failed to list stock opnames", err)
	}

	items := make([]StockOpnameSummary, 0, len(opnames))
	for i := range opnames {
		items = append(items, toStockOpnameSummary(&opnames[i], nil))
	}

	return StockOpnameListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *stockOpnameService) Get(ctx context.Context, publicID string) (StockOpnameSummary, error) {
	opname, items, err := s.stockOpnameRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrStockOpnameNotFound) {
			return StockOpnameSummary{}, apperror.NotFound("sesi opname tidak ditemukan", nil)
		}
		return StockOpnameSummary{}, apperror.Internal("failed to fetch stock opname", err)
	}

	summary := toStockOpnameSummary(opname, items)
	if opname.Status == "IN_PROGRESS" {
		pending, err := s.stockOpnameRepo.PendingItems(ctx, opname.ID)
		if err != nil {
			return StockOpnameSummary{}, apperror.Internal("failed to list pending stock opname items", err)
		}
		summary.Summary.NotScanned = len(pending)
		summary.NotScannedItems = toNotScannedItems(pending)
		summary.NotScannedByWeight = groupNotScannedByWeight(pending)
	}
	return summary, nil
}

func (s *stockOpnameService) Scan(ctx context.Context, opnamePublicID, barcode string) (ScanStockOpnameResult, error) {
	if barcode == "" {
		return ScanStockOpnameResult{}, apperror.BadRequest("barcode wajib diisi", nil)
	}

	item, err := s.stockOpnameRepo.Scan(ctx, opnamePublicID, barcode)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrStockOpnameNotFound):
			return ScanStockOpnameResult{}, apperror.NotFound("sesi opname tidak ditemukan", nil)
		case errors.Is(err, repository.ErrStockOpnameNotInProgress):
			return ScanStockOpnameResult{}, apperror.Conflict("sesi opname sudah selesai, tidak bisa discan lagi", nil)
		case errors.Is(err, repository.ErrStockItemNotFound):
			return ScanStockOpnameResult{}, apperror.NotFound("barcode tidak ditemukan", nil)
		case errors.Is(err, repository.ErrStockItemAlreadyScanned):
			return ScanStockOpnameResult{}, apperror.Conflict("unit sudah discan di sesi ini", nil)
		default:
			return ScanStockOpnameResult{}, apperror.Internal("failed to scan stock item", err)
		}
	}

	pending, err := s.stockOpnameRepo.PendingItems(ctx, item.OpnameID)
	if err != nil {
		return ScanStockOpnameResult{}, apperror.Internal("failed to list pending stock opname items", err)
	}

	return ScanStockOpnameResult{
		Item:               toStockOpnameItemSummary(*item),
		NotScanned:         len(pending),
		NotScannedItems:    toNotScannedItems(pending),
		NotScannedByWeight: groupNotScannedByWeight(pending),
	}, nil
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

func toNotScannedItems(items []repository.PendingStockItem) []NotScannedItem {
	out := make([]NotScannedItem, 0, len(items))
	for _, it := range items {
		out = append(out, NotScannedItem{
			StockItemPublicID: it.StockItemPublicID,
			Barcode:           it.Barcode,
			SKU:               it.SKU,
			ProductName:       it.ProductName,
			WeightGram:        it.WeightGram,
		})
	}
	return out
}

// groupNotScannedByWeight aggregates not-yet-scanned units by weight_gram
// (items is already ORDER BY weight_gram from the repo, so a linear scan
// keeps groups in ascending weight order without a separate sort step).
func groupNotScannedByWeight(items []repository.PendingStockItem) []NotScannedWeightGroup {
	groups := make([]NotScannedWeightGroup, 0)
	index := make(map[float64]int)
	for _, it := range items {
		if i, ok := index[it.WeightGram]; ok {
			groups[i].Count++
			groups[i].TotalWeightGram += it.WeightGram
			continue
		}
		index[it.WeightGram] = len(groups)
		groups = append(groups, NotScannedWeightGroup{
			WeightGram:      it.WeightGram,
			Count:           1,
			TotalWeightGram: it.WeightGram,
		})
	}
	return groups
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
