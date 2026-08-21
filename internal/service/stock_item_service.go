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

const (
	defaultStockItemPage  = 1
	defaultStockItemLimit = 20
	maxStockItemLimit     = 100

	stockItemDateLayout = "2006-01-02"
)

var allowedConditions = map[string]struct{}{
	"GOOD": {},
	"BAD":  {},
}

var allowedStockStatuses = map[string]struct{}{
	"AVAILABLE": {},
	"SOLD":      {},
	"ARCHIVED":  {},
	"VOID":      {},
}

// StockItemSoldTo is who bought a SOLD unit — nil for a unit that's never
// been (currently) sold. Only one of Customer/Supplier applies, captured by
// Type rather than two separate nilable fields, mirroring which side of
// SELL/SELL_SUPPLIER actually sold this unit.
type StockItemSoldTo struct {
	Type     string // CUSTOMER | SUPPLIER
	PublicID string
	Name     string
}

// StockItemSummary is the public-facing view of a stock item: only
// PublicID (UUID) ever leaves this layer, the internal bigint PK stays in
// model.StockItem/the repository.
type StockItemSummary struct {
	PublicID       string
	Product        ProductRef
	WeightGram     float64
	Barcode        string
	SerialNumber   string
	Condition      string
	PurchasePrice  float64
	PurchaseDate   string // "2006-01-02"
	ProductionYear *int   // optional
	Status         string
	SoldAt         *time.Time
	SoldTo         *StockItemSoldTo
	Notes          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// StockItemLabel is the narrow view BE-505 needs to render a CODE128 label
// client-side — works regardless of status (AVAILABLE or SOLD, reprints
// allowed).
type StockItemLabel struct {
	Barcode      string
	ProductName  string
	WeightGram   float64
	SerialNumber string
}

type CreateStockItemInput struct {
	ProductPublicID   string
	SerialNumber      string
	Condition         string
	PurchasePrice     float64
	PurchaseDate      string // "2006-01-02"
	ProductionYear    *int   // optional
	Notes             string
	CreatedByPublicID string
}

type UpdateStockItemInput struct {
	PublicID       string
	SerialNumber   string
	Condition      string
	PurchasePrice  float64
	PurchaseDate   string // "2006-01-02"
	ProductionYear *int   // optional
	Notes          string
}

type ListStockItemsInput struct {
	ProductPublicID string
	Status          string
	Condition       string
	Search          string
	Page            int
	Limit           int
}

// ListAllStockItemsInput backs List, the global (not product-scoped) stock
// item listing — ProductPublicID is optional here (unlike
// ListStockItemsInput.ProductPublicID, always required by ListByProduct's
// caller): "" means every product.
type ListAllStockItemsInput struct {
	ProductPublicID string
	Status          string
	Condition       string
	Search          string
	Page            int
	Limit           int
}

type StockItemListResult struct {
	Items      []StockItemSummary
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type StockItemService interface {
	Create(ctx context.Context, input CreateStockItemInput) (StockItemSummary, error)
	List(ctx context.Context, input ListStockItemsInput) (StockItemListResult, error)
	// ListAll is List's global counterpart — not scoped to one product,
	// backs pages that browse stock across the whole catalog (e.g. a
	// "Barang Terjual" sold-items view via ?status=SOLD).
	ListAll(ctx context.Context, input ListAllStockItemsInput) (StockItemListResult, error)
	Get(ctx context.Context, publicID string) (StockItemSummary, error)
	GetLabel(ctx context.Context, publicID string) (StockItemLabel, error)
	Update(ctx context.Context, input UpdateStockItemInput) (StockItemSummary, error)
	Delete(ctx context.Context, publicID string) error
	// Lookup finds a stock item by its physical barcode (BE-701). If
	// transactionType is "SELL" and the unit's condition is BAD, the
	// returned flag signals the client should show a confirmation prompt
	// before adding it to the cart (BE-703) — SELL_SUPPLIER or an omitted
	// type never requires confirmation.
	Lookup(ctx context.Context, barcode, transactionType string) (StockItemSummary, bool, error)
}

type stockItemService struct {
	stockItemRepo repository.StockItemRepository
	productRepo   repository.ProductRepository
	userRepo      repository.UserRepository
}

func NewStockItemService(
	stockItemRepo repository.StockItemRepository,
	productRepo repository.ProductRepository,
	userRepo repository.UserRepository,
) StockItemService {
	return &stockItemService{
		stockItemRepo: stockItemRepo,
		productRepo:   productRepo,
		userRepo:      userRepo,
	}
}

func (s *stockItemService) Create(ctx context.Context, input CreateStockItemInput) (StockItemSummary, error) {
	product, err := s.productRepo.FindByPublicID(ctx, input.ProductPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrProductNotFound) {
			return StockItemSummary{}, apperror.NotFound("produk tidak ditemukan", nil)
		}
		return StockItemSummary{}, apperror.Internal("failed to fetch product", err)
	}
	if !product.IsActive {
		return StockItemSummary{}, apperror.BadRequest("produk sudah diarsipkan, tidak bisa menambah stok", nil)
	}

	serialNumber := strings.TrimSpace(input.SerialNumber)
	purchaseDate, err := validateStockItemFields(serialNumber, input.Condition, input.PurchasePrice, input.PurchaseDate)
	if err != nil {
		return StockItemSummary{}, err
	}
	if err := validateProductionYear(input.ProductionYear); err != nil {
		return StockItemSummary{}, err
	}

	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return StockItemSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	stockItem := &model.StockItem{
		ProductID:      product.ID,
		SerialNumber:   serialNumber,
		Condition:      input.Condition,
		PurchasePrice:  input.PurchasePrice,
		PurchaseDate:   purchaseDate,
		ProductionYear: input.ProductionYear,
		Notes:          nilIfEmpty(input.Notes),
		Status:         "AVAILABLE",
		CreatedBy:      creator.ID,
	}

	created, err := s.stockItemRepo.CreateWithGeneratedBarcode(ctx, stockItem)
	if err != nil {
		if errors.Is(err, repository.ErrSerialNumberTaken) {
			return StockItemSummary{}, apperror.Conflict("serial_number sudah dipakai", nil)
		}
		if errors.Is(err, repository.ErrBarcodeGenerationFailed) {
			return StockItemSummary{}, apperror.Conflict("gagal membuat barcode unik, coba lagi", nil)
		}
		return StockItemSummary{}, apperror.Internal("failed to create stock item", err)
	}

	return toStockItemSummary(created, ProductRef{PublicID: product.PublicID, Name: product.Name}, product.WeightGram), nil
}

func (s *stockItemService) List(ctx context.Context, input ListStockItemsInput) (StockItemListResult, error) {
	product, err := s.productRepo.FindByPublicID(ctx, input.ProductPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrProductNotFound) {
			return StockItemListResult{}, apperror.NotFound("produk tidak ditemukan", nil)
		}
		return StockItemListResult{}, apperror.Internal("failed to fetch product", err)
	}

	page := input.Page
	if page <= 0 {
		page = defaultStockItemPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultStockItemLimit
	}
	if limit > maxStockItemLimit {
		limit = maxStockItemLimit
	}

	filter := repository.StockItemFilter{
		Search: strings.TrimSpace(input.Search),
		Page:   page,
		Limit:  limit,
	}

	if input.Status != "" {
		if _, ok := allowedStockStatuses[input.Status]; !ok {
			return StockItemListResult{}, apperror.BadRequest("status harus AVAILABLE, SOLD, VOID, atau ARCHIVED", nil)
		}
		status := input.Status
		filter.Status = &status
	}

	if input.Condition != "" {
		if _, ok := allowedConditions[input.Condition]; !ok {
			return StockItemListResult{}, apperror.BadRequest("condition harus GOOD atau BAD", nil)
		}
		condition := input.Condition
		filter.Condition = &condition
	}

	items, total, err := s.stockItemRepo.ListByProduct(ctx, product.ID, filter)
	if err != nil {
		return StockItemListResult{}, apperror.Internal("failed to list stock items", err)
	}

	summaries := make([]StockItemSummary, 0, len(items))
	for i := range items {
		summaries = append(summaries, toStockItemSummaryFromRefs(&items[i]))
	}

	return StockItemListResult{
		Items:      summaries,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *stockItemService) ListAll(ctx context.Context, input ListAllStockItemsInput) (StockItemListResult, error) {
	page := input.Page
	if page <= 0 {
		page = defaultStockItemPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultStockItemLimit
	}
	if limit > maxStockItemLimit {
		limit = maxStockItemLimit
	}

	filter := repository.StockItemFilter{
		Search: strings.TrimSpace(input.Search),
		Page:   page,
		Limit:  limit,
	}

	if input.ProductPublicID != "" {
		product, err := s.productRepo.FindByPublicID(ctx, input.ProductPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrProductNotFound) {
				return StockItemListResult{}, apperror.NotFound("produk tidak ditemukan", nil)
			}
			return StockItemListResult{}, apperror.Internal("failed to fetch product", err)
		}
		filter.ProductID = &product.ID
	}

	if input.Status != "" {
		if _, ok := allowedStockStatuses[input.Status]; !ok {
			return StockItemListResult{}, apperror.BadRequest("status harus AVAILABLE, SOLD, VOID, atau ARCHIVED", nil)
		}
		status := input.Status
		filter.Status = &status
	}

	if input.Condition != "" {
		if _, ok := allowedConditions[input.Condition]; !ok {
			return StockItemListResult{}, apperror.BadRequest("condition harus GOOD atau BAD", nil)
		}
		condition := input.Condition
		filter.Condition = &condition
	}

	items, total, err := s.stockItemRepo.List(ctx, filter)
	if err != nil {
		return StockItemListResult{}, apperror.Internal("failed to list stock items", err)
	}

	summaries := make([]StockItemSummary, 0, len(items))
	for i := range items {
		summaries = append(summaries, toStockItemSummaryFromRefs(&items[i]))
	}

	return StockItemListResult{
		Items:      summaries,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *stockItemService) Get(ctx context.Context, publicID string) (StockItemSummary, error) {
	item, err := s.stockItemRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrStockItemNotFound) {
			return StockItemSummary{}, apperror.NotFound("unit stok tidak ditemukan", nil)
		}
		return StockItemSummary{}, apperror.Internal("failed to fetch stock item", err)
	}
	return toStockItemSummaryFromRefs(item), nil
}

func (s *stockItemService) GetLabel(ctx context.Context, publicID string) (StockItemLabel, error) {
	item, err := s.stockItemRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrStockItemNotFound) {
			return StockItemLabel{}, apperror.NotFound("unit stok tidak ditemukan", nil)
		}
		return StockItemLabel{}, apperror.Internal("failed to fetch stock item", err)
	}
	return StockItemLabel{
		Barcode:      item.Barcode,
		ProductName:  item.ProductName,
		WeightGram:   item.ProductWeight,
		SerialNumber: item.SerialNumber,
	}, nil
}

func (s *stockItemService) Update(ctx context.Context, input UpdateStockItemInput) (StockItemSummary, error) {
	serialNumber := strings.TrimSpace(input.SerialNumber)
	purchaseDate, err := validateStockItemFields(serialNumber, input.Condition, input.PurchasePrice, input.PurchaseDate)
	if err != nil {
		return StockItemSummary{}, err
	}
	if err := validateProductionYear(input.ProductionYear); err != nil {
		return StockItemSummary{}, err
	}

	existing, err := s.stockItemRepo.FindByPublicID(ctx, input.PublicID)
	if err != nil {
		if errors.Is(err, repository.ErrStockItemNotFound) {
			return StockItemSummary{}, apperror.NotFound("unit stok tidak ditemukan", nil)
		}
		return StockItemSummary{}, apperror.Internal("failed to fetch stock item", err)
	}
	if existing.Status == "SOLD" {
		return StockItemSummary{}, apperror.Conflict("unit sudah terjual (SOLD), tidak bisa diedit", nil)
	}
	if existing.Status == "ARCHIVED" {
		return StockItemSummary{}, apperror.Conflict("unit sudah diarsipkan, tidak bisa diedit", nil)
	}

	existing.SerialNumber = serialNumber
	existing.Condition = input.Condition
	existing.PurchasePrice = input.PurchasePrice
	existing.PurchaseDate = purchaseDate
	existing.ProductionYear = input.ProductionYear
	existing.Notes = nilIfEmpty(input.Notes)

	if err := s.stockItemRepo.Update(ctx, &existing.StockItem); err != nil {
		if errors.Is(err, repository.ErrStockItemNotFound) {
			return StockItemSummary{}, apperror.NotFound("unit stok tidak ditemukan", nil)
		}
		return StockItemSummary{}, apperror.Internal("failed to update stock item", err)
	}

	return toStockItemSummaryFromRefs(existing), nil
}

func (s *stockItemService) Delete(ctx context.Context, publicID string) error {
	archived, err := s.stockItemRepo.Delete(ctx, publicID)
	if err != nil {
		return apperror.Internal("failed to archive stock item", err)
	}
	if archived {
		return nil
	}

	// Not archived — disambiguate "doesn't exist" (404) from "exists but not
	// AVAILABLE/SOLD" (409). Only VOID and already-ARCHIVED can land here:
	// AVAILABLE and SOLD both succeed above.
	existing, err := s.stockItemRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrStockItemNotFound) {
			return apperror.NotFound("unit stok tidak ditemukan", nil)
		}
		return apperror.Internal("failed to fetch stock item", err)
	}
	switch existing.Status {
	case "ARCHIVED":
		return apperror.Conflict("unit sudah diarsipkan sebelumnya", nil)
	case "VOID":
		return apperror.Conflict("unit sudah void (dibatalkan), tidak bisa diarsipkan", nil)
	default:
		return apperror.Conflict("unit tidak bisa diarsipkan", nil)
	}
}

func (s *stockItemService) Lookup(ctx context.Context, barcode, transactionType string) (StockItemSummary, bool, error) {
	item, err := s.stockItemRepo.FindByBarcode(ctx, barcode)
	if err != nil {
		if errors.Is(err, repository.ErrStockItemNotFound) {
			return StockItemSummary{}, false, apperror.NotFound("barcode tidak ditemukan", nil)
		}
		return StockItemSummary{}, false, apperror.Internal("failed to fetch stock item", err)
	}
	if item.Status != "AVAILABLE" {
		if item.Status == "ARCHIVED" {
			return StockItemSummary{}, false, apperror.Conflict("unit sudah diarsipkan", nil)
		}
		return StockItemSummary{}, false, apperror.Conflict("unit sudah terjual (SOLD)", nil)
	}

	requiresConfirmation := transactionType == "SELL" && item.Condition == "BAD"
	return toStockItemSummaryFromRefs(item), requiresConfirmation, nil
}

// validateStockItemFields returns the parsed purchase date on success.
func validateStockItemFields(serialNumber, condition string, purchasePrice float64, purchaseDateRaw string) (time.Time, error) {
	if serialNumber == "" {
		return time.Time{}, apperror.UnprocessableEntity("serial_number wajib diisi", nil)
	}
	if _, ok := allowedConditions[condition]; !ok {
		return time.Time{}, apperror.UnprocessableEntity("condition wajib diisi dan harus GOOD atau BAD", nil)
	}
	if purchasePrice <= 0 {
		return time.Time{}, apperror.UnprocessableEntity("purchase_price wajib diisi lebih besar dari 0", nil)
	}
	purchaseDate, err := time.Parse(stockItemDateLayout, strings.TrimSpace(purchaseDateRaw))
	if err != nil {
		return time.Time{}, apperror.UnprocessableEntity("purchase_date wajib diisi dengan format YYYY-MM-DD", nil)
	}
	return purchaseDate, nil
}

// validateProductionYear is optional — not every product/unit has a known
// production/mint year — but when supplied it must be a plausible year, not
// an arbitrary integer.
func validateProductionYear(year *int) error {
	if year == nil {
		return nil
	}
	currentYear := time.Now().Year()
	if *year < 2000 || *year > currentYear+1 {
		return apperror.UnprocessableEntity("production_year tidak valid", nil)
	}
	return nil
}

func toStockItemSummary(s *model.StockItem, product ProductRef, weightGram float64) StockItemSummary {
	notes := ""
	if s.Notes != nil {
		notes = *s.Notes
	}
	return StockItemSummary{
		PublicID:       s.PublicID,
		Product:        product,
		WeightGram:     weightGram,
		Barcode:        s.Barcode,
		SerialNumber:   s.SerialNumber,
		Condition:      s.Condition,
		PurchasePrice:  s.PurchasePrice,
		PurchaseDate:   s.PurchaseDate.Format(stockItemDateLayout),
		ProductionYear: s.ProductionYear,
		Status:         s.Status,
		SoldAt:         s.SoldAt,
		Notes:          notes,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}

func toStockItemSummaryFromRefs(s *repository.StockItemWithRefs) StockItemSummary {
	summary := toStockItemSummary(&s.StockItem, ProductRef{PublicID: s.ProductPublicID, Name: s.ProductName}, s.ProductWeight)
	summary.SoldTo = toStockItemSoldTo(s)
	return summary
}

// toStockItemSoldTo maps the repository's raw SELL/SELL_SUPPLIER join into
// the CUSTOMER/SUPPLIER-tagged public shape — nil unless every joined
// column came back non-NULL (a SOLD unit with a currently-COMPLETED sale).
func toStockItemSoldTo(s *repository.StockItemWithRefs) *StockItemSoldTo {
	if s.SoldToType == nil || s.SoldToPublicID == nil || s.SoldToName == nil {
		return nil
	}
	soldToType := "CUSTOMER"
	if *s.SoldToType == "SELL_SUPPLIER" {
		soldToType = "SUPPLIER"
	}
	return &StockItemSoldTo{
		Type:     soldToType,
		PublicID: *s.SoldToPublicID,
		Name:     *s.SoldToName,
	}
}
