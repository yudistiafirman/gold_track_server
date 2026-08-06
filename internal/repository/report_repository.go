package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TransactionReportFilter narrows ReportRepository.TransactionSummary —
// DateFrom/DateTo are each independently optional inclusive bounds.
type TransactionReportFilter struct {
	DateFrom *time.Time
	DateTo   *time.Time
	Type     *string
}

// TransactionTypeBreakdown is one row of the transaction report — one per
// type that has at least one matching transaction in the filtered range.
type TransactionTypeBreakdown struct {
	Type             string
	TransactionCount int
	TotalAmount      float64
	TotalWeight      float64
}

// StockReportRow is one active product's stock summary — LEFT JOIN'd from
// stock_items, so a product with zero AVAILABLE units still gets a row.
type StockReportRow struct {
	ProductPublicID string
	ProductName     string
	ProductSKU      string
	AvailableCount  int
	GoodCount       int
	BadCount        int
}

// SalesTypeProfitBreakdown is one row of the finance report's sales side —
// one per type that counts as a sale (SELL, SELL_SUPPLIER) with at least
// one matching transaction_item in the filtered range. Revenue and cogs
// are kept separate here (not pre-subtracted) so the service layer can
// derive gross profit per row as well as the grand total.
type SalesTypeProfitBreakdown struct {
	Type             string
	TransactionCount int // distinct transactions, not items
	TotalRevenue     float64
	TotalCOGS        float64
}

// ExpenseCategoryBreakdown is one row of the finance report's expense
// side — one per category with at least one expense in the filtered range.
type ExpenseCategoryBreakdown struct {
	CategoryPublicID string
	CategoryName     string
	TotalAmount      float64
}

// PendingPurchaseOrder is one row of the dashboard's "awaiting receipt"
// list — POs still status = BELUM_DITERIMA.
type PendingPurchaseOrder struct {
	PublicID     string
	POCode       string
	SupplierName string
	TotalAmount  float64
	CreatedAt    time.Time
}

// CashSummaryTotals is the "where the shop's money lives" snapshot: current
// gold stock value, current cash/bank balances, current external funds and
// external debts. All four are point-in-time (no date range — none of the
// underlying tables has a date column to filter by).
type CashSummaryTotals struct {
	TotalGoldValue     float64
	TotalBalance       float64
	TotalExternalFunds float64
	TotalExternalDebts float64
}

type ReportRepository interface {
	// TransactionSummary groups transactions by type within the filtered
	// date range.
	TransactionSummary(ctx context.Context, filter TransactionReportFilter) ([]TransactionTypeBreakdown, error)
	// StockSummary returns one row per active product.
	StockSummary(ctx context.Context) ([]StockReportRow, error)
	// SalesProfitByType groups SELL/SELL_SUPPLIER transaction_items by
	// transaction type within the filtered date range — the revenue/cogs
	// breakdown backing the finance report's gross profit figure.
	SalesProfitByType(ctx context.Context, dateFrom, dateTo *time.Time) ([]SalesTypeProfitBreakdown, error)
	// ExpensesByCategory groups expenses by category within the filtered
	// date range.
	ExpensesByCategory(ctx context.Context, dateFrom, dateTo *time.Time) ([]ExpenseCategoryBreakdown, error)
	// PendingPurchaseOrders returns the most recent `limit` purchase
	// orders still awaiting receipt, newest first, plus the total count of
	// all such POs (may exceed len(items) if there are more than limit).
	PendingPurchaseOrders(ctx context.Context, limit int) ([]PendingPurchaseOrder, int, error)
	// CashSummary computes the 4 cash-tracking totals in one round trip.
	CashSummary(ctx context.Context) (CashSummaryTotals, error)
}

type reportRepository struct {
	db *pgxpool.Pool
}

func NewReportRepository(db *pgxpool.Pool) ReportRepository {
	return &reportRepository{db: db}
}

func (r *reportRepository) TransactionSummary(ctx context.Context, filter TransactionReportFilter) ([]TransactionTypeBreakdown, error) {
	var conditions []string
	var args []any

	if filter.DateFrom != nil {
		args = append(args, *filter.DateFrom)
		conditions = append(conditions, fmt.Sprintf("created_at::date >= $%d", len(args)))
	}
	if filter.DateTo != nil {
		args = append(args, *filter.DateTo)
		conditions = append(conditions, fmt.Sprintf("created_at::date <= $%d", len(args)))
	}
	if filter.Type != nil {
		args = append(args, *filter.Type)
		conditions = append(conditions, fmt.Sprintf("type = $%d", len(args)))
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := `
		SELECT type, COUNT(*), COALESCE(SUM(total_amount),0)::float8, COALESCE(SUM(total_weight),0)::float8
		FROM transactions
		` + where + `
		GROUP BY type
		ORDER BY type
	`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("summarize transactions: %w", err)
	}
	defer rows.Close()

	var breakdown []TransactionTypeBreakdown
	for rows.Next() {
		var b TransactionTypeBreakdown
		if err := rows.Scan(&b.Type, &b.TransactionCount, &b.TotalAmount, &b.TotalWeight); err != nil {
			return nil, fmt.Errorf("scan transaction report row: %w", err)
		}
		breakdown = append(breakdown, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("summarize transactions: %w", err)
	}
	return breakdown, nil
}

func (r *reportRepository) StockSummary(ctx context.Context) ([]StockReportRow, error) {
	const query = `
		SELECT p.public_id::text, p.name, p.sku,
		       COUNT(si.id) FILTER (WHERE si.status = 'AVAILABLE') AS available_count,
		       COUNT(si.id) FILTER (WHERE si.status = 'AVAILABLE' AND si.condition = 'GOOD') AS good_count,
		       COUNT(si.id) FILTER (WHERE si.status = 'AVAILABLE' AND si.condition = 'BAD') AS bad_count
		FROM products p
		LEFT JOIN stock_items si ON si.product_id = p.id
		WHERE p.is_active = true
		GROUP BY p.id, p.public_id, p.name, p.sku
		ORDER BY p.name
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("summarize stock: %w", err)
	}
	defer rows.Close()

	var report []StockReportRow
	for rows.Next() {
		var row StockReportRow
		if err := rows.Scan(&row.ProductPublicID, &row.ProductName, &row.ProductSKU, &row.AvailableCount, &row.GoodCount, &row.BadCount); err != nil {
			return nil, fmt.Errorf("scan stock report row: %w", err)
		}
		report = append(report, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("summarize stock: %w", err)
	}
	return report, nil
}

func (r *reportRepository) SalesProfitByType(ctx context.Context, dateFrom, dateTo *time.Time) ([]SalesTypeProfitBreakdown, error) {
	var conditions []string
	var args []any
	if dateFrom != nil {
		args = append(args, *dateFrom)
		conditions = append(conditions, fmt.Sprintf("t.created_at::date >= $%d", len(args)))
	}
	if dateTo != nil {
		args = append(args, *dateTo)
		conditions = append(conditions, fmt.Sprintf("t.created_at::date <= $%d", len(args)))
	}
	dateWhere := ""
	if len(conditions) > 0 {
		dateWhere = " AND " + strings.Join(conditions, " AND ")
	}

	query := `
		SELECT t.type, COUNT(DISTINCT t.id),
		       COALESCE(SUM(ti.price_total),0)::float8, COALESCE(SUM(ti.cogs),0)::float8
		FROM transaction_items ti
		JOIN transactions t ON t.id = ti.transaction_id
		WHERE t.type IN ('SELL', 'SELL_SUPPLIER') AND ti.cogs IS NOT NULL
	` + dateWhere + `
		GROUP BY t.type
		ORDER BY t.type
	`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("summarize sales profit: %w", err)
	}
	defer rows.Close()

	var breakdown []SalesTypeProfitBreakdown
	for rows.Next() {
		var b SalesTypeProfitBreakdown
		if err := rows.Scan(&b.Type, &b.TransactionCount, &b.TotalRevenue, &b.TotalCOGS); err != nil {
			return nil, fmt.Errorf("scan sales profit row: %w", err)
		}
		breakdown = append(breakdown, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("summarize sales profit: %w", err)
	}
	return breakdown, nil
}

func (r *reportRepository) ExpensesByCategory(ctx context.Context, dateFrom, dateTo *time.Time) ([]ExpenseCategoryBreakdown, error) {
	var conditions []string
	var args []any
	if dateFrom != nil {
		args = append(args, *dateFrom)
		conditions = append(conditions, fmt.Sprintf("e.expense_date >= $%d", len(args)))
	}
	if dateTo != nil {
		args = append(args, *dateTo)
		conditions = append(conditions, fmt.Sprintf("e.expense_date <= $%d", len(args)))
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := `
		SELECT ec.public_id::text, ec.name, COALESCE(SUM(e.amount),0)::float8
		FROM expenses e
		JOIN expense_categories ec ON ec.id = e.category_id
		` + where + `
		GROUP BY ec.id, ec.public_id, ec.name
		ORDER BY ec.name
	`

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("summarize expenses by category: %w", err)
	}
	defer rows.Close()

	var breakdown []ExpenseCategoryBreakdown
	for rows.Next() {
		var b ExpenseCategoryBreakdown
		if err := rows.Scan(&b.CategoryPublicID, &b.CategoryName, &b.TotalAmount); err != nil {
			return nil, fmt.Errorf("scan expense category breakdown row: %w", err)
		}
		breakdown = append(breakdown, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("summarize expenses by category: %w", err)
	}
	return breakdown, nil
}

func (r *reportRepository) PendingPurchaseOrders(ctx context.Context, limit int) ([]PendingPurchaseOrder, int, error) {
	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM purchase_orders WHERE status = 'BELUM_DITERIMA'`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count pending purchase orders: %w", err)
	}

	const listQuery = `
		SELECT po.public_id::text, po.po_code, s.name, po.total_amount::float8, po.created_at
		FROM purchase_orders po
		JOIN suppliers s ON s.id = po.supplier_id
		WHERE po.status = 'BELUM_DITERIMA'
		ORDER BY po.created_at DESC
		LIMIT $1
	`
	rows, err := r.db.Query(ctx, listQuery, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list pending purchase orders: %w", err)
	}
	defer rows.Close()

	var pending []PendingPurchaseOrder
	for rows.Next() {
		var p PendingPurchaseOrder
		if err := rows.Scan(&p.PublicID, &p.POCode, &p.SupplierName, &p.TotalAmount, &p.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan pending purchase order row: %w", err)
		}
		pending = append(pending, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list pending purchase orders: %w", err)
	}
	return pending, total, nil
}

func (r *reportRepository) CashSummary(ctx context.Context) (CashSummaryTotals, error) {
	const query = `
		SELECT
			(SELECT COALESCE(SUM(purchase_price),0) FROM stock_items WHERE status = 'AVAILABLE')::float8,
			(SELECT COALESCE(SUM(balance),0) FROM balance_accounts)::float8,
			(SELECT COALESCE(SUM(amount),0) FROM external_funds)::float8,
			(SELECT COALESCE(SUM(amount),0) FROM external_debts)::float8
	`

	var t CashSummaryTotals
	if err := r.db.QueryRow(ctx, query).Scan(&t.TotalGoldValue, &t.TotalBalance, &t.TotalExternalFunds, &t.TotalExternalDebts); err != nil {
		return CashSummaryTotals{}, fmt.Errorf("summarize cash: %w", err)
	}
	return t, nil
}
