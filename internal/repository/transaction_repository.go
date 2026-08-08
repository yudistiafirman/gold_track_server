package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrStockItemUnavailableForSale is returned when a referenced stock item
// is no longer AVAILABLE at the moment it's locked for sale — either it
// was already SOLD, or a concurrent sale won the race. Never surfaces
// as-is; the service maps it to a 409.
var ErrStockItemUnavailableForSale = errors.New("stock item unavailable for sale")

// ErrStockItemArchivedForSale is the ErrStockItemUnavailableForSale case
// specifically for an ARCHIVED unit — split out so the service can report
// an accurate reason instead of implying the unit was SOLD.
var ErrStockItemArchivedForSale = errors.New("stock item archived, cannot sell")

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

// ErrTransactionNotFound is returned when no transaction matches the lookup.
var ErrTransactionNotFound = errors.New("transaction not found")

// ErrTransactionAlreadyCancelled is returned when Cancel is called on a
// transaction whose status isn't COMPLETED (already CANCELLED).
var ErrTransactionAlreadyCancelled = errors.New("transaction already cancelled")

// ErrStockItemAlreadyResold is returned when cancelling a BUY transaction
// whose stock item was already sold onward in a later transaction — the
// unit is no longer in the shop's possession, so the buy can't be undone.
var ErrStockItemAlreadyResold = errors.New("stock item already resold, cannot cancel buy")

const createSaleMaxAttempts = 5
const createBuyMaxAttempts = 5

const transactionColumns = `
	id, public_id::text, transaction_code, type, customer_id, supplier_id,
	total_amount::float8, total_weight::float8, payment_method, payment_ref,
	gold_price_id, notes, invoice_url, status, created_by, created_at, completed_at
`

// transactionReceiptColumns is qualified with the t. alias, unlike
// transactionColumns — the receipt query joins customers/suppliers, which
// have columns of the same name (id, public_id, name, ...), so unqualified
// references would be ambiguous.
const transactionReceiptColumns = `
	t.id, t.public_id::text, t.transaction_code, t.type, t.customer_id, t.supplier_id,
	t.total_amount::float8, t.total_weight::float8, t.payment_method, t.payment_ref,
	t.gold_price_id, t.notes, t.invoice_url, t.status, t.created_by, t.created_at, t.completed_at,
	c.name, c.phone, c.address,
	s.name, s.phone, s.address
`

const transactionItemColumns = `
	ti.id, ti.public_id::text, ti.transaction_id, ti.stock_item_id, ti.product_name,
	ti.weight_gram::float8, ti.price_per_gram::float8, ti.price_total::float8, ti.cogs::float8, ti.created_at,
	si.public_id::text, si.barcode, si.serial_number
`

const transactionItemFrom = `
	FROM transaction_items ti
	JOIN stock_items si ON si.id = ti.stock_item_id
`

// TransactionItemWithStockRef is a transaction_item joined with its stock
// item's identity — the detail/struk view needs the barcode/public_id, not
// just the internal FK, without a second round trip per row.
type TransactionItemWithStockRef struct {
	model.TransactionItem
	StockItemPublicID string
	Barcode           string
	SerialNumber      string
}

// TransactionWithParties is a transaction joined with its counterparty's
// display fields (customer for SELL/BUY, supplier for SELL_SUPPLIER) — the
// receipt view needs a name/phone/address to print, not just the internal
// FK. Only one side is ever non-nil, matching the transaction's type.
type TransactionWithParties struct {
	model.Transaction
	CustomerName    *string
	CustomerPhone   *string
	CustomerAddress *string
	SupplierName    *string
	SupplierPhone   *string
	SupplierAddress *string
}

// TransactionFilter narrows TransactionRepository.ListByCustomer.
type TransactionFilter struct {
	Page  int
	Limit int
}

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
	ProductID      int64
	SerialNumber   string
	Condition      string
	PriceTotal     float64
	ProductionYear *int // optional
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
	SerialNumber      string
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
	// ListByCustomer returns header-only transaction rows (type SELL/BUY)
	// for one customer, newest first, plus the total count ignoring
	// pagination.
	ListByCustomer(ctx context.Context, customerID int64, filter TransactionFilter) ([]model.Transaction, int, error)
	// FindByPublicID returns one transaction with its full
	// transaction_items (each joined with its stock item's public_id/barcode),
	// regardless of type — used for the detail/struk view.
	FindByPublicID(ctx context.Context, publicID string) (*model.Transaction, []TransactionItemWithStockRef, error)
	// FindReceiptByPublicID is like FindByPublicID but also joins in the
	// counterparty's (customer or supplier) display fields — used for the
	// receipt view, which needs a name/phone/address to print.
	FindReceiptByPublicID(ctx context.Context, publicID string) (*TransactionWithParties, []TransactionItemWithStockRef, error)
	// SetInvoiceURL caches the receipt's canonical URL on first generation.
	// Idempotent — the value is a pure function of the transaction's own
	// public_id, so a concurrent caller racing to set it computes the same
	// value, no lost-update risk.
	SetInvoiceURL(ctx context.Context, id int64, url string) error
	// Cancel locks the transaction row, verifies it's still COMPLETED, and
	// reverses its stock effects — SELL/SELL_SUPPLIER units go back to
	// AVAILABLE, BUY-created units go to VOID (never AVAILABLE again: the
	// unit was never legitimately in stock) — then flips the transaction to
	// CANCELLED. All in one DB transaction, row-locked against concurrent
	// double-cancel.
	Cancel(ctx context.Context, publicID string) (*model.Transaction, error)
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
			if status == "ARCHIVED" {
				return nil, nil, ErrStockItemArchivedForSale
			}
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
	id           int64
	publicID     string
	barcode      string
	serialNumber string
	productName  string
	weightGram   float64
	priceTotal   float64
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
		var productName string
		var weightGram float64
		if err := tx.QueryRow(ctx, `SELECT name, weight_gram::float8 FROM products WHERE id = $1`, item.ProductID).
			Scan(&productName, &weightGram); err != nil {
			return nil, nil, fmt.Errorf("fetch product for buy item: %w", err)
		}

		barcode, err := nextStockItemBarcode(ctx, tx)
		if err != nil {
			return nil, nil, err
		}

		const insertStockItemQuery = `
			INSERT INTO stock_items (product_id, barcode, serial_number, condition, purchase_price, purchase_date, production_year, status, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'AVAILABLE', $8)
			RETURNING id, public_id::text
		`
		var stockItemID int64
		var stockItemPublicID string
		err = tx.QueryRow(ctx, insertStockItemQuery, item.ProductID, barcode, item.SerialNumber, item.Condition, item.PriceTotal, purchaseDate, item.ProductionYear, input.CreatedBy).
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
			id:           stockItemID,
			publicID:     stockItemPublicID,
			barcode:      barcode,
			serialNumber: item.SerialNumber,
			productName:  productName,
			weightGram:   weightGram,
			priceTotal:   item.PriceTotal,
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
			SerialNumber:      ns.serialNumber,
		})
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit tx: %w", err)
	}

	return transaction, results, nil
}

func scanTransaction(row pgx.Row, t *model.Transaction) error {
	return row.Scan(
		&t.ID, &t.PublicID, &t.TransactionCode, &t.Type, &t.CustomerID, &t.SupplierID,
		&t.TotalAmount, &t.TotalWeight, &t.PaymentMethod, &t.PaymentRef,
		&t.GoldPriceID, &t.Notes, &t.InvoiceURL, &t.Status, &t.CreatedBy, &t.CreatedAt, &t.CompletedAt,
	)
}

func scanTransactionItemWithStockRef(row pgx.Row, it *TransactionItemWithStockRef) error {
	return row.Scan(
		&it.ID, &it.PublicID, &it.TransactionID, &it.StockItemID, &it.ProductName,
		&it.WeightGram, &it.PricePerGram, &it.PriceTotal, &it.COGS, &it.CreatedAt,
		&it.StockItemPublicID, &it.Barcode, &it.SerialNumber,
	)
}

func (r *transactionRepository) ListByCustomer(ctx context.Context, customerID int64, filter TransactionFilter) ([]model.Transaction, int, error) {
	const where = `WHERE customer_id = $1 AND type IN ('SELL', 'BUY')`

	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM transactions `+where, customerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count customer transactions: %w", err)
	}

	query := `SELECT ` + transactionColumns + ` FROM transactions ` + where +
		` ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := r.db.Query(ctx, query, customerID, filter.Limit, (filter.Page-1)*filter.Limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list customer transactions: %w", err)
	}
	defer rows.Close()

	var transactions []model.Transaction
	for rows.Next() {
		var t model.Transaction
		if err := scanTransaction(rows, &t); err != nil {
			return nil, 0, fmt.Errorf("scan transaction row: %w", err)
		}
		transactions = append(transactions, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list customer transactions: %w", err)
	}
	return transactions, total, nil
}

func (r *transactionRepository) FindByPublicID(ctx context.Context, publicID string) (*model.Transaction, []TransactionItemWithStockRef, error) {
	query := `SELECT ` + transactionColumns + ` FROM transactions WHERE public_id = $1`

	var t model.Transaction
	if err := scanTransaction(r.db.QueryRow(ctx, query, publicID), &t); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrTransactionNotFound
		}
		return nil, nil, fmt.Errorf("find transaction by public id: %w", err)
	}

	itemsQuery := `SELECT ` + transactionItemColumns + transactionItemFrom + `WHERE ti.transaction_id = $1 ORDER BY ti.id`
	rows, err := r.db.Query(ctx, itemsQuery, t.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list transaction items: %w", err)
	}
	defer rows.Close()

	var items []TransactionItemWithStockRef
	for rows.Next() {
		var it TransactionItemWithStockRef
		if err := scanTransactionItemWithStockRef(rows, &it); err != nil {
			return nil, nil, fmt.Errorf("scan transaction item row: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list transaction items: %w", err)
	}

	return &t, items, nil
}

func scanTransactionWithParties(row pgx.Row, t *TransactionWithParties) error {
	return row.Scan(
		&t.ID, &t.PublicID, &t.TransactionCode, &t.Type, &t.CustomerID, &t.SupplierID,
		&t.TotalAmount, &t.TotalWeight, &t.PaymentMethod, &t.PaymentRef,
		&t.GoldPriceID, &t.Notes, &t.InvoiceURL, &t.Status, &t.CreatedBy, &t.CreatedAt, &t.CompletedAt,
		&t.CustomerName, &t.CustomerPhone, &t.CustomerAddress,
		&t.SupplierName, &t.SupplierPhone, &t.SupplierAddress,
	)
}

func (r *transactionRepository) FindReceiptByPublicID(ctx context.Context, publicID string) (*TransactionWithParties, []TransactionItemWithStockRef, error) {
	query := `
		SELECT ` + transactionReceiptColumns + `
		FROM transactions t
		LEFT JOIN customers c ON c.id = t.customer_id
		LEFT JOIN suppliers s ON s.id = t.supplier_id
		WHERE t.public_id = $1
	`

	var t TransactionWithParties
	if err := scanTransactionWithParties(r.db.QueryRow(ctx, query, publicID), &t); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrTransactionNotFound
		}
		return nil, nil, fmt.Errorf("find transaction receipt by public id: %w", err)
	}

	itemsQuery := `SELECT ` + transactionItemColumns + transactionItemFrom + `WHERE ti.transaction_id = $1 ORDER BY ti.id`
	rows, err := r.db.Query(ctx, itemsQuery, t.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list transaction items: %w", err)
	}
	defer rows.Close()

	var items []TransactionItemWithStockRef
	for rows.Next() {
		var it TransactionItemWithStockRef
		if err := scanTransactionItemWithStockRef(rows, &it); err != nil {
			return nil, nil, fmt.Errorf("scan transaction item row: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list transaction items: %w", err)
	}

	return &t, items, nil
}

func (r *transactionRepository) SetInvoiceURL(ctx context.Context, id int64, url string) error {
	if _, err := r.db.Exec(ctx, `UPDATE transactions SET invoice_url = $1 WHERE id = $2`, url, id); err != nil {
		return fmt.Errorf("set invoice url: %w", err)
	}
	return nil
}

func (r *transactionRepository) Cancel(ctx context.Context, publicID string) (*model.Transaction, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var transactionID int64
	var txType, status string
	err = tx.QueryRow(ctx, `SELECT id, type, status FROM transactions WHERE public_id = $1 FOR UPDATE`, publicID).
		Scan(&transactionID, &txType, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("lock transaction: %w", err)
	}
	if status != "COMPLETED" {
		return nil, ErrTransactionAlreadyCancelled
	}

	rows, err := tx.Query(ctx, `SELECT stock_item_id FROM transaction_items WHERE transaction_id = $1`, transactionID)
	if err != nil {
		return nil, fmt.Errorf("list transaction items for cancel: %w", err)
	}
	var stockItemIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan transaction item stock_item_id: %w", err)
		}
		stockItemIDs = append(stockItemIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list transaction items for cancel: %w", err)
	}
	rows.Close()

	if txType == "BUY" {
		// Cancel wins over an in-between archive: a unit archived after this
		// BUY (still AVAILABLE or now ARCHIVED) just falls through to VOID
		// same as the normal case — only an actually-resold unit (SOLD)
		// blocks the cancel, since the shop no longer has it to give back.
		for _, id := range stockItemIDs {
			var itemStatus string
			if err := tx.QueryRow(ctx, `SELECT status FROM stock_items WHERE id = $1 FOR UPDATE`, id).Scan(&itemStatus); err != nil {
				return nil, fmt.Errorf("lock stock item for buy cancel: %w", err)
			}
			if itemStatus == "SOLD" {
				return nil, ErrStockItemAlreadyResold
			}
			if _, err := tx.Exec(ctx, `UPDATE stock_items SET status = 'VOID', updated_at = now() WHERE id = $1`, id); err != nil {
				return nil, fmt.Errorf("void stock item: %w", err)
			}
		}
	} else {
		// Cancel wins over an in-between archive here too — reverting
		// straight back to AVAILABLE regardless of the unit's current
		// status is intentional: cancelling the sale that made it dead
		// stock is exactly what should bring it back to life. Anyone who
		// wants a unit permanently out of circulation needs to cancel
		// first, then archive — not the other way around.
		for _, id := range stockItemIDs {
			const query = `UPDATE stock_items SET status = 'AVAILABLE', sold_at = NULL, updated_at = now() WHERE id = $1`
			if _, err := tx.Exec(ctx, query, id); err != nil {
				return nil, fmt.Errorf("revert stock item to available: %w", err)
			}
		}
	}

	query := `UPDATE transactions SET status = 'CANCELLED' WHERE id = $1 RETURNING ` + transactionColumns
	var t model.Transaction
	if err := scanTransaction(tx.QueryRow(ctx, query, transactionID), &t); err != nil {
		return nil, fmt.Errorf("cancel transaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}

	return &t, nil
}
