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
const createBuyMaxAttempts = 5

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

// BuyItemInput is one unit being bought from a customer — ProductID is the
// internal id, already resolved from the client-supplied public_id by the
// service layer. Unlike SaleItemInput, there's no existing stock_item to
// reference — one is created fresh per item.
type BuyItemInput struct {
	ProductID    int64
	SerialNumber string
	Condition    string
	PriceTotal   float64
}

type CreateBuyInput struct {
	CustomerID    int64
	PaymentMethod string
	PaymentRef    *string
	Notes         *string
	CreatedBy     int64
	Items         []BuyItemInput
}

// BuyItemResult pairs the created stock item's identity with its
// transaction_item row, so the service can build a response showing the
// new unit's barcode without a second query.
type BuyItemResult struct {
	TransactionItem   model.TransactionItem
	StockItemPublicID string
	Barcode           string
}

type TransactionRepository interface {
	// CreateSale locks every referenced stock_items row (SELECT ... FOR
	// UPDATE), verifies each is still AVAILABLE (and, for a SELL with a
	// BAD-condition unit, Confirmed), generates a unique transaction_code
	// with retry, inserts the transaction + transaction_items, and flips
	// every unit to SOLD — all in one DB transaction.
	CreateSale(ctx context.Context, input CreateSaleInput) (*model.Transaction, []model.TransactionItem, error)
	// CreateBuy creates one brand-new stock_items row per item (status
	// AVAILABLE, purchase_price = the item's negotiated PriceTotal,
	// purchase_date = today), a transaction_code with retry, the
	// transaction header, and every transaction_items row — all in one DB
	// transaction.
	CreateBuy(ctx context.Context, input CreateBuyInput) (*model.Transaction, []BuyItemResult, error)
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

func (r *transactionRepository) CreateBuy(ctx context.Context, input CreateBuyInput) (*model.Transaction, []BuyItemResult, error) {
	for i := 0; i < createBuyMaxAttempts; i++ {
		transaction, items, err := r.tryBuy(ctx, input)
		if err == nil {
			return transaction, items, nil
		}
		if errors.Is(err, ErrTransactionCodeConflict) || errors.Is(err, ErrBarcodeConflict) {
			continue
		}
		return nil, nil, err
	}
	return nil, nil, ErrTransactionCodeGenerationFailed
}

type newStockItem struct {
	id          int64
	publicID    string
	barcode     string
	productName string
	weightGram  float64
	priceTotal  float64
}

func (r *transactionRepository) tryBuy(ctx context.Context, input CreateBuyInput) (*model.Transaction, []BuyItemResult, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var totalAmount, totalWeight float64
	created := make([]newStockItem, 0, len(input.Items))
	purchaseDate := time.Now()

	for _, item := range input.Items {
		var sku, productName string
		var weightGram float64
		if err := tx.QueryRow(ctx, `SELECT sku, name, weight_gram::float8 FROM products WHERE id = $1`, item.ProductID).
			Scan(&sku, &productName, &weightGram); err != nil {
			return nil, nil, fmt.Errorf("fetch product for buy item: %w", err)
		}

		barcodePrefix := sku + "-"
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM stock_items WHERE barcode LIKE $1`, barcodePrefix+"%").Scan(&count); err != nil {
			return nil, nil, fmt.Errorf("count stock items by barcode prefix: %w", err)
		}
		barcode := fmt.Sprintf("%s%04d", barcodePrefix, count+1)

		const insertStockItemQuery = `
			INSERT INTO stock_items (product_id, barcode, serial_number, condition, purchase_price, purchase_date, status, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, 'AVAILABLE', $7)
			RETURNING id, public_id::text
		`
		var stockItemID int64
		var stockItemPublicID string
		err = tx.QueryRow(ctx, insertStockItemQuery, item.ProductID, barcode, item.SerialNumber, item.Condition, item.PriceTotal, purchaseDate, input.CreatedBy).
			Scan(&stockItemID, &stockItemPublicID)
		if err != nil {
			if isUniqueViolationOnConstraint(err, uqStockItemsSerialNumber) {
				return nil, nil, ErrSerialNumberTaken
			}
			if isUniqueViolation(err) {
				return nil, nil, ErrBarcodeConflict
			}
			return nil, nil, fmt.Errorf("insert stock item for buy: %w", err)
		}

		totalAmount += item.PriceTotal
		totalWeight += weightGram
		created = append(created, newStockItem{
			id:          stockItemID,
			publicID:    stockItemPublicID,
			barcode:     barcode,
			productName: productName,
			weightGram:  weightGram,
			priceTotal:  item.PriceTotal,
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
		Type:            "BUY",
		CustomerID:      &input.CustomerID,
		TotalAmount:     totalAmount,
		TotalWeight:     totalWeight,
		PaymentMethod:   input.PaymentMethod,
		PaymentRef:      input.PaymentRef,
		Notes:           input.Notes,
		Status:          "COMPLETED",
		CreatedBy:       input.CreatedBy,
	}

	const insertTxQuery = `
		INSERT INTO transactions (transaction_code, type, customer_id, total_amount, total_weight, payment_method, payment_ref, notes, status, created_by, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
		RETURNING id, public_id::text, created_at, completed_at
	`
	err = tx.QueryRow(ctx, insertTxQuery,
		transaction.TransactionCode, transaction.Type, transaction.CustomerID,
		transaction.TotalAmount, transaction.TotalWeight, transaction.PaymentMethod, transaction.PaymentRef,
		transaction.Notes, transaction.Status, transaction.CreatedBy,
	).Scan(&transaction.ID, &transaction.PublicID, &transaction.CreatedAt, &transaction.CompletedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, nil, ErrTransactionCodeConflict
		}
		return nil, nil, fmt.Errorf("insert transaction: %w", err)
	}

	results := make([]BuyItemResult, 0, len(created))
	const insertItemQuery = `
		INSERT INTO transaction_items (transaction_id, stock_item_id, product_name, weight_gram, price_per_gram, price_total, cogs)
		VALUES ($1, $2, $3, $4, $5, $6, NULL)
		RETURNING id, public_id::text, created_at
	`
	for _, ns := range created {
		pricePerGram := ns.priceTotal / ns.weightGram

		txItem := model.TransactionItem{
			TransactionID: transaction.ID,
			StockItemID:   ns.id,
			ProductName:   ns.productName,
			WeightGram:    ns.weightGram,
			PricePerGram:  pricePerGram,
			PriceTotal:    ns.priceTotal,
		}
		if err := tx.QueryRow(ctx, insertItemQuery,
			txItem.TransactionID, txItem.StockItemID, txItem.ProductName, txItem.WeightGram,
			txItem.PricePerGram, txItem.PriceTotal,
		).Scan(&txItem.ID, &txItem.PublicID, &txItem.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("insert transaction item for buy: %w", err)
		}

		results = append(results, BuyItemResult{
			TransactionItem:   txItem,
			StockItemPublicID: ns.publicID,
			Barcode:           ns.barcode,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit tx: %w", err)
	}

	return transaction, results, nil
}
