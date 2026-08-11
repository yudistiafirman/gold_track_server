package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const (
	defaultTransactionPage  = 1
	defaultTransactionLimit = 20
	maxTransactionLimit     = 100
)

var allowedTransactionTypes = map[string]struct{}{
	"SELL":          {},
	"SELL_SUPPLIER": {},
}

var allowedPaymentMethods = map[string]struct{}{
	"CASH":      {},
	"TRANSFER":  {},
	"QRIS":      {},
	"DEBIT":     {},
	"KREDIT":    {},
	"GOPAY":     {},
	"OVO":       {},
	"DANA":      {},
	"SHOPEEPAY": {},
}

const allowedPaymentMethodsMessage = "payment_method harus salah satu dari CASH, TRANSFER, QRIS, DEBIT, KREDIT, GOPAY, OVO, DANA, SHOPEEPAY"

// TransactionItemSummary is the public-facing view of one unit within a
// transaction (sold or bought) — cogs (the unit's wholesale cost) is
// deliberately not part of this DTO, it stays internal to avoid leaking
// margin data to a cashier-facing response.
type TransactionItemSummary struct {
	PublicID          string
	StockItemPublicID string
	Barcode           string
	SerialNumber      string
	ProductName       string
	WeightGram        float64
	PricePerGram      float64
	PriceTotal        float64
}

type TransactionSummary struct {
	PublicID        string
	TransactionCode string
	Type            string
	TotalAmount     float64
	TotalWeight     float64
	PaymentMethod   string
	PaymentRef      string // reference/identifier for non-cash payments (e.g. transfer ref, e-wallet name) — empty for CASH
	Status          string
	Items           []TransactionItemSummary
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

// CreateSaleItemInput is one sale item, in one of two mutually exclusive
// modes:
//
//   - Scan mode: StockItemPublicID set — an existing AVAILABLE unit.
//   - Manual mode: StockItemPublicID empty, ProductPublicID set instead —
//     gold that never passed through tracked inventory (off-catalog
//     resale, walk-through deals). CreateSale creates the stock_items row
//     and sells it in the same atomic transaction, so profit/cogs still
//     books exactly like a scanned sale.
type CreateSaleItemInput struct {
	StockItemPublicID string

	ProductPublicID string
	SerialNumber    string
	Condition       string
	CostTotal       float64
	ProductionYear  *int // optional

	PriceTotal float64
	Confirmed  bool
}

type CreateSaleInput struct {
	Type              string // SELL | SELL_SUPPLIER
	CustomerPublicID  string
	SupplierPublicID  string
	PaymentMethod     string
	PaymentRef        string
	Notes             string
	Items             []CreateSaleItemInput
	CreatedByPublicID string
}

type CreateBuyItemInput struct {
	ProductPublicID string
	SerialNumber    string
	Condition       string
	PriceTotal      float64
	ProductionYear  *int // optional
}

type CreateBuyInput struct {
	CustomerPublicID  string
	PaymentMethod     string
	PaymentRef        string
	Notes             string
	Items             []CreateBuyItemInput
	CreatedByPublicID string
}

type ListCustomerTransactionsInput struct {
	CustomerPublicID string
	Page             int
	Limit            int
}

// ListTransactionsInput backs the general (not customer/supplier-scoped)
// transaction list — Type/DateFrom/DateTo are all optional, same semantics
// as TransactionReportInput (allowedTransactionReportTypes, YYYY-MM-DD,
// inclusive day-range on created_at).
type ListTransactionsInput struct {
	Type     string
	DateFrom string
	DateTo   string
	Page     int
	Limit    int
}

// ReceiptPartySummary is the counterparty (customer or supplier) shown on
// a receipt — only one of Customer/Supplier is ever populated on
// ReceiptSummary, matching the transaction's type.
type ReceiptPartySummary struct {
	Name    string
	Phone   string
	Address string
}

// ReceiptStoreSummary is the shop's own display data, sourced from
// settings (shop_name/shop_address/shop_phone) — never hardcoded, since a
// store's details can change.
type ReceiptStoreSummary struct {
	Name    string
	Address string
	Phone   string
}

type ReceiptSummary struct {
	TransactionSummary
	Customer   *ReceiptPartySummary
	Supplier   *ReceiptPartySummary
	Store      ReceiptStoreSummary
	InvoiceURL string
}

type TransactionListResult struct {
	Items      []TransactionSummary // header-only: Items field left nil
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

// PartyRef is a lightweight {public_id, name} reference to a customer or
// supplier — used only by List (the general transaction list), which isn't
// pre-scoped to one known party like ListByCustomer/supplier ListHistory
// are.
type PartyRef struct {
	PublicID string
	Name     string
}

// TransactionListItem is one row of the general transaction list —
// TransactionSummary plus the counterparty ref. Only one of Customer/
// Supplier is ever non-nil, matching the transaction's type.
type TransactionListItem struct {
	TransactionSummary
	Customer *PartyRef
	Supplier *PartyRef
}

type GeneralTransactionListResult struct {
	Items      []TransactionListItem
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type TransactionService interface {
	CreateSale(ctx context.Context, input CreateSaleInput) (TransactionSummary, error)
	CreateBuy(ctx context.Context, input CreateBuyInput) (TransactionSummary, error)
	ListByCustomer(ctx context.Context, input ListCustomerTransactionsInput) (TransactionListResult, error)
	List(ctx context.Context, input ListTransactionsInput) (GeneralTransactionListResult, error)
	Get(ctx context.Context, publicID string) (TransactionSummary, error)
	GetReceipt(ctx context.Context, publicID string) (ReceiptSummary, error)
	Cancel(ctx context.Context, publicID string) (TransactionSummary, error)
}

type transactionService struct {
	transactionRepo repository.TransactionRepository
	stockItemRepo   repository.StockItemRepository
	productRepo     repository.ProductRepository
	customerRepo    repository.CustomerRepository
	supplierRepo    repository.SupplierRepository
	userRepo        repository.UserRepository
	settingsRepo    repository.SettingsRepository
}

func NewTransactionService(
	transactionRepo repository.TransactionRepository,
	stockItemRepo repository.StockItemRepository,
	productRepo repository.ProductRepository,
	customerRepo repository.CustomerRepository,
	supplierRepo repository.SupplierRepository,
	userRepo repository.UserRepository,
	settingsRepo repository.SettingsRepository,
) TransactionService {
	return &transactionService{
		transactionRepo: transactionRepo,
		stockItemRepo:   stockItemRepo,
		productRepo:     productRepo,
		customerRepo:    customerRepo,
		supplierRepo:    supplierRepo,
		userRepo:        userRepo,
		settingsRepo:    settingsRepo,
	}
}

func (s *transactionService) CreateSale(ctx context.Context, input CreateSaleInput) (TransactionSummary, error) {
	if _, ok := allowedTransactionTypes[input.Type]; !ok {
		return TransactionSummary{}, apperror.BadRequest("type harus SELL atau SELL_SUPPLIER", nil)
	}
	if _, ok := allowedPaymentMethods[input.PaymentMethod]; !ok {
		return TransactionSummary{}, apperror.BadRequest(allowedPaymentMethodsMessage, nil)
	}
	if len(input.Items) == 0 {
		return TransactionSummary{}, apperror.BadRequest("items wajib diisi minimal 1 unit", nil)
	}
	seenManualSerialNumbers := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		isManual := item.StockItemPublicID == ""
		if !isManual {
			if item.ProductPublicID != "" {
				return TransactionSummary{}, apperror.BadRequest("item tidak boleh mengisi stock_item_id dan product_id sekaligus", nil)
			}
		} else {
			// Manual mode — this item mints a brand-new stock_items row
			// (gold not from tracked stock), so its unit-creating fields
			// use the same 422 tier as BUY items (see CLAUDE.md
			// "Validation status-code tiers").
			if item.ProductPublicID == "" {
				return TransactionSummary{}, apperror.BadRequest("item wajib mengisi stock_item_id atau product_id", nil)
			}
			serialNumber := strings.TrimSpace(item.SerialNumber)
			if serialNumber == "" {
				return TransactionSummary{}, apperror.UnprocessableEntity("serial_number wajib diisi untuk item manual", nil)
			}
			if _, ok := allowedConditions[item.Condition]; !ok {
				return TransactionSummary{}, apperror.UnprocessableEntity("condition wajib diisi dan harus GOOD atau BAD untuk item manual", nil)
			}
			if item.CostTotal <= 0 {
				return TransactionSummary{}, apperror.UnprocessableEntity("cost_total item manual harus lebih besar dari 0", nil)
			}
			if _, dup := seenManualSerialNumbers[serialNumber]; dup {
				return TransactionSummary{}, apperror.Conflict("serial_number tidak boleh sama antar item dalam satu transaksi", nil)
			}
			seenManualSerialNumbers[serialNumber] = struct{}{}
			if err := validateProductionYear(item.ProductionYear); err != nil {
				return TransactionSummary{}, err
			}
		}
		if item.PriceTotal <= 0 {
			return TransactionSummary{}, apperror.BadRequest("price_total setiap item harus lebih besar dari 0", nil)
		}
	}

	var customerID, supplierID *int64
	switch input.Type {
	case "SELL":
		if input.CustomerPublicID == "" || input.SupplierPublicID != "" {
			return TransactionSummary{}, apperror.BadRequest("type SELL wajib mengisi customer_id dan tidak boleh mengisi supplier_id", nil)
		}
		customer, err := s.customerRepo.FindByPublicID(ctx, input.CustomerPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrCustomerNotFound) {
				return TransactionSummary{}, apperror.NotFound("pelanggan tidak ditemukan", nil)
			}
			return TransactionSummary{}, apperror.Internal("failed to fetch customer", err)
		}
		customerID = &customer.ID
	case "SELL_SUPPLIER":
		if input.SupplierPublicID == "" || input.CustomerPublicID != "" {
			return TransactionSummary{}, apperror.BadRequest("type SELL_SUPPLIER wajib mengisi supplier_id dan tidak boleh mengisi customer_id", nil)
		}
		supplier, err := s.supplierRepo.FindByPublicID(ctx, input.SupplierPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrSupplierNotFound) {
				return TransactionSummary{}, apperror.NotFound("supplier tidak ditemukan", nil)
			}
			return TransactionSummary{}, apperror.Internal("failed to fetch supplier", err)
		}
		supplierID = &supplier.ID
	}

	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return TransactionSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	saleItems := make([]repository.SaleItemInput, 0, len(input.Items))
	for _, item := range input.Items {
		if item.StockItemPublicID != "" {
			stockItem, err := s.stockItemRepo.FindByPublicID(ctx, item.StockItemPublicID)
			if err != nil {
				if errors.Is(err, repository.ErrStockItemNotFound) {
					return TransactionSummary{}, apperror.NotFound("unit stok tidak ditemukan", nil)
				}
				return TransactionSummary{}, apperror.Internal("failed to fetch stock item", err)
			}
			saleItems = append(saleItems, repository.SaleItemInput{
				StockItemID: stockItem.ID,
				PriceTotal:  item.PriceTotal,
				Confirmed:   item.Confirmed,
			})
			continue
		}

		product, err := s.productRepo.FindByPublicID(ctx, item.ProductPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrProductNotFound) {
				return TransactionSummary{}, apperror.NotFound("produk tidak ditemukan", nil)
			}
			return TransactionSummary{}, apperror.Internal("failed to fetch product", err)
		}
		if !product.IsActive {
			return TransactionSummary{}, apperror.BadRequest("produk sudah diarsipkan, tidak bisa dijual manual", nil)
		}
		saleItems = append(saleItems, repository.SaleItemInput{
			ProductID:      product.ID,
			SerialNumber:   strings.TrimSpace(item.SerialNumber),
			Condition:      item.Condition,
			CostTotal:      item.CostTotal,
			ProductionYear: item.ProductionYear,
			PriceTotal:     item.PriceTotal,
			Confirmed:      item.Confirmed,
		})
	}

	transaction, results, err := s.transactionRepo.CreateSale(ctx, repository.CreateSaleInput{
		Type:          input.Type,
		CustomerID:    customerID,
		SupplierID:    supplierID,
		PaymentMethod: input.PaymentMethod,
		PaymentRef:    nilIfEmpty(input.PaymentRef),
		Notes:         nilIfEmpty(input.Notes),
		CreatedBy:     creator.ID,
		Items:         saleItems,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrStockItemUnavailableForSale):
			return TransactionSummary{}, apperror.Conflict("unit sudah terjual (SOLD), tidak bisa dijual", nil)
		case errors.Is(err, repository.ErrStockItemArchivedForSale):
			return TransactionSummary{}, apperror.Conflict("unit sudah diarsipkan, tidak bisa dijual", nil)
		case errors.Is(err, repository.ErrConfirmationRequired):
			return TransactionSummary{}, apperror.Conflict("unit kondisi BAD perlu konfirmasi (confirmed=true) untuk dijual ke pelanggan", nil)
		case errors.Is(err, repository.ErrSerialNumberTaken):
			return TransactionSummary{}, apperror.Conflict("serial_number sudah dipakai", nil)
		case errors.Is(err, repository.ErrTransactionCodeGenerationFailed):
			return TransactionSummary{}, apperror.Conflict("gagal membuat kode transaksi unik, coba lagi", nil)
		default:
			return TransactionSummary{}, apperror.Internal("failed to create transaction", err)
		}
	}

	itemSummaries := make([]TransactionItemSummary, 0, len(results))
	for _, r := range results {
		itemSummaries = append(itemSummaries, TransactionItemSummary{
			PublicID:          r.TransactionItem.PublicID,
			StockItemPublicID: r.StockItemPublicID,
			Barcode:           r.Barcode,
			SerialNumber:      r.SerialNumber,
			ProductName:       r.TransactionItem.ProductName,
			WeightGram:        r.TransactionItem.WeightGram,
			PricePerGram:      r.TransactionItem.PricePerGram,
			PriceTotal:        r.TransactionItem.PriceTotal,
		})
	}

	return TransactionSummary{
		PublicID:        transaction.PublicID,
		TransactionCode: transaction.TransactionCode,
		Type:            transaction.Type,
		TotalAmount:     transaction.TotalAmount,
		TotalWeight:     transaction.TotalWeight,
		PaymentMethod:   transaction.PaymentMethod,
		PaymentRef:      stringOrEmpty(transaction.PaymentRef),
		Status:          transaction.Status,
		Items:           itemSummaries,
		CreatedAt:       transaction.CreatedAt,
		CompletedAt:     transaction.CompletedAt,
	}, nil
}

func (s *transactionService) CreateBuy(ctx context.Context, input CreateBuyInput) (TransactionSummary, error) {
	if _, ok := allowedPaymentMethods[input.PaymentMethod]; !ok {
		return TransactionSummary{}, apperror.BadRequest(allowedPaymentMethodsMessage, nil)
	}
	if len(input.Items) == 0 {
		return TransactionSummary{}, apperror.BadRequest("items wajib diisi minimal 1 unit", nil)
	}
	if input.CustomerPublicID == "" {
		return TransactionSummary{}, apperror.BadRequest("customer_id wajib diisi", nil)
	}

	seenSerialNumbers := make(map[string]struct{}, len(input.Items))
	for _, item := range input.Items {
		serialNumber := strings.TrimSpace(item.SerialNumber)
		if serialNumber == "" {
			return TransactionSummary{}, apperror.UnprocessableEntity("serial_number wajib diisi di setiap item", nil)
		}
		if _, ok := allowedConditions[item.Condition]; !ok {
			return TransactionSummary{}, apperror.UnprocessableEntity("condition wajib diisi dan harus GOOD atau BAD di setiap item", nil)
		}
		if item.PriceTotal <= 0 {
			return TransactionSummary{}, apperror.BadRequest("price_total setiap item harus lebih besar dari 0", nil)
		}
		if _, dup := seenSerialNumbers[serialNumber]; dup {
			return TransactionSummary{}, apperror.Conflict("serial_number tidak boleh sama antar item dalam satu transaksi", nil)
		}
		seenSerialNumbers[serialNumber] = struct{}{}
		if err := validateProductionYear(item.ProductionYear); err != nil {
			return TransactionSummary{}, err
		}
	}

	customer, err := s.customerRepo.FindByPublicID(ctx, input.CustomerPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return TransactionSummary{}, apperror.NotFound("pelanggan tidak ditemukan", nil)
		}
		return TransactionSummary{}, apperror.Internal("failed to fetch customer", err)
	}

	creator, err := s.userRepo.FindByPublicID(ctx, input.CreatedByPublicID)
	if err != nil {
		return TransactionSummary{}, apperror.Internal("failed to resolve acting user", err)
	}

	buyItems := make([]repository.BuyItemInput, 0, len(input.Items))
	for _, item := range input.Items {
		product, err := s.productRepo.FindByPublicID(ctx, item.ProductPublicID)
		if err != nil {
			if errors.Is(err, repository.ErrProductNotFound) {
				return TransactionSummary{}, apperror.NotFound("produk tidak ditemukan", nil)
			}
			return TransactionSummary{}, apperror.Internal("failed to fetch product", err)
		}
		if !product.IsActive {
			return TransactionSummary{}, apperror.BadRequest("produk sudah diarsipkan, tidak bisa menambah stok", nil)
		}
		buyItems = append(buyItems, repository.BuyItemInput{
			ProductID:      product.ID,
			SerialNumber:   strings.TrimSpace(item.SerialNumber),
			Condition:      item.Condition,
			PriceTotal:     item.PriceTotal,
			ProductionYear: item.ProductionYear,
		})
	}

	transaction, results, err := s.transactionRepo.CreateBuy(ctx, repository.CreateBuyInput{
		CustomerID:    customer.ID,
		PaymentMethod: input.PaymentMethod,
		PaymentRef:    nilIfEmpty(input.PaymentRef),
		Notes:         nilIfEmpty(input.Notes),
		CreatedBy:     creator.ID,
		Items:         buyItems,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrSerialNumberTaken):
			return TransactionSummary{}, apperror.Conflict("serial_number sudah dipakai", nil)
		case errors.Is(err, repository.ErrTransactionCodeGenerationFailed):
			return TransactionSummary{}, apperror.Conflict("gagal membuat kode transaksi/barcode unik, coba lagi", nil)
		default:
			return TransactionSummary{}, apperror.Internal("failed to create transaction", err)
		}
	}

	itemSummaries := make([]TransactionItemSummary, 0, len(results))
	for _, r := range results {
		itemSummaries = append(itemSummaries, TransactionItemSummary{
			PublicID:          r.TransactionItem.PublicID,
			StockItemPublicID: r.StockItemPublicID,
			Barcode:           r.Barcode,
			SerialNumber:      r.SerialNumber,
			ProductName:       r.TransactionItem.ProductName,
			WeightGram:        r.TransactionItem.WeightGram,
			PricePerGram:      r.TransactionItem.PricePerGram,
			PriceTotal:        r.TransactionItem.PriceTotal,
		})
	}

	return TransactionSummary{
		PublicID:        transaction.PublicID,
		TransactionCode: transaction.TransactionCode,
		Type:            transaction.Type,
		TotalAmount:     transaction.TotalAmount,
		TotalWeight:     transaction.TotalWeight,
		PaymentMethod:   transaction.PaymentMethod,
		PaymentRef:      stringOrEmpty(transaction.PaymentRef),
		Status:          transaction.Status,
		Items:           itemSummaries,
		CreatedAt:       transaction.CreatedAt,
		CompletedAt:     transaction.CompletedAt,
	}, nil
}

func (s *transactionService) ListByCustomer(ctx context.Context, input ListCustomerTransactionsInput) (TransactionListResult, error) {
	customer, err := s.customerRepo.FindByPublicID(ctx, input.CustomerPublicID)
	if err != nil {
		if errors.Is(err, repository.ErrCustomerNotFound) {
			return TransactionListResult{}, apperror.NotFound("pelanggan tidak ditemukan", nil)
		}
		return TransactionListResult{}, apperror.Internal("failed to fetch customer", err)
	}

	page := input.Page
	if page <= 0 {
		page = defaultTransactionPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultTransactionLimit
	}
	if limit > maxTransactionLimit {
		limit = maxTransactionLimit
	}

	transactions, total, err := s.transactionRepo.ListByCustomer(ctx, customer.ID, repository.TransactionFilter{
		Page:  page,
		Limit: limit,
	})
	if err != nil {
		return TransactionListResult{}, apperror.Internal("failed to list customer transactions", err)
	}

	items := make([]TransactionSummary, 0, len(transactions))
	for _, t := range transactions {
		items = append(items, TransactionSummary{
			PublicID:        t.PublicID,
			TransactionCode: t.TransactionCode,
			Type:            t.Type,
			TotalAmount:     t.TotalAmount,
			TotalWeight:     t.TotalWeight,
			PaymentMethod:   t.PaymentMethod,
			PaymentRef:      stringOrEmpty(t.PaymentRef),
			Status:          t.Status,
			CreatedAt:       t.CreatedAt,
			CompletedAt:     t.CompletedAt,
		})
	}

	return TransactionListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

// List returns header-only transaction rows across all customers/suppliers
// (each with its counterparty ref), optionally filtered by type and date
// range — backs the sales/buyback list screens (as opposed to
// /reports/transactions, which stays aggregate-only and is composed into
// the dashboard).
func (s *transactionService) List(ctx context.Context, input ListTransactionsInput) (GeneralTransactionListResult, error) {
	filter := repository.TransactionFilter{}

	if input.Type != "" {
		if _, ok := allowedTransactionReportTypes[input.Type]; !ok {
			return GeneralTransactionListResult{}, apperror.BadRequest("type harus SELL, BUY, atau SELL_SUPPLIER", nil)
		}
		filter.Type = &input.Type
	}

	dateFrom, dateTo, err := parseReportDateRange(input.DateFrom, input.DateTo)
	if err != nil {
		return GeneralTransactionListResult{}, err
	}
	filter.DateFrom = dateFrom
	filter.DateTo = dateTo

	page := input.Page
	if page <= 0 {
		page = defaultTransactionPage
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultTransactionLimit
	}
	if limit > maxTransactionLimit {
		limit = maxTransactionLimit
	}
	filter.Page = page
	filter.Limit = limit

	rows, total, err := s.transactionRepo.List(ctx, filter)
	if err != nil {
		return GeneralTransactionListResult{}, apperror.Internal("failed to list transactions", err)
	}

	items := make([]TransactionListItem, 0, len(rows))
	for _, t := range rows {
		item := TransactionListItem{
			TransactionSummary: TransactionSummary{
				PublicID:        t.PublicID,
				TransactionCode: t.TransactionCode,
				Type:            t.Type,
				TotalAmount:     t.TotalAmount,
				TotalWeight:     t.TotalWeight,
				PaymentMethod:   t.PaymentMethod,
				PaymentRef:      stringOrEmpty(t.PaymentRef),
				Status:          t.Status,
				CreatedAt:       t.CreatedAt,
				CompletedAt:     t.CompletedAt,
			},
		}
		if t.CustomerPublicID != nil && t.CustomerName != nil {
			item.Customer = &PartyRef{PublicID: *t.CustomerPublicID, Name: *t.CustomerName}
		}
		if t.SupplierPublicID != nil && t.SupplierName != nil {
			item.Supplier = &PartyRef{PublicID: *t.SupplierPublicID, Name: *t.SupplierName}
		}
		items = append(items, item)
	}

	return GeneralTransactionListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (s *transactionService) Get(ctx context.Context, publicID string) (TransactionSummary, error) {
	transaction, items, err := s.transactionRepo.FindByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return TransactionSummary{}, apperror.NotFound("transaksi tidak ditemukan", nil)
		}
		return TransactionSummary{}, apperror.Internal("failed to fetch transaction", err)
	}

	itemSummaries := make([]TransactionItemSummary, 0, len(items))
	for _, it := range items {
		itemSummaries = append(itemSummaries, TransactionItemSummary{
			PublicID:          it.PublicID,
			StockItemPublicID: it.StockItemPublicID,
			Barcode:           it.Barcode,
			SerialNumber:      it.SerialNumber,
			ProductName:       it.ProductName,
			WeightGram:        it.WeightGram,
			PricePerGram:      it.PricePerGram,
			PriceTotal:        it.PriceTotal,
		})
	}

	return TransactionSummary{
		PublicID:        transaction.PublicID,
		TransactionCode: transaction.TransactionCode,
		Type:            transaction.Type,
		TotalAmount:     transaction.TotalAmount,
		TotalWeight:     transaction.TotalWeight,
		PaymentMethod:   transaction.PaymentMethod,
		PaymentRef:      stringOrEmpty(transaction.PaymentRef),
		Status:          transaction.Status,
		Items:           itemSummaries,
		CreatedAt:       transaction.CreatedAt,
		CompletedAt:     transaction.CompletedAt,
	}, nil
}

// Cancel reverses a COMPLETED transaction's stock effects (SELL/SELL_SUPPLIER
// units go back to AVAILABLE, BUY-created units go to VOID — never
// AVAILABLE, since that unit was never legitimately bought into stock) and
// flips it to CANCELLED. Restricted to ADMIN/SUPER_ADMIN at the route level.
func (s *transactionService) Cancel(ctx context.Context, publicID string) (TransactionSummary, error) {
	if _, err := s.transactionRepo.Cancel(ctx, publicID); err != nil {
		switch {
		case errors.Is(err, repository.ErrTransactionNotFound):
			return TransactionSummary{}, apperror.NotFound("transaksi tidak ditemukan", nil)
		case errors.Is(err, repository.ErrTransactionAlreadyCancelled):
			return TransactionSummary{}, apperror.Conflict("transaksi sudah dibatalkan", nil)
		case errors.Is(err, repository.ErrStockItemAlreadyResold):
			return TransactionSummary{}, apperror.Conflict("unit hasil buyback ini sudah terjual lagi di transaksi lain, tidak bisa dibatalkan", nil)
		default:
			return TransactionSummary{}, apperror.Internal("failed to cancel transaction", err)
		}
	}

	return s.Get(ctx, publicID)
}

// receiptShopSettingKeys are the settings rows read for a receipt's store
// section — kept as a var so GetReceipt's single lookup call stays readable.
var receiptShopSettingKeys = []string{"shop_name", "shop_address", "shop_phone"}

// GetReceipt returns a transaction's full struk payload (BE-1001) —
// snapshotted items (same as Get), plus the counterparty's display data and
// the shop's own details from settings. invoice_url is cached on the
// transaction on first call: it's just a canonical reference to this same
// endpoint, not a stored file — PDF rendering and printing are entirely
// the FE print-agent's responsibility.
func (s *transactionService) GetReceipt(ctx context.Context, publicID string) (ReceiptSummary, error) {
	transaction, items, err := s.transactionRepo.FindReceiptByPublicID(ctx, publicID)
	if err != nil {
		if errors.Is(err, repository.ErrTransactionNotFound) {
			return ReceiptSummary{}, apperror.NotFound("transaksi tidak ditemukan", nil)
		}
		return ReceiptSummary{}, apperror.Internal("failed to fetch transaction", err)
	}

	var customer *ReceiptPartySummary
	if transaction.CustomerName != nil {
		customer = &ReceiptPartySummary{
			Name:    *transaction.CustomerName,
			Phone:   stringOrEmpty(transaction.CustomerPhone),
			Address: stringOrEmpty(transaction.CustomerAddress),
		}
	}
	var supplier *ReceiptPartySummary
	if transaction.SupplierName != nil {
		supplier = &ReceiptPartySummary{
			Name:    *transaction.SupplierName,
			Phone:   stringOrEmpty(transaction.SupplierPhone),
			Address: stringOrEmpty(transaction.SupplierAddress),
		}
	}

	invoiceURL := transaction.InvoiceURL
	if invoiceURL == nil {
		generated := "/api/transactions/" + transaction.PublicID + "/receipt"
		if err := s.transactionRepo.SetInvoiceURL(ctx, transaction.ID, generated); err != nil {
			return ReceiptSummary{}, apperror.Internal("failed to cache invoice url", err)
		}
		invoiceURL = &generated
	}

	shopSettings, err := s.settingsRepo.GetByKeys(ctx, receiptShopSettingKeys)
	if err != nil {
		return ReceiptSummary{}, apperror.Internal("failed to fetch shop settings", err)
	}

	itemSummaries := make([]TransactionItemSummary, 0, len(items))
	for _, it := range items {
		itemSummaries = append(itemSummaries, TransactionItemSummary{
			PublicID:          it.PublicID,
			StockItemPublicID: it.StockItemPublicID,
			Barcode:           it.Barcode,
			SerialNumber:      it.SerialNumber,
			ProductName:       it.ProductName,
			WeightGram:        it.WeightGram,
			PricePerGram:      it.PricePerGram,
			PriceTotal:        it.PriceTotal,
		})
	}

	return ReceiptSummary{
		TransactionSummary: TransactionSummary{
			PublicID:        transaction.PublicID,
			TransactionCode: transaction.TransactionCode,
			Type:            transaction.Type,
			TotalAmount:     transaction.TotalAmount,
			TotalWeight:     transaction.TotalWeight,
			PaymentMethod:   transaction.PaymentMethod,
			PaymentRef:      stringOrEmpty(transaction.PaymentRef),
			Status:          transaction.Status,
			Items:           itemSummaries,
			CreatedAt:       transaction.CreatedAt,
			CompletedAt:     transaction.CompletedAt,
		},
		Customer: customer,
		Supplier: supplier,
		Store: ReceiptStoreSummary{
			Name:    shopSettings["shop_name"],
			Address: shopSettings["shop_address"],
			Phone:   shopSettings["shop_phone"],
		},
		InvoiceURL: *invoiceURL,
	}, nil
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
