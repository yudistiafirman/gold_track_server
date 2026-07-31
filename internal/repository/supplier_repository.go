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

// ErrSupplierNotFound is returned when no supplier matches the lookup.
var ErrSupplierNotFound = errors.New("supplier not found")

// supplierColumns are cast public_id::text so every scan target can be a
// plain Go string, regardless of pgx's uuid type mapping.
const supplierColumns = `id, public_id::text, name, phone, address, notes, is_active, created_at, updated_at`

type SupplierFilter struct {
	Search string
	Page   int
	Limit  int
}

// SupplierHistoryEntry is one row of a supplier's combined history — a
// purchase order (the shop buying from them) or a SELL_SUPPLIER
// transaction (the shop selling back to them). Source tells them apart;
// Code/Status/TotalAmount/CreatedAt are the fields both sides have in
// common (po_code vs transaction_code just become "Code").
type SupplierHistoryEntry struct {
	PublicID    string
	Source      string // PURCHASE_ORDER | SELL_SUPPLIER
	Code        string
	Status      string
	TotalAmount float64
	CreatedAt   time.Time
}

type SupplierHistoryFilter struct {
	Page, Limit int
}

type SupplierRepository interface {
	Create(ctx context.Context, s *model.Supplier) (*model.Supplier, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.Supplier, error)
	List(ctx context.Context, filter SupplierFilter) ([]model.Supplier, int, error)
	Update(ctx context.Context, s *model.Supplier) error
	SetActive(ctx context.Context, publicID string, isActive bool) error
	// ListHistory returns a supplier's combined purchase-order +
	// SELL_SUPPLIER-transaction history, newest first, via a single UNION
	// ALL query — one Postgres merge-sort, not two round trips merged in Go.
	ListHistory(ctx context.Context, supplierID int64, filter SupplierHistoryFilter) ([]SupplierHistoryEntry, int, error)
}

type supplierRepository struct {
	db *pgxpool.Pool
}

func NewSupplierRepository(db *pgxpool.Pool) SupplierRepository {
	return &supplierRepository{db: db}
}

func scanSupplier(row pgx.Row, s *model.Supplier) error {
	return row.Scan(&s.ID, &s.PublicID, &s.Name, &s.Phone, &s.Address, &s.Notes, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
}

func (r *supplierRepository) Create(ctx context.Context, s *model.Supplier) (*model.Supplier, error) {
	const query = `
		INSERT INTO suppliers (name, phone, address, notes, is_active)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, public_id::text, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query, s.Name, s.Phone, s.Address, s.Notes, s.IsActive).
		Scan(&s.ID, &s.PublicID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create supplier: %w", err)
	}
	return s, nil
}

func (r *supplierRepository) FindByPublicID(ctx context.Context, publicID string) (*model.Supplier, error) {
	query := `SELECT ` + supplierColumns + ` FROM suppliers WHERE public_id = $1`

	var s model.Supplier
	if err := scanSupplier(r.db.QueryRow(ctx, query, publicID), &s); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSupplierNotFound
		}
		return nil, fmt.Errorf("find supplier by public id: %w", err)
	}
	return &s, nil
}

func (r *supplierRepository) List(ctx context.Context, filter SupplierFilter) ([]model.Supplier, int, error) {
	var conditions []string
	var args []any

	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", len(args)))
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM suppliers ` + where
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count suppliers: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	listArgs := append(append([]any{}, args...), filter.Limit, (filter.Page-1)*filter.Limit)
	listQuery := `SELECT ` + supplierColumns + ` FROM suppliers ` + where +
		fmt.Sprintf(" ORDER BY id LIMIT $%d OFFSET $%d", limitArg, offsetArg)

	rows, err := r.db.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list suppliers: %w", err)
	}
	defer rows.Close()

	var suppliers []model.Supplier
	for rows.Next() {
		var s model.Supplier
		if err := scanSupplier(rows, &s); err != nil {
			return nil, 0, fmt.Errorf("scan supplier row: %w", err)
		}
		suppliers = append(suppliers, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list suppliers: %w", err)
	}
	return suppliers, total, nil
}

func (r *supplierRepository) Update(ctx context.Context, s *model.Supplier) error {
	const query = `
		UPDATE suppliers
		SET name = $1, phone = $2, address = $3, notes = $4, is_active = $5, updated_at = now()
		WHERE id = $6
		RETURNING updated_at
	`

	err := r.db.QueryRow(ctx, query, s.Name, s.Phone, s.Address, s.Notes, s.IsActive, s.ID).
		Scan(&s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSupplierNotFound
		}
		return fmt.Errorf("update supplier: %w", err)
	}
	return nil
}

func (r *supplierRepository) SetActive(ctx context.Context, publicID string, isActive bool) error {
	const query = `UPDATE suppliers SET is_active = $1, updated_at = now() WHERE public_id = $2`
	tag, err := r.db.Exec(ctx, query, isActive, publicID)
	if err != nil {
		return fmt.Errorf("set supplier active status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSupplierNotFound
	}
	return nil
}

func (r *supplierRepository) ListHistory(ctx context.Context, supplierID int64, filter SupplierHistoryFilter) ([]SupplierHistoryEntry, int, error) {
	const countQuery = `
		SELECT
			(SELECT COUNT(*) FROM purchase_orders WHERE supplier_id = $1) +
			(SELECT COUNT(*) FROM transactions WHERE supplier_id = $1 AND type = 'SELL_SUPPLIER')
	`
	var total int
	if err := r.db.QueryRow(ctx, countQuery, supplierID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count supplier history: %w", err)
	}

	const listQuery = `
		(
			SELECT 'PURCHASE_ORDER' AS source, po.public_id::text AS id, po.po_code AS code,
			       po.status AS status, po.total_amount::float8 AS total_amount, po.created_at AS created_at
			FROM purchase_orders po
			WHERE po.supplier_id = $1
		)
		UNION ALL
		(
			SELECT 'SELL_SUPPLIER' AS source, t.public_id::text AS id, t.transaction_code AS code,
			       t.status AS status, t.total_amount::float8 AS total_amount, t.created_at AS created_at
			FROM transactions t
			WHERE t.supplier_id = $1 AND t.type = 'SELL_SUPPLIER'
		)
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, listQuery, supplierID, filter.Limit, (filter.Page-1)*filter.Limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list supplier history: %w", err)
	}
	defer rows.Close()

	var entries []SupplierHistoryEntry
	for rows.Next() {
		var e SupplierHistoryEntry
		if err := rows.Scan(&e.Source, &e.PublicID, &e.Code, &e.Status, &e.TotalAmount, &e.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan supplier history row: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list supplier history: %w", err)
	}
	return entries, total, nil
}
