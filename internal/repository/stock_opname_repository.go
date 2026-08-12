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

// ErrStockOpnameNotFound is returned when no stock opname matches the lookup.
var ErrStockOpnameNotFound = errors.New("stock opname not found")

// ErrStockOpnameNotInProgress is returned when a scan or complete is
// attempted on a session whose status isn't IN_PROGRESS.
var ErrStockOpnameNotInProgress = errors.New("stock opname not in progress")

// ErrStockItemAlreadyScanned is returned when the same unit is scanned
// twice within one opname session.
var ErrStockItemAlreadyScanned = errors.New("stock item already scanned in this opname")

// ErrOpnameCodeConflict signals a unique-violation race on
// stock_opnames.opname_code between the COUNT-based sequence computation
// and the INSERT — caller should recompute and retry. Never surfaces as-is.
var ErrOpnameCodeConflict = errors.New("opname code conflict")

// ErrOpnameCodeGenerationFailed is returned once CreateWithGeneratedCode
// exhausts its retry budget without landing a unique opname_code.
var ErrOpnameCodeGenerationFailed = errors.New("failed to generate a unique opname code")

// ErrStockOpnameAlreadyInProgress is returned when Create is rejected
// because another session is still IN_PROGRESS — guarded at the DB level by
// uq_stock_opnames_single_in_progress (a partial unique index on status),
// not a pre-check-then-insert, so a concurrent double-create can't slip two
// IN_PROGRESS rows past each other.
var ErrStockOpnameAlreadyInProgress = errors.New("another stock opname session is already in progress")

const createOpnameMaxAttempts = 5

const uqStockOpnamesOpnameCode = "uq_stock_opnames_opname_code"
const uqStockOpnamesSingleInProgress = "uq_stock_opnames_single_in_progress"

const stockOpnameColumns = `id, public_id::text, opname_code, opname_date, status, notes, created_by, created_at`

const stockOpnameItemWithStockRefColumns = `
	soi.id, soi.public_id::text, soi.opname_id, soi.stock_item_id, soi.system_status,
	soi.physical_status, soi.result, soi.notes,
	si.public_id::text, si.barcode, p.name
`

const stockOpnameItemWithStockRefFrom = `
	FROM stock_opname_items soi
	JOIN stock_items si ON si.id = soi.stock_item_id
	JOIN products p ON p.id = si.product_id
`

// StockOpnameItemWithStockRef joins in the scanned unit's barcode/product
// name — the detail view needs those for display, not just the internal FK.
type StockOpnameItemWithStockRef struct {
	model.StockOpnameItem
	StockItemPublicID string
	Barcode           string
	ProductName       string
}

type CreateOpnameInput struct {
	Notes     *string
	CreatedBy int64
}

type StockOpnameFilter struct {
	Status *string // IN_PROGRESS | COMPLETED | CANCELLED, nil = no filter
	Page   int
	Limit  int
}

type StockOpnameRepository interface {
	// CreateWithGeneratedCode counts existing opnames sharing the day's
	// code prefix, inserts, retrying on a concurrent opname_code
	// unique-violation race — same shape as purchase_order_repository.go's
	// CreateWithGeneratedCode. Rejects with ErrStockOpnameAlreadyInProgress
	// (never retried) if another session is still IN_PROGRESS.
	CreateWithGeneratedCode(ctx context.Context, input CreateOpnameInput) (*model.StockOpname, error)
	// List returns opname sessions matching filter (header only, no items),
	// newest first, along with the total count ignoring pagination.
	List(ctx context.Context, filter StockOpnameFilter) ([]model.StockOpname, int, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.StockOpname, []StockOpnameItemWithStockRef, error)
	// Scan locks the opname row (FOR UPDATE), verifies IN_PROGRESS, looks
	// up the unit by barcode (ErrStockItemNotFound if no stock_items row
	// matches at all), rejects an already-scanned unit
	// (ErrStockItemAlreadyScanned), inserts one stock_opname_items row
	// (system_status = the unit's current status, physical_status =
	// FOUND, result = MATCH if AVAILABLE else UNEXPECTED) — all in one DB
	// transaction.
	Scan(ctx context.Context, opnamePublicID, barcode string) (*StockOpnameItemWithStockRef, error)
	// Complete locks the opname row (FOR UPDATE), verifies IN_PROGRESS,
	// inserts one MISSING row per AVAILABLE stock item not already present
	// among this opname's items (single set-based INSERT...SELECT), then
	// flips status to COMPLETED — all in one DB transaction.
	Complete(ctx context.Context, publicID string) error
	// PendingItems returns every AVAILABLE stock item not yet scanned in
	// this session — exactly what Complete would insert as MISSING rows if
	// run right now — joined with product sku/name/weight_gram so the UI
	// can list and group them by weight before Complete (see
	// stock_opname_service.go's use in Get/Scan).
	PendingItems(ctx context.Context, opnameID int64) ([]PendingStockItem, error)
}

// PendingStockItem is an AVAILABLE stock item not yet scanned in an opname
// session, carrying the product fields the FE needs to display and group
// it by weight.
type PendingStockItem struct {
	StockItemPublicID string
	Barcode           string
	SKU               string
	ProductName       string
	WeightGram        float64
}

type stockOpnameRepository struct {
	db *pgxpool.Pool
}

func NewStockOpnameRepository(db *pgxpool.Pool) StockOpnameRepository {
	return &stockOpnameRepository{db: db}
}

func scanStockOpname(row pgx.Row, o *model.StockOpname) error {
	return row.Scan(&o.ID, &o.PublicID, &o.OpnameCode, &o.OpnameDate, &o.Status, &o.Notes, &o.CreatedBy, &o.CreatedAt)
}

func scanStockOpnameItemWithStockRef(row pgx.Row, it *StockOpnameItemWithStockRef) error {
	return row.Scan(
		&it.ID, &it.PublicID, &it.OpnameID, &it.StockItemID, &it.SystemStatus,
		&it.PhysicalStatus, &it.Result, &it.Notes,
		&it.StockItemPublicID, &it.Barcode, &it.ProductName,
	)
}

func (r *stockOpnameRepository) CreateWithGeneratedCode(ctx context.Context, input CreateOpnameInput) (*model.StockOpname, error) {
	for i := 0; i < createOpnameMaxAttempts; i++ {
		opname, err := r.tryCreate(ctx, input)
		if err == nil {
			return opname, nil
		}
		if errors.Is(err, ErrOpnameCodeConflict) {
			continue
		}
		return nil, err
	}
	return nil, ErrOpnameCodeGenerationFailed
}

func (r *stockOpnameRepository) tryCreate(ctx context.Context, input CreateOpnameInput) (*model.StockOpname, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	codePrefix := "OPN-" + time.Now().Format("20060102") + "-"
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM stock_opnames WHERE opname_code LIKE $1`, codePrefix+"%").Scan(&count); err != nil {
		return nil, fmt.Errorf("count stock opnames by code prefix: %w", err)
	}
	code := fmt.Sprintf("%s%04d", codePrefix, count+1)

	opname := &model.StockOpname{
		OpnameCode: code,
		Status:     "IN_PROGRESS",
		Notes:      input.Notes,
		CreatedBy:  input.CreatedBy,
	}

	const insertQuery = `
		INSERT INTO stock_opnames (opname_code, opname_date, status, notes, created_by)
		VALUES ($1, CURRENT_DATE, $2, $3, $4)
		RETURNING id, public_id::text, opname_date, created_at
	`
	err = tx.QueryRow(ctx, insertQuery, opname.OpnameCode, opname.Status, opname.Notes, opname.CreatedBy).
		Scan(&opname.ID, &opname.PublicID, &opname.OpnameDate, &opname.CreatedAt)
	if err != nil {
		if isUniqueViolationOnConstraint(err, uqStockOpnamesSingleInProgress) {
			return nil, ErrStockOpnameAlreadyInProgress
		}
		if isUniqueViolationOnConstraint(err, uqStockOpnamesOpnameCode) || isUniqueViolation(err) {
			return nil, ErrOpnameCodeConflict
		}
		return nil, fmt.Errorf("insert stock opname: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return opname, nil
}

func (r *stockOpnameRepository) List(ctx context.Context, filter StockOpnameFilter) ([]model.StockOpname, int, error) {
	conditions := []string{"1=1"}
	var args []any

	if filter.Status != nil {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int
	countQuery := `SELECT COUNT(*) FROM stock_opnames ` + where
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count stock opnames: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	listArgs := append(append([]any{}, args...), filter.Limit, (filter.Page-1)*filter.Limit)
	listQuery := `SELECT ` + stockOpnameColumns + ` FROM stock_opnames ` + where +
		fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", limitArg, offsetArg)

	rows, err := r.db.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list stock opnames: %w", err)
	}
	defer rows.Close()

	var opnames []model.StockOpname
	for rows.Next() {
		var o model.StockOpname
		if err := scanStockOpname(rows, &o); err != nil {
			return nil, 0, fmt.Errorf("scan stock opname row: %w", err)
		}
		opnames = append(opnames, o)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list stock opnames: %w", err)
	}
	return opnames, total, nil
}

func (r *stockOpnameRepository) FindByPublicID(ctx context.Context, publicID string) (*model.StockOpname, []StockOpnameItemWithStockRef, error) {
	query := `SELECT ` + stockOpnameColumns + ` FROM stock_opnames WHERE public_id = $1`

	var o model.StockOpname
	if err := scanStockOpname(r.db.QueryRow(ctx, query, publicID), &o); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrStockOpnameNotFound
		}
		return nil, nil, fmt.Errorf("find stock opname by public id: %w", err)
	}

	itemsQuery := `SELECT ` + stockOpnameItemWithStockRefColumns + stockOpnameItemWithStockRefFrom + `WHERE soi.opname_id = $1 ORDER BY soi.id`
	rows, err := r.db.Query(ctx, itemsQuery, o.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("list stock opname items: %w", err)
	}
	defer rows.Close()

	var items []StockOpnameItemWithStockRef
	for rows.Next() {
		var it StockOpnameItemWithStockRef
		if err := scanStockOpnameItemWithStockRef(rows, &it); err != nil {
			return nil, nil, fmt.Errorf("scan stock opname item row: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list stock opname items: %w", err)
	}

	return &o, items, nil
}

func (r *stockOpnameRepository) Scan(ctx context.Context, opnamePublicID, barcode string) (*StockOpnameItemWithStockRef, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var opnameID int64
	var status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM stock_opnames WHERE public_id = $1 FOR UPDATE`, opnamePublicID).
		Scan(&opnameID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStockOpnameNotFound
		}
		return nil, fmt.Errorf("lock stock opname: %w", err)
	}
	if status != "IN_PROGRESS" {
		return nil, ErrStockOpnameNotInProgress
	}

	var stockItemID int64
	var stockItemPublicID, systemStatus, productName string
	err = tx.QueryRow(ctx, `
		SELECT si.id, si.public_id::text, si.status, p.name
		FROM stock_items si
		JOIN products p ON p.id = si.product_id
		WHERE si.barcode = $1
	`, barcode).Scan(&stockItemID, &stockItemPublicID, &systemStatus, &productName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrStockItemNotFound
		}
		return nil, fmt.Errorf("find stock item by barcode: %w", err)
	}

	var alreadyScanned bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stock_opname_items WHERE opname_id = $1 AND stock_item_id = $2)`, opnameID, stockItemID).
		Scan(&alreadyScanned); err != nil {
		return nil, fmt.Errorf("check already-scanned: %w", err)
	}
	if alreadyScanned {
		return nil, ErrStockItemAlreadyScanned
	}

	result := "MATCH"
	if systemStatus != "AVAILABLE" {
		result = "UNEXPECTED"
	}
	physicalStatus := "FOUND"

	item := StockOpnameItemWithStockRef{
		StockOpnameItem: model.StockOpnameItem{
			OpnameID:       opnameID,
			StockItemID:    stockItemID,
			SystemStatus:   systemStatus,
			PhysicalStatus: &physicalStatus,
			Result:         result,
		},
		StockItemPublicID: stockItemPublicID,
		Barcode:           barcode,
		ProductName:       productName,
	}

	const insertQuery = `
		INSERT INTO stock_opname_items (opname_id, stock_item_id, system_status, physical_status, result)
		VALUES ($1, $2, $3, 'FOUND', $4)
		RETURNING id, public_id::text
	`
	if err := tx.QueryRow(ctx, insertQuery, opnameID, stockItemID, systemStatus, result).
		Scan(&item.ID, &item.PublicID); err != nil {
		return nil, fmt.Errorf("insert stock opname item: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return &item, nil
}

func (r *stockOpnameRepository) Complete(ctx context.Context, publicID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var opnameID int64
	var status string
	err = tx.QueryRow(ctx, `SELECT id, status FROM stock_opnames WHERE public_id = $1 FOR UPDATE`, publicID).
		Scan(&opnameID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrStockOpnameNotFound
		}
		return fmt.Errorf("lock stock opname: %w", err)
	}
	if status != "IN_PROGRESS" {
		return ErrStockOpnameNotInProgress
	}

	const insertMissingQuery = `
		INSERT INTO stock_opname_items (opname_id, stock_item_id, system_status, physical_status, result)
		SELECT $1, si.id, 'AVAILABLE', 'NOT_FOUND', 'MISSING'
		FROM stock_items si
		WHERE si.status = 'AVAILABLE'
		  AND NOT EXISTS (
		    SELECT 1 FROM stock_opname_items soi
		    WHERE soi.opname_id = $1 AND soi.stock_item_id = si.id
		  )
	`
	if _, err := tx.Exec(ctx, insertMissingQuery, opnameID); err != nil {
		return fmt.Errorf("insert missing stock opname items: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE stock_opnames SET status = 'COMPLETED' WHERE id = $1`, opnameID); err != nil {
		return fmt.Errorf("update stock opname status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (r *stockOpnameRepository) PendingItems(ctx context.Context, opnameID int64) ([]PendingStockItem, error) {
	const query = `
		SELECT si.public_id::text, si.barcode, p.sku, p.name, p.weight_gram::float8
		FROM stock_items si
		JOIN products p ON p.id = si.product_id
		WHERE si.status = 'AVAILABLE'
		  AND NOT EXISTS (
		    SELECT 1 FROM stock_opname_items soi
		    WHERE soi.opname_id = $1 AND soi.stock_item_id = si.id
		  )
		ORDER BY p.weight_gram, p.sku, si.barcode
	`
	rows, err := r.db.Query(ctx, query, opnameID)
	if err != nil {
		return nil, fmt.Errorf("list pending stock opname items: %w", err)
	}
	defer rows.Close()

	items := make([]PendingStockItem, 0)
	for rows.Next() {
		var it PendingStockItem
		if err := rows.Scan(&it.StockItemPublicID, &it.Barcode, &it.SKU, &it.ProductName, &it.WeightGram); err != nil {
			return nil, fmt.Errorf("scan pending stock opname item row: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending stock opname items: %w", err)
	}
	return items, nil
}
