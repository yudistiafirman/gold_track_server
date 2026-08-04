package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrPOCodeConflict signals a unique-violation race on
// purchase_orders.po_code between the COUNT-based sequence computation and
// the INSERT — caller should recompute and retry. Never surfaces as-is.
var ErrPOCodeConflict = errors.New("po code conflict")

// ErrPOCodeGenerationFailed is returned once CreateWithGeneratedCode
// exhausts its retry budget without landing a unique po_code.
var ErrPOCodeGenerationFailed = errors.New("failed to generate a unique po code")

// ErrPurchaseOrderNotFound is returned when no PO matches the lookup.
var ErrPurchaseOrderNotFound = errors.New("purchase order not found")

// ErrPurchaseOrderNotReceivable is returned when a PO's status isn't
// BELUM_DITERIMA at the moment it's locked for receiving.
var ErrPurchaseOrderNotReceivable = errors.New("purchase order not receivable")

const createPOMaxAttempts = 5

const purchaseOrderWithSupplierColumns = `
	po.id, po.public_id::text, po.po_code, po.supplier_id, po.total_amount::float8,
	po.status, po.notes, po.created_by, po.created_at, po.received_at,
	s.public_id::text, s.name
`

const purchaseOrderWithSupplierFrom = `
	FROM purchase_orders po
	JOIN suppliers s ON s.id = po.supplier_id
`

const purchaseOrderItemWithProductColumns = `
	poi.id, poi.public_id::text, poi.po_id, poi.product_id, poi.quantity, poi.purchase_price::float8,
	p.public_id::text, p.name, p.sku
`

const purchaseOrderItemWithProductFrom = `
	FROM purchase_order_items poi
	JOIN products p ON p.id = poi.product_id
`

// PurchaseOrderWithSupplier is a PO joined with its supplier's identity —
// list/detail views need the name, not just the internal FK.
type PurchaseOrderWithSupplier struct {
	model.PurchaseOrder
	SupplierPublicID string
	SupplierName     string
}

// PurchaseOrderItemWithProduct is a PO item joined with its product's
// identity — purchase_order_items has no product_name snapshot column
// (unlike transaction_items), so a live join is the only way to display it.
type PurchaseOrderItemWithProduct struct {
	model.PurchaseOrderItem
	ProductPublicID string
	ProductName     string
	ProductSKU      string
}

type CreatePOItemInput struct {
	ProductID     int64
	Quantity      int
	PurchasePrice float64
}

type CreatePOInput struct {
	SupplierID int64
	Notes      *string
	CreatedBy  int64
	Items      []CreatePOItemInput
}

type POFilter struct {
	Status *string // nil = no filter
	Page   int
	Limit  int
}

// ReceiveSerialInput is one physical unit arriving — condition is captured
// per serial since a single shipment isn't guaranteed to be uniformly
// GOOD or BAD.
type ReceiveSerialInput struct {
	SerialNumber   string
	Condition      string
	ProductionYear *int // optional
}

// ReceiveItemInput is one product's physical units arriving — ProductID is
// the internal id, already resolved from the client-supplied public_id by
// the service layer. PurchasePrice is deliberately not here: the
// repository pulls it from purchase_order_items itself, never trusting the
// client to supply it again at receive time.
type ReceiveItemInput struct {
	ProductID int64
	Serials   []ReceiveSerialInput
}

type ReceivePOInput struct {
	CreatedBy int64
	Items     []ReceiveItemInput
}

type PurchaseOrderRepository interface {
	// CreateWithGeneratedCode counts existing POs sharing the day's code
	// prefix, inserts the header + items, retrying the whole unit on a
	// concurrent po_code unique-violation race.
	CreateWithGeneratedCode(ctx context.Context, input CreatePOInput) (*model.PurchaseOrder, []model.PurchaseOrderItem, error)
	List(ctx context.Context, filter POFilter) ([]PurchaseOrderWithSupplier, int, error)
	FindByPublicID(ctx context.Context, publicID string) (*PurchaseOrderWithSupplier, []PurchaseOrderItemWithProduct, error)
	// Receive locks the PO row (FOR UPDATE), verifies status=BELUM_DITERIMA,
	// creates one stock_items row per serial (barcode retry same as
	// stock_item_repository.go), flips the PO to DITERIMA+received_at —
	// all in one DB transaction. Returns the newly created units.
	Receive(ctx context.Context, poPublicID string, input ReceivePOInput) ([]model.StockItem, error)
	// Cancel is a single guarded UPDATE — only succeeds if the PO is still
	// BELUM_DITERIMA. Returns whether a row was actually updated so the
	// service can disambiguate 404 vs 409.
	Cancel(ctx context.Context, publicID string) (bool, error)
}

type purchaseOrderRepository struct {
	db *pgxpool.Pool
}

func NewPurchaseOrderRepository(db *pgxpool.Pool) PurchaseOrderRepository {
	return &purchaseOrderRepository{db: db}
}

func scanPurchaseOrderWithSupplier(row pgx.Row, po *PurchaseOrderWithSupplier) error {
	return row.Scan(
		&po.ID, &po.PublicID, &po.POCode, &po.SupplierID, &po.TotalAmount,
		&po.Status, &po.Notes, &po.CreatedBy, &po.CreatedAt, &po.ReceivedAt,
		&po.SupplierPublicID, &po.SupplierName,
	)
}

func scanPurchaseOrderItemWithProduct(row pgx.Row, it *PurchaseOrderItemWithProduct) error {
	return row.Scan(
		&it.ID, &it.PublicID, &it.POID, &it.ProductID, &it.Quantity, &it.PurchasePrice,
		&it.ProductPublicID, &it.ProductName, &it.ProductSKU,
	)
}

func (r *purchaseOrderRepository) CreateWithGeneratedCode(ctx context.Context, input CreatePOInput) (*model.PurchaseOrder, []model.PurchaseOrderItem, error) {
	for i := 0; i < createPOMaxAttempts; i++ {
		po, items, err := r.tryCreatePO(ctx, input)
		if err == nil {
			return po, items, nil
		}
		if errors.Is(err, ErrPOCodeConflict) {
			continue
		}
		return nil, nil, err
	}
	return nil, nil, ErrPOCodeGenerationFailed
}

func (r *purchaseOrderRepository) tryCreatePO(ctx context.Context, input CreatePOInput) (*model.PurchaseOrder, []model.PurchaseOrderItem, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var totalAmount float64
	for _, item := range input.Items {
		totalAmount += float64(item.Quantity) * item.PurchasePrice
	}

	codePrefix := "PO-" + time.Now().Format("20060102") + "-"
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM purchase_orders WHERE po_code LIKE $1`, codePrefix+"%").Scan(&count); err != nil {
		return nil, nil, fmt.Errorf("count purchase orders by code prefix: %w", err)
	}
	code := fmt.Sprintf("%s%04d", codePrefix, count+1)

	po := &model.PurchaseOrder{
		POCode:      code,
		SupplierID:  input.SupplierID,
		TotalAmount: totalAmount,
		Status:      "BELUM_DITERIMA",
		Notes:       input.Notes,
		CreatedBy:   input.CreatedBy,
	}

	const insertPOQuery = `
		INSERT INTO purchase_orders (po_code, supplier_id, total_amount, status, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, public_id::text, created_at
	`
	err = tx.QueryRow(ctx, insertPOQuery, po.POCode, po.SupplierID, po.TotalAmount, po.Status, po.Notes, po.CreatedBy).
		Scan(&po.ID, &po.PublicID, &po.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, nil, ErrPOCodeConflict
		}
		return nil, nil, fmt.Errorf("insert purchase order: %w", err)
	}

	items := make([]model.PurchaseOrderItem, 0, len(input.Items))
	const insertItemQuery = `
		INSERT INTO purchase_order_items (po_id, product_id, quantity, purchase_price)
		VALUES ($1, $2, $3, $4)
		RETURNING id, public_id::text
	`
	for _, in := range input.Items {
		item := model.PurchaseOrderItem{
			POID:          po.ID,
			ProductID:     in.ProductID,
			Quantity:      in.Quantity,
			PurchasePrice: in.PurchasePrice,
		}
		if err := tx.QueryRow(ctx, insertItemQuery, item.POID, item.ProductID, item.Quantity, item.PurchasePrice).
			Scan(&item.ID, &item.PublicID); err != nil {
			return nil, nil, fmt.Errorf("insert purchase order item: %w", err)
		}
		items = append(items, item)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit tx: %w", err)
	}
	return po, items, nil
}

func (r *purchaseOrderRepository) List(ctx context.Context, filter POFilter) ([]PurchaseOrderWithSupplier, int, error) {
	conditions := []string{"1=1"}
	var args []any

	if filter.Status != nil {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("po.status = $%d", len(args)))
	}
	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int
	countQuery := `SELECT COUNT(*) FROM purchase_orders po ` + where
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count purchase orders: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	listArgs := append(append([]any{}, args...), filter.Limit, (filter.Page-1)*filter.Limit)
	listQuery := `SELECT ` + purchaseOrderWithSupplierColumns + purchaseOrderWithSupplierFrom + where +
		fmt.Sprintf(" ORDER BY po.id DESC LIMIT $%d OFFSET $%d", limitArg, offsetArg)

	rows, err := r.db.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list purchase orders: %w", err)
	}
	defer rows.Close()

	var pos []PurchaseOrderWithSupplier
	for rows.Next() {
		var po PurchaseOrderWithSupplier
		if err := scanPurchaseOrderWithSupplier(rows, &po); err != nil {
			return nil, 0, fmt.Errorf("scan purchase order row: %w", err)
		}
		pos = append(pos, po)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list purchase orders: %w", err)
	}
	return pos, total, nil
}

func (r *purchaseOrderRepository) FindByPublicID(ctx context.Context, publicID string) (*PurchaseOrderWithSupplier, []PurchaseOrderItemWithProduct, error) {
	query := `SELECT ` + purchaseOrderWithSupplierColumns + purchaseOrderWithSupplierFrom + `WHERE po.public_id = $1`

	var po PurchaseOrderWithSupplier
	if err := scanPurchaseOrderWithSupplier(r.db.QueryRow(ctx, query, publicID), &po); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrPurchaseOrderNotFound
		}
		return nil, nil, fmt.Errorf("find purchase order by public id: %w", err)
	}

	itemsQuery := `SELECT ` + purchaseOrderItemWithProductColumns + purchaseOrderItemWithProductFrom + `WHERE poi.po_id = $1 ORDER BY poi.id`
	rows, err := r.db.Query(ctx, itemsQuery, po.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list purchase order items: %w", err)
	}
	defer rows.Close()

	var items []PurchaseOrderItemWithProduct
	for rows.Next() {
		var it PurchaseOrderItemWithProduct
		if err := scanPurchaseOrderItemWithProduct(rows, &it); err != nil {
			return nil, nil, fmt.Errorf("scan purchase order item row: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list purchase order items: %w", err)
	}

	return &po, items, nil
}

const createReceiveMaxAttempts = 5

func (r *purchaseOrderRepository) Receive(ctx context.Context, poPublicID string, input ReceivePOInput) ([]model.StockItem, error) {
	for i := 0; i < createReceiveMaxAttempts; i++ {
		units, err := r.tryReceive(ctx, poPublicID, input)
		if err == nil {
			return units, nil
		}
		if errors.Is(err, ErrBarcodeConflict) {
			continue
		}
		return nil, err
	}
	return nil, ErrBarcodeGenerationFailed
}

func (r *purchaseOrderRepository) tryReceive(ctx context.Context, poPublicID string, input ReceivePOInput) ([]model.StockItem, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var poID, supplierID int64
	var status string
	err = tx.QueryRow(ctx, `SELECT id, supplier_id, status FROM purchase_orders WHERE public_id = $1 FOR UPDATE`, poPublicID).
		Scan(&poID, &supplierID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPurchaseOrderNotFound
		}
		return nil, fmt.Errorf("lock purchase order: %w", err)
	}
	if status != "BELUM_DITERIMA" {
		return nil, ErrPurchaseOrderNotReceivable
	}

	var createdUnits []model.StockItem
	purchaseDate := time.Now()

	const insertStockItemQuery = `
		INSERT INTO stock_items (product_id, barcode, serial_number, condition, purchase_price, purchase_date, production_year, po_id, supplier_id, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'AVAILABLE', $10)
		RETURNING id, public_id::text, created_at, updated_at
	`

	for _, item := range input.Items {
		var purchasePrice float64
		if err := tx.QueryRow(ctx, `SELECT purchase_price::float8 FROM purchase_order_items WHERE po_id = $1 AND product_id = $2`, poID, item.ProductID).
			Scan(&purchasePrice); err != nil {
			return nil, fmt.Errorf("fetch po item purchase price: %w", err)
		}
		for _, serial := range item.Serials {
			barcode, err := nextStockItemBarcode(ctx, tx)
			if err != nil {
				return nil, err
			}

			unit := model.StockItem{
				ProductID:      item.ProductID,
				SerialNumber:   serial.SerialNumber,
				Condition:      serial.Condition,
				PurchasePrice:  purchasePrice,
				PurchaseDate:   purchaseDate,
				ProductionYear: serial.ProductionYear,
				POID:           &poID,
				SupplierID:     &supplierID,
				Status:         "AVAILABLE",
				CreatedBy:      input.CreatedBy,
			}
			err = tx.QueryRow(ctx, insertStockItemQuery,
				unit.ProductID, barcode, unit.SerialNumber, unit.Condition, unit.PurchasePrice,
				unit.PurchaseDate, unit.ProductionYear, unit.POID, unit.SupplierID, unit.CreatedBy,
			).Scan(&unit.ID, &unit.PublicID, &unit.CreatedAt, &unit.UpdatedAt)
			if err != nil {
				if isUniqueViolationOnConstraint(err, uqStockItemsSerialNumber) {
					return nil, ErrSerialNumberTaken
				}
				if isUniqueViolation(err) {
					return nil, ErrBarcodeConflict
				}
				return nil, fmt.Errorf("insert stock item for receive: %w", err)
			}
			unit.Barcode = barcode
			createdUnits = append(createdUnits, unit)
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET status = 'DITERIMA', received_at = now() WHERE id = $1`, poID); err != nil {
		return nil, fmt.Errorf("update purchase order status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return createdUnits, nil
}

func (r *purchaseOrderRepository) Cancel(ctx context.Context, publicID string) (bool, error) {
	const query = `UPDATE purchase_orders SET status = 'DIBATALKAN' WHERE public_id = $1 AND status = 'BELUM_DITERIMA'`
	tag, err := r.db.Exec(ctx, query, publicID)
	if err != nil {
		return false, fmt.Errorf("cancel purchase order: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
