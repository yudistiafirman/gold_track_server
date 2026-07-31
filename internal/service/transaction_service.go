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
	"CASH":     {},
	"TRANSFER": {},
	"QRIS":     {},
}

// TransactionItemSummary is the public-facing view of one unit within a
// transaction (sold or bought) — cogs (the unit's wholesale cost) is
// deliberately not part of this DTO, it stays internal to avoid leaking
// margin data to a cashier-facing response.
type TransactionItemSummary struct {
	PublicID          string
	StockItemPublicID string
	Barcode           string
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
	Status          string
	Items           []TransactionItemSummary
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

type CreateSaleItemInput struct {
	StockItemPublicID string
	PriceTotal        float64
	Confirmed         bool
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

type TransactionListResult struct {
	Items      []TransactionSummary // header-only: Items field left nil
	Page       int
	Limit      int
	Total      int
	TotalPages int
}

type TransactionService interface {
	CreateSale(ctx context.Context, input CreateSaleInput) (TransactionSummary, error)
	CreateBuy(ctx context.Context, input CreateBuyInput) (TransactionSummary, error)
	ListByCustomer(ctx context.Context, input ListCustomerTransactionsInput) (TransactionListResult, error)
	Get(ctx context.Context, publicID string) (TransactionSummary, error)
}

type transactionService struct {
	transactionRepo repository.TransactionRepository
	stockItemRepo   repository.StockItemRepository
	productRepo     repository.ProductRepository
	customerRepo    repository.CustomerRepository
	supplierRepo    repository.SupplierRepository
	userRepo        repository.UserRepository
}

func NewTransactionService(
	transactionRepo repository.TransactionRepository,
	stockItemRepo repository.StockItemRepository,
	productRepo repository.ProductRepository,
	customerRepo repository.CustomerRepository,
	supplierRepo repository.SupplierRepository,
	userRepo repository.UserRepository,
) TransactionService {
	return &transactionService{
		transactionRepo: transactionRepo,
		stockItemRepo:   stockItemRepo,
		productRepo:     productRepo,
		customerRepo:    customerRepo,
		supplierRepo:    supplierRepo,
		userRepo:        userRepo,
	}
}

func (s *transactionService) CreateSale(ctx context.Context, input CreateSaleInput) (TransactionSummary, error) {
	if _, ok := allowedTransactionTypes[input.Type]; !ok {
		return TransactionSummary{}, apperror.BadRequest("type harus SELL atau SELL_SUPPLIER", nil)
	}
	if _, ok := allowedPaymentMethods[input.PaymentMethod]; !ok {
		return TransactionSummary{}, apperror.BadRequest("payment_method harus CASH, TRANSFER, atau QRIS", nil)
	}
	if len(input.Items) == 0 {
		return TransactionSummary{}, apperror.BadRequest("items wajib diisi minimal 1 unit", nil)
	}
	for _, item := range input.Items {
		if item.StockItemPublicID == "" {
			return TransactionSummary{}, apperror.BadRequest("stock_item_id wajib diisi di setiap item", nil)
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
	// stockItemRefs mirrors saleItems index-for-index — trySale processes
	// input.Items in order and returns transaction_items in that same
	// order, so this lets us re-attach each result to the stock item's
	// public_id/barcode without changing CreateSale's return shape.
	stockItemRefs := make([]struct{ PublicID, Barcode string }, 0, len(input.Items))
	for _, item := range input.Items {
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
		stockItemRefs = append(stockItemRefs, struct{ PublicID, Barcode string }{stockItem.PublicID, stockItem.Barcode})
	}

	transaction, items, err := s.transactionRepo.CreateSale(ctx, repository.CreateSaleInput{
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
		case errors.Is(err, repository.ErrConfirmationRequired):
			return TransactionSummary{}, apperror.Conflict("unit kondisi BAD perlu konfirmasi (confirmed=true) untuk dijual ke pelanggan", nil)
		case errors.Is(err, repository.ErrTransactionCodeGenerationFailed):
			return TransactionSummary{}, apperror.Conflict("gagal membuat kode transaksi unik, coba lagi", nil)
		default:
			return TransactionSummary{}, apperror.Internal("failed to create transaction", err)
		}
	}

	itemSummaries := make([]TransactionItemSummary, 0, len(items))
	for i, it := range items {
		itemSummaries = append(itemSummaries, TransactionItemSummary{
			PublicID:          it.PublicID,
			StockItemPublicID: stockItemRefs[i].PublicID,
			Barcode:           stockItemRefs[i].Barcode,
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
		Status:          transaction.Status,
		Items:           itemSummaries,
		CreatedAt:       transaction.CreatedAt,
		CompletedAt:     transaction.CompletedAt,
	}, nil
}

func (s *transactionService) CreateBuy(ctx context.Context, input CreateBuyInput) (TransactionSummary, error) {
	if _, ok := allowedPaymentMethods[input.PaymentMethod]; !ok {
		return TransactionSummary{}, apperror.BadRequest("payment_method harus CASH, TRANSFER, atau QRIS", nil)
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
			ProductID:    product.ID,
			SerialNumber: strings.TrimSpace(item.SerialNumber),
			Condition:    item.Condition,
			PriceTotal:   item.PriceTotal,
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
		Status:          transaction.Status,
		Items:           itemSummaries,
		CreatedAt:       transaction.CreatedAt,
		CompletedAt:     transaction.CompletedAt,
	}, nil
}
