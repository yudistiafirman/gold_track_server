package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrBarcodeConflict signals a unique-violation race on stock_items.barcode
// between the COUNT-based urut computation and the INSERT — caller should
// recompute the count and retry. Never surfaces to the client.
var ErrBarcodeConflict = errors.New("barcode conflict")

// ErrBarcodeGenerationFailed is returned once CreateWithGeneratedBarcode
// exhausts its retry budget without landing a unique barcode.
var ErrBarcodeGenerationFailed = errors.New("failed to generate a unique barcode")

// ErrStockItemNotFound is returned when no stock item matches the lookup.
var ErrStockItemNotFound = errors.New("stock item not found")

const createStockItemMaxAttempts = 5

// StockItemWithRefs is a stock item joined with its product's identity —
// list/detail/label views need the product name/weight, not just the
// internal FK, without a second round trip per row.
type StockItemWithRefs struct {
	model.StockItem
	ProductPublicID string
	ProductName     string
	ProductSKU      string
	ProductWeight   float64
}

type StockItemFilter struct {
	Status    *string // AVAILABLE | SOLD, nil = no filter
	Condition *string // GOOD | BAD, nil = no filter
	Search    string  // ILIKE on serial_number
	Page      int
	Limit     int
}

const stockItemWithRefsColumns = `
	si.id, si.public_id::text, si.product_id, si.barcode, si.serial_number, si.condition,
	si.purchase_price::float8, si.purchase_date, si.supplier_id, si.po_id, si.status, si.sold_at,
	si.notes, si.created_by, si.created_at, si.updated_at,
	p.public_id::text, p.name, p.sku, p.weight_gram::float8
`

const stockItemWithRefsFrom = `
	FROM stock_items si
	JOIN products p ON p.id = si.product_id
`

type StockItemRepository interface {
	// CreateWithGeneratedBarcode counts existing stock items sharing
	// barcodePrefix, appends a zero-padded sequence number, and inserts —
	// retrying the whole count+insert unit on a concurrent unique-violation
	// race.
	CreateWithGeneratedBarcode(ctx context.Context, s *model.StockItem, barcodePrefix string) (*model.StockItem, error)
	// FindByPublicID looks up a stock item regardless of status.
	FindByPublicID(ctx context.Context, publicID string) (*StockItemWithRefs, error)
	// FindByBarcode looks up a stock item regardless of status — mirrors
	// FindByPublicID but keyed by the physical barcode instead of public_id.
	FindByBarcode(ctx context.Context, barcode string) (*StockItemWithRefs, error)
	// ListByProduct returns stock items for a single product matching filter,
	// along with the total count ignoring pagination.
	ListByProduct(ctx context.Context, productID int64, filter StockItemFilter) ([]StockItemWithRefs, int, error)
	// Update applies serial_number/condition/purchase_price/purchase_date/notes
	// and bumps updated_at — barcode and product_id are deliberately never in
	// the SET clause, so they cannot change regardless of what else is edited.
	Update(ctx context.Context, s *model.StockItem) error
	// Delete hard-deletes, guarded at the SQL level — only succeeds if the
	// row is still AVAILABLE at the moment of deletion. Returns whether a
	// row was actually removed so the caller can disambiguate "not found"
	// from "exists but not AVAILABLE".
	Delete(ctx context.Context, publicID string) (bool, error)
}

type stockItemRepository struct {
	db *pgxpool.Pool
}

func NewStockItemRepository(db *pgxpool.Pool) StockItemRepository {
	return &stockItemRepository{db: db}
}

func (r *stockItemRepository) CreateWithGeneratedBarcode(ctx context.Context, s *model.StockItem, barcodePrefix string) (*model.StockItem, error) {
	for i := 0; i < createStockItemMaxAttempts; i++ {
		created, err := r.tryCreate(ctx, s, barcodePrefix)
		if err == nil {
			return created, nil
		}
		if errors.Is(err, ErrBarcodeConflict) {
			continue
		}
		return nil, err
	}
	return nil, ErrBarcodeGenerationFailed
}

func (r *stockItemRepository) tryCreate(ctx context.Context, s *model.StockItem, barcodePrefix string) (*model.StockItem, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM stock_items WHERE barcode LIKE $1`, barcodePrefix+"%").Scan(&count); err != nil {
		return nil, fmt.Errorf("count stock items by barcode prefix: %w", err)
	}
	barcode := fmt.Sprintf("%s%04d", barcodePrefix, count+1)

	const insertQuery = `
		INSERT INTO stock_items (product_id, barcode, serial_number, condition, purchase_price, purchase_date, status, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, public_id::text, created_at, updated_at
	`
	err = tx.QueryRow(ctx, insertQuery, s.ProductID, barcode, s.SerialNumber, s.Condition, s.PurchasePrice, s.PurchaseDate, s.Status, s.Notes, s.CreatedBy).
		Scan(&s.ID, &s.PublicID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrBarcodeConflict
		}
		return nil, fmt.Errorf("insert stock item: %w", err)
	}
	s.Barcode = barcode

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return s, nil
}

func scanStockItemWithRefs(row pgx.Row, s *StockItemWithRefs) error {
	return row.Scan(
		&s.ID, &s.PublicID, &s.ProductID, &s.Barcode, &s.SerialNumber, &s.Condition,
		&s.PurchasePrice, &s.PurchaseDate, &s.SupplierID, &s.POID, &s.Status, &s.SoldAt,
		&s.Notes, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt,
		&s.ProductPublicID, &s.ProductName, &s.ProductSKU, &s.ProductWeight,
	)
}

func (r *stockItemRepository) FindByPublicID(ctx context.Context, publicID string) (*StockItemWithRefs, error) {
	query := `SELECT ` + stockItemWithRefsColumns + stockItemWithRefsFrom + `WHERE si.public_id = $1`

	var s StockItemWithRefs
	if err := scanStockItemWithRefs(r.db.QueryRow(ctx, query, publicID), &s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStockItemNotFound
		}
		return nil, fmt.Errorf("find stock item by public id: %w", err)
	}
	return &s, nil
}

func (r *stockItemRepository) FindByBarcode(ctx context.Context, barcode string) (*StockItemWithRefs, error) {
	query := `SELECT ` + stockItemWithRefsColumns + stockItemWithRefsFrom + `WHERE si.barcode = $1`

	var s StockItemWithRefs
	if err := scanStockItemWithRefs(r.db.QueryRow(ctx, query, barcode), &s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStockItemNotFound
		}
		return nil, fmt.Errorf("find stock item by barcode: %w", err)
	}
	return &s, nil
}

func (r *stockItemRepository) ListByProduct(ctx context.Context, productID int64, filter StockItemFilter) ([]StockItemWithRefs, int, error) {
	conditions := []string{"si.product_id = $1"}
	args := []any{productID}

	if filter.Status != nil {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("si.status = $%d", len(args)))
	}
	if filter.Condition != nil {
		args = append(args, *filter.Condition)
		conditions = append(conditions, fmt.Sprintf("si.condition = $%d", len(args)))
	}
	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		conditions = append(conditions, fmt.Sprintf("si.serial_number ILIKE $%d", len(args)))
	}
	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int
	countQuery := `SELECT COUNT(*) FROM stock_items si ` + where
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count stock items: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	listArgs := append(append([]any{}, args...), filter.Limit, (filter.Page-1)*filter.Limit)
	listQuery := `SELECT ` + stockItemWithRefsColumns + stockItemWithRefsFrom + where +
		fmt.Sprintf(" ORDER BY si.id LIMIT $%d OFFSET $%d", limitArg, offsetArg)

	rows, err := r.db.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list stock items: %w", err)
	}
	defer rows.Close()

	var items []StockItemWithRefs
	for rows.Next() {
		var s StockItemWithRefs
		if err := scanStockItemWithRefs(rows, &s); err != nil {
			return nil, 0, fmt.Errorf("scan stock item row: %w", err)
		}
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list stock items: %w", err)
	}
	return items, total, nil
}

func (r *stockItemRepository) Update(ctx context.Context, s *model.StockItem) error {
	const query = `
		UPDATE stock_items
		SET serial_number = $1, condition = $2, purchase_price = $3, purchase_date = $4,
			notes = $5, updated_at = now()
		WHERE id = $6
		RETURNING updated_at
	`
	err := r.db.QueryRow(ctx, query, s.SerialNumber, s.Condition, s.PurchasePrice, s.PurchaseDate, s.Notes, s.ID).
		Scan(&s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStockItemNotFound
		}
		return fmt.Errorf("update stock item: %w", err)
	}
	return nil
}

func (r *stockItemRepository) Delete(ctx context.Context, publicID string) (bool, error) {
	const query = `DELETE FROM stock_items WHERE public_id = $1 AND status = 'AVAILABLE'`
	tag, err := r.db.Exec(ctx, query, publicID)
	if err != nil {
		return false, fmt.Errorf("delete stock item: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
