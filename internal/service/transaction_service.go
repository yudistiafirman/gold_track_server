package service

import (
	"context"
	"errors"
	"time"

	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
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

// TransactionItemSummary is the public-facing view of one sold unit within
// a transaction — cogs (the unit's wholesale cost) is deliberately not
// part of this DTO, it stays internal to avoid leaking margin data to a
// cashier-facing response.
type TransactionItemSummary struct {
	PublicID     string
	ProductName  string
	WeightGram   float64
	PricePerGram float64
	PriceTotal   float64
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

type TransactionService interface {
	CreateSale(ctx context.Context, input CreateSaleInput) (TransactionSummary, error)
}

type transactionService struct {
	transactionRepo repository.TransactionRepository
	stockItemRepo   repository.StockItemRepository
	customerRepo    repository.CustomerRepository
	supplierRepo    repository.SupplierRepository
	userRepo        repository.UserRepository
}

func NewTransactionService(
	transactionRepo repository.TransactionRepository,
	stockItemRepo repository.StockItemRepository,
	customerRepo repository.CustomerRepository,
	supplierRepo repository.SupplierRepository,
	userRepo repository.UserRepository,
) TransactionService {
	return &transactionService{
		transactionRepo: transactionRepo,
		stockItemRepo:   stockItemRepo,
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
	for _, it := range items {
		itemSummaries = append(itemSummaries, TransactionItemSummary{
			PublicID:     it.PublicID,
			ProductName:  it.ProductName,
			WeightGram:   it.WeightGram,
			PricePerGram: it.PricePerGram,
			PriceTotal:   it.PriceTotal,
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
