package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrStockItemUnavailableForSale is returned when a referenced stock item
// is no longer AVAILABLE at the moment it's locked for sale — either it
// was already SOLD, or a concurrent sale won the race. Never surfaces
// as-is; the service maps it to a 409.
var ErrStockItemUnavailableForSale = errors.New("stock item unavailable for sale")

// ErrConfirmationRequired is returned when a BAD-condition unit is being
// sold to a customer (type SELL) without item.Confirmed set.
var ErrConfirmationRequired = errors.New("confirmation required for BAD condition unit")

// ErrTransactionCodeConflict signals a unique-violation race on
// transactions.transaction_code between the COUNT-based sequence
// computation and the INSERT — caller should recompute and retry.
var ErrTransactionCodeConflict = errors.New("transaction code conflict")

// ErrTransactionCodeGenerationFailed is returned once CreateSale exhausts
// its retry budget without landing a unique transaction_code.
var ErrTransactionCodeGenerationFailed = errors.New("failed to generate a unique transaction code")

const createSaleMaxAttempts = 5

// SaleItemInput is one scanned unit going into a sale — StockItemID is the
// internal id, already resolved from the client-supplied public_id by the
// service layer.
type SaleItemInput struct {
	StockItemID int64
	PriceTotal  float64
	Confirmed   bool
}

type CreateSaleInput struct {
	Type          string // SELL | SELL_SUPPLIER
	CustomerID    *int64
	SupplierID    *int64
	PaymentMethod string
	PaymentRef    *string
	Notes         *string
	CreatedBy     int64
	Items         []SaleItemInput
}

type TransactionRepository interface {
	// CreateSale locks every referenced stock_items row (SELECT ... FOR
	// UPDATE), verifies each is still AVAILABLE (and, for a SELL with a
	// BAD-condition unit, Confirmed), generates a unique transaction_code
	// with retry, inserts the transaction + transaction_items, and flips
	// every unit to SOLD — all in one DB transaction.
	CreateSale(ctx context.Context, input CreateSaleInput) (*model.Transaction, []model.TransactionItem, error)
}

type transactionRepository struct {
	db *pgxpool.Pool
}

func NewTransactionRepository(db *pgxpool.Pool) TransactionRepository {
	return &transactionRepository{db: db}
}

func (r *transactionRepository) CreateSale(ctx context.Context, input CreateSaleInput) (*model.Transaction, []model.TransactionItem, error) {
	for i := 0; i < createSaleMaxAttempts; i++ {
		transaction, items, err := r.trySale(ctx, input)
		if err == nil {
			return transaction, items, nil
		}
		if errors.Is(err, ErrTransactionCodeConflict) {
			continue
		}
		return nil, nil, err
	}
	return nil, nil, ErrTransactionCodeGenerationFailed
}

type lockedStockItem struct {
	stockItemID   int64
	productName   string
	weightGram    float64
	purchasePrice float64
	priceTotal    float64
}

func (r *transactionRepository) trySale(ctx context.Context, input CreateSaleInput) (*model.Transaction, []model.TransactionItem, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var totalAmount, totalWeight float64
	locked := make([]lockedStockItem, 0, len(input.Items))

	for _, item := range input.Items {
		var status, condition, productName string
		var purchasePrice, weightGram float64

		err := tx.QueryRow(ctx, `
			SELECT si.status, si.condition, si.purchase_price::float8, p.name, p.weight_gram::float8
			FROM stock_items si
			JOIN products p ON p.id = si.product_id
			WHERE si.id = $1
			FOR UPDATE
		`, item.StockItemID).Scan(&status, &condition, &purchasePrice, &productName, &weightGram)
		if err != nil {
			return nil, nil, fmt.Errorf("lock stock item: %w", err)
		}

		if status != "AVAILABLE" {
			return nil, nil, ErrStockItemUnavailableForSale
		}
		if input.Type == "SELL" && condition == "BAD" && !item.Confirmed {
			return nil, nil, ErrConfirmationRequired
		}

		totalAmount += item.PriceTotal
		totalWeight += weightGram
		locked = append(locked, lockedStockItem{
			stockItemID:   item.StockItemID,
			productName:   productName,
			weightGram:    weightGram,
			purchasePrice: purchasePrice,
			priceTotal:    item.PriceTotal,
		})
	}

	codePrefix := "TRX-" + time.Now().Format("20060102") + "-"
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM transactions WHERE transaction_code LIKE $1`, codePrefix+"%").Scan(&count); err != nil {
		return nil, nil, fmt.Errorf("count transactions by code prefix: %w", err)
	}
	code := fmt.Sprintf("%s%04d", codePrefix, count+1)

	transaction := &model.Transaction{
		TransactionCode: code,
		Type:            input.Type,
		CustomerID:      input.CustomerID,
		SupplierID:      input.SupplierID,
		TotalAmount:     totalAmount,
		TotalWeight:     totalWeight,
		PaymentMethod:   input.PaymentMethod,
		PaymentRef:      input.PaymentRef,
		Notes:           input.Notes,
		Status:          "COMPLETED",
		CreatedBy:       input.CreatedBy,
	}

	const insertTxQuery = `
		INSERT INTO transactions (transaction_code, type, customer_id, supplier_id, total_amount, total_weight, payment_method, payment_ref, notes, status, created_by, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		RETURNING id, public_id::text, created_at, completed_at
	`
	err = tx.QueryRow(ctx, insertTxQuery,
		transaction.TransactionCode, transaction.Type, transaction.CustomerID, transaction.SupplierID,
		transaction.TotalAmount, transaction.TotalWeight, transaction.PaymentMethod, transaction.PaymentRef,
		transaction.Notes, transaction.Status, transaction.CreatedBy,
	).Scan(&transaction.ID, &transaction.PublicID, &transaction.CreatedAt, &transaction.CompletedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, nil, ErrTransactionCodeConflict
		}
		return nil, nil, fmt.Errorf("insert transaction: %w", err)
	}

	items := make([]model.TransactionItem, 0, len(locked))
	const insertItemQuery = `
		INSERT INTO transaction_items (transaction_id, stock_item_id, product_name, weight_gram, price_per_gram, price_total, cogs)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, public_id::text, created_at
	`
	const updateStockItemQuery = `UPDATE stock_items SET status = 'SOLD', sold_at = now(), updated_at = now() WHERE id = $1`

	for _, li := range locked {
		pricePerGram := li.priceTotal / li.weightGram
		cogs := li.purchasePrice

		txItem := model.TransactionItem{
			TransactionID: transaction.ID,
			StockItemID:   li.stockItemID,
			ProductName:   li.productName,
			WeightGram:    li.weightGram,
			PricePerGram:  pricePerGram,
			PriceTotal:    li.priceTotal,
			COGS:          &cogs,
		}
		if err := tx.QueryRow(ctx, insertItemQuery,
			txItem.TransactionID, txItem.StockItemID, txItem.ProductName, txItem.WeightGram,
			txItem.PricePerGram, txItem.PriceTotal, txItem.COGS,
		).Scan(&txItem.ID, &txItem.PublicID, &txItem.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("insert transaction item: %w", err)
		}

		if _, err := tx.Exec(ctx, updateStockItemQuery, li.stockItemID); err != nil {
			return nil, nil, fmt.Errorf("mark stock item sold: %w", err)
		}

		items = append(items, txItem)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit tx: %w", err)
	}

	return transaction, items, nil
}
