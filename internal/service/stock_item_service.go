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
}

// StockItemSummary is the public-facing view of a stock item: only
// PublicID (UUID) ever leaves this layer, the internal bigint PK stays in
// model.StockItem/the repository.
type StockItemSummary struct {
	PublicID      string
	Product       ProductRef
	WeightGram    float64
	Barcode       string
	SerialNumber  string
	Condition     string
	PurchasePrice float64
	PurchaseDate  string // "2006-01-02"
	Status        string
	SoldAt        *time.Time
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
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
	Notes             string
	CreatedByPublicID string
}

type UpdateStockItemInput struct {
	PublicID      string
	SerialNumber  string
	Condition     string
	PurchasePrice float64
	PurchaseDate  string // "2006-01-02"
	Notes         string
}

type ListStockItemsInput struct {
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

	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return StockItemSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	barcodePrefix := product.SKU + "-"

	stockItem := &model.StockItem{
		ProductID:     product.ID,
		SerialNumber:  serialNumber,
		Condition:     input.Condition,
		PurchasePrice: input.PurchasePrice,
		PurchaseDate:  purchaseDate,
		Notes:         nilIfEmpty(input.Notes),
		Status:        "AVAILABLE",
		CreatedBy:     creator.ID,
	}

	created, err := s.stockItemRepo.CreateWithGeneratedBarcode(ctx, stockItem, barcodePrefix)
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
			return StockItemListResult{}, apperror.BadRequest("status harus AVAILABLE atau SOLD", nil)
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

	existing.SerialNumber = serialNumber
	existing.Condition = input.Condition
	existing.PurchasePrice = input.PurchasePrice
	existing.PurchaseDate = purchaseDate
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
	deleted, err := s.stockItemRepo.Delete(ctx, publicID)
	if err != nil {
		return apperror.Internal("failed to delete stock item", err)
	}
	if deleted {
		return nil
	}

	// Not deleted — disambiguate "doesn't exist" (404) from "exists but SOLD" (409).
	if _, err := s.stockItemRepo.FindByPublicID(ctx, publicID); err != nil {
		if errors.Is(err, repository.ErrStockItemNotFound) {
			return apperror.NotFound("unit stok tidak ditemukan", nil)
		}
		return apperror.Internal("failed to fetch stock item", err)
	}
	return apperror.Conflict("unit sudah terjual (SOLD), tidak bisa dihapus", nil)
}

func (s *stockItemService) Lookup(ctx context.Context, barcode, transactionType string) (StockItemSummary, bool, error) {
	item, err := s.stockItemRepo.FindByBarcode(ctx, barcode)
	if err != nil {
		if errors.Is(err, repository.ErrStockItemNotFound) {
			return StockItemSummary{}, false, apperror.NotFound("barcode tidak ditemukan", nil)
		}
		return StockItemSummary{}, false, apperror.Internal("failed to fetch stock item", err)
	}
	if item.Status == "SOLD" {
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

func toStockItemSummary(s *model.StockItem, product ProductRef, weightGram float64) StockItemSummary {
	notes := ""
	if s.Notes != nil {
		notes = *s.Notes
	}
	return StockItemSummary{
		PublicID:      s.PublicID,
		Product:       product,
		WeightGram:    weightGram,
		Barcode:       s.Barcode,
		SerialNumber:  s.SerialNumber,
		Condition:     s.Condition,
		PurchasePrice: s.PurchasePrice,
		PurchaseDate:  s.PurchaseDate.Format(stockItemDateLayout),
		Status:        s.Status,
		SoldAt:        s.SoldAt,
		Notes:         notes,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func toStockItemSummaryFromRefs(s *repository.StockItemWithRefs) StockItemSummary {
	return toStockItemSummary(&s.StockItem, ProductRef{PublicID: s.ProductPublicID, Name: s.ProductName}, s.ProductWeight)
}
