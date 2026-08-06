package service

import (
	"context"
	"strings"
	"time"

	"gold-track-be/internal/repository"
	"gold-track-be/pkg/apperror"
)

const reportDateLayout = "2006-01-02"

const defaultStockLowThreshold = 5

var allowedTransactionReportTypes = map[string]struct{}{
	"SELL":          {},
	"BUY":           {},
	"SELL_SUPPLIER": {},
}

type TransactionReportInput struct {
	DateFrom string
	DateTo   string
	Type     string
}

type TransactionReportTotals struct {
	TransactionCount int
	TotalAmount      float64
	TotalWeight      float64
}

type TransactionReportSummary struct {
	Breakdown []repository.TransactionTypeBreakdown
	Total     TransactionReportTotals
}

type StockReportItem struct {
	ProductPublicID string
	ProductName     string
	ProductSKU      string
	AvailableCount  int
	GoodCount       int
	BadCount        int
	LowStock        bool
}

type StockReportSummary struct {
	Threshold int
	Items     []StockReportItem
}

type FinanceReportInput struct {
	DateFrom string
	DateTo   string
}

// SalesTypeProfitSummary is one row of the finance report's sales side —
// GrossProfit is derived here (TotalRevenue - TotalCOGS), not stored.
type SalesTypeProfitSummary struct {
	Type             string
	TransactionCount int
	TotalRevenue     float64
	TotalCOGS        float64
	GrossProfit      float64
}

// FinanceExpenseBreakdown — named distinctly from ExpenseCategorySummary
// (the expense-category CRUD DTO in expense_category_service.go) to avoid
// a name collision; this is just the per-category slice of this report.
type FinanceExpenseBreakdown struct {
	CategoryPublicID string
	CategoryName     string
	TotalAmount      float64
}

type FinanceReportSummary struct {
	SalesBreakdown     []SalesTypeProfitSummary
	ExpenseBreakdown   []FinanceExpenseBreakdown
	TotalRevenue       float64
	TotalCOGS          float64
	GrossProfit        float64
	GrossMarginPercent float64 // GrossProfit / TotalRevenue * 100; 0 when TotalRevenue is 0
	TotalExpenses      float64
	NetProfit          float64
}

const defaultDashboardPendingPOLimit = 5

// PendingPurchaseOrderSummary is one row of the dashboard's "awaiting
// receipt" list.
type PendingPurchaseOrderSummary struct {
	PublicID     string
	POCode       string
	SupplierName string
	TotalAmount  float64
	CreatedAt    time.Time
}

// CashSummary is the "where the shop's money lives" snapshot — service-layer
// copy of repository.CashSummaryTotals, kept distinct per this file's usual
// repository.X → service.X DTO convention.
type CashSummary struct {
	TotalGoldValue     float64
	TotalBalance       float64
	TotalExternalFunds float64
	TotalExternalDebts float64
}

type DashboardInput struct {
	DateFrom     string // "" + DateTo == "" together → defaults to the current month
	DateTo       string
	Threshold    int // stock low-stock threshold, same default (5) as StockReport
	PendingLimit int // default 5
}

// DashboardSummary composes the other three reports plus pending POs —
// FinanceReport/TransactionReport/StockReport are called internally
// (not reimplemented) so the dashboard and the standalone report
// endpoints can never drift apart.
type DashboardSummary struct {
	From                       string
	To                         string
	Finance                    FinanceReportSummary
	TransactionBreakdown       []repository.TransactionTypeBreakdown
	TransactionTotal           TransactionReportTotals
	LowStockThreshold          int
	LowStockItems              []StockReportItem // StockReport's Items filtered to LowStock == true
	PendingPurchaseOrders      []PendingPurchaseOrderSummary
	PendingPurchaseOrdersTotal int
	Cash                       CashSummary
}

type ReportService interface {
	TransactionReport(ctx context.Context, input TransactionReportInput) (TransactionReportSummary, error)
	StockReport(ctx context.Context, threshold int) (StockReportSummary, error)
	FinanceReport(ctx context.Context, input FinanceReportInput) (FinanceReportSummary, error)
	Dashboard(ctx context.Context, input DashboardInput) (DashboardSummary, error)
	CashSummary(ctx context.Context) (CashSummary, error)
}

type reportService struct {
	reportRepo repository.ReportRepository
}

func NewReportService(reportRepo repository.ReportRepository) ReportService {
	return &reportService{reportRepo: reportRepo}
}

func (s *reportService) TransactionReport(ctx context.Context, input TransactionReportInput) (TransactionReportSummary, error) {
	filter := repository.TransactionReportFilter{}

	if input.Type != "" {
		if _, ok := allowedTransactionReportTypes[input.Type]; !ok {
			return TransactionReportSummary{}, apperror.BadRequest("type harus SELL, BUY, atau SELL_SUPPLIER", nil)
		}
		filter.Type = &input.Type
	}

	dateFrom, dateTo, err := parseReportDateRange(input.DateFrom, input.DateTo)
	if err != nil {
		return TransactionReportSummary{}, err
	}
	filter.DateFrom = dateFrom
	filter.DateTo = dateTo

	breakdown, err := s.reportRepo.TransactionSummary(ctx, filter)
	if err != nil {
		return TransactionReportSummary{}, apperror.Internal("failed to summarize transactions", err)
	}

	var total TransactionReportTotals
	for _, b := range breakdown {
		total.TransactionCount += b.TransactionCount
		total.TotalAmount += b.TotalAmount
		total.TotalWeight += b.TotalWeight
	}

	return TransactionReportSummary{Breakdown: breakdown, Total: total}, nil
}

func (s *reportService) StockReport(ctx context.Context, threshold int) (StockReportSummary, error) {
	if threshold <= 0 {
		threshold = defaultStockLowThreshold
	}

	rows, err := s.reportRepo.StockSummary(ctx)
	if err != nil {
		return StockReportSummary{}, apperror.Internal("failed to summarize stock", err)
	}

	items := make([]StockReportItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, StockReportItem{
			ProductPublicID: row.ProductPublicID,
			ProductName:     row.ProductName,
			ProductSKU:      row.ProductSKU,
			AvailableCount:  row.AvailableCount,
			GoodCount:       row.GoodCount,
			BadCount:        row.BadCount,
			LowStock:        row.AvailableCount <= threshold,
		})
	}

	return StockReportSummary{Threshold: threshold, Items: items}, nil
}

func (s *reportService) FinanceReport(ctx context.Context, input FinanceReportInput) (FinanceReportSummary, error) {
	dateFrom, dateTo, err := parseReportDateRange(input.DateFrom, input.DateTo)
	if err != nil {
		return FinanceReportSummary{}, err
	}

	salesRows, err := s.reportRepo.SalesProfitByType(ctx, dateFrom, dateTo)
	if err != nil {
		return FinanceReportSummary{}, apperror.Internal("failed to summarize sales profit", err)
	}

	salesBreakdown := make([]SalesTypeProfitSummary, 0, len(salesRows))
	var totalRevenue, totalCOGS float64
	for _, row := range salesRows {
		grossProfit := row.TotalRevenue - row.TotalCOGS
		salesBreakdown = append(salesBreakdown, SalesTypeProfitSummary{
			Type:             row.Type,
			TransactionCount: row.TransactionCount,
			TotalRevenue:     row.TotalRevenue,
			TotalCOGS:        row.TotalCOGS,
			GrossProfit:      grossProfit,
		})
		totalRevenue += row.TotalRevenue
		totalCOGS += row.TotalCOGS
	}
	grossProfit := totalRevenue - totalCOGS

	expenseRows, err := s.reportRepo.ExpensesByCategory(ctx, dateFrom, dateTo)
	if err != nil {
		return FinanceReportSummary{}, apperror.Internal("failed to summarize expenses by category", err)
	}

	expenseBreakdown := make([]FinanceExpenseBreakdown, 0, len(expenseRows))
	var totalExpenses float64
	for _, row := range expenseRows {
		expenseBreakdown = append(expenseBreakdown, FinanceExpenseBreakdown{
			CategoryPublicID: row.CategoryPublicID,
			CategoryName:     row.CategoryName,
			TotalAmount:      row.TotalAmount,
		})
		totalExpenses += row.TotalAmount
	}

	var marginPercent float64
	if totalRevenue > 0 {
		marginPercent = grossProfit / totalRevenue * 100
	}

	return FinanceReportSummary{
		SalesBreakdown:     salesBreakdown,
		ExpenseBreakdown:   expenseBreakdown,
		TotalRevenue:       totalRevenue,
		TotalCOGS:          totalCOGS,
		GrossProfit:        grossProfit,
		GrossMarginPercent: marginPercent,
		TotalExpenses:      totalExpenses,
		NetProfit:          grossProfit - totalExpenses,
	}, nil
}

func (s *reportService) Dashboard(ctx context.Context, input DashboardInput) (DashboardSummary, error) {
	dateFrom, dateTo := input.DateFrom, input.DateTo
	if strings.TrimSpace(dateFrom) == "" && strings.TrimSpace(dateTo) == "" {
		// UTC, not server-local time: transactions.created_at is filtered
		// via Postgres' created_at::date, and the DB session runs in UTC —
		// using local time here could silently exclude "today"'s rows
		// whenever the server's local date has already rolled over but the
		// UTC date (what the DB actually compares against) hasn't yet.
		now := time.Now().UTC()
		firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		dateFrom = firstOfMonth.Format(reportDateLayout)
		dateTo = now.Format(reportDateLayout)
	}

	finance, err := s.FinanceReport(ctx, FinanceReportInput{DateFrom: dateFrom, DateTo: dateTo})
	if err != nil {
		return DashboardSummary{}, err
	}

	txReport, err := s.TransactionReport(ctx, TransactionReportInput{DateFrom: dateFrom, DateTo: dateTo})
	if err != nil {
		return DashboardSummary{}, err
	}

	stockReport, err := s.StockReport(ctx, input.Threshold)
	if err != nil {
		return DashboardSummary{}, err
	}
	lowStockItems := make([]StockReportItem, 0)
	for _, item := range stockReport.Items {
		if item.LowStock {
			lowStockItems = append(lowStockItems, item)
		}
	}

	pendingLimit := input.PendingLimit
	if pendingLimit <= 0 {
		pendingLimit = defaultDashboardPendingPOLimit
	}
	pendingRows, pendingTotal, err := s.reportRepo.PendingPurchaseOrders(ctx, pendingLimit)
	if err != nil {
		return DashboardSummary{}, apperror.Internal("failed to fetch pending purchase orders", err)
	}
	pendingItems := make([]PendingPurchaseOrderSummary, 0, len(pendingRows))
	for _, p := range pendingRows {
		pendingItems = append(pendingItems, PendingPurchaseOrderSummary{
			PublicID:     p.PublicID,
			POCode:       p.POCode,
			SupplierName: p.SupplierName,
			TotalAmount:  p.TotalAmount,
			CreatedAt:    p.CreatedAt,
		})
	}

	cash, err := s.CashSummary(ctx)
	if err != nil {
		return DashboardSummary{}, err
	}

	return DashboardSummary{
		From:                       dateFrom,
		To:                         dateTo,
		Finance:                    finance,
		TransactionBreakdown:       txReport.Breakdown,
		TransactionTotal:           txReport.Total,
		LowStockThreshold:          stockReport.Threshold,
		LowStockItems:              lowStockItems,
		PendingPurchaseOrders:      pendingItems,
		PendingPurchaseOrdersTotal: pendingTotal,
		Cash:                       cash,
	}, nil
}

func (s *reportService) CashSummary(ctx context.Context) (CashSummary, error) {
	totals, err := s.reportRepo.CashSummary(ctx)
	if err != nil {
		return CashSummary{}, apperror.Internal("failed to summarize cash", err)
	}

	return CashSummary{
		TotalGoldValue:     totals.TotalGoldValue,
		TotalBalance:       totals.TotalBalance,
		TotalExternalFunds: totals.TotalExternalFunds,
		TotalExternalDebts: totals.TotalExternalDebts,
	}, nil
}

// parseReportDateRange parses from/to independently — each is optional,
// "" means unbounded on that side.
func parseReportDateRange(from, to string) (*time.Time, *time.Time, error) {
	var dateFrom, dateTo *time.Time

	if strings.TrimSpace(from) != "" {
		parsed, err := time.Parse(reportDateLayout, strings.TrimSpace(from))
		if err != nil {
			return nil, nil, apperror.BadRequest("from harus format YYYY-MM-DD", nil)
		}
		dateFrom = &parsed
	}
	if strings.TrimSpace(to) != "" {
		parsed, err := time.Parse(reportDateLayout, strings.TrimSpace(to))
		if err != nil {
			return nil, nil, apperror.BadRequest("to harus format YYYY-MM-DD", nil)
		}
		dateTo = &parsed
	}

	return dateFrom, dateTo, nil
}
