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

type ReportService interface {
	TransactionReport(ctx context.Context, input TransactionReportInput) (TransactionReportSummary, error)
	StockReport(ctx context.Context, threshold int) (StockReportSummary, error)
	FinanceReport(ctx context.Context, input FinanceReportInput) (FinanceReportSummary, error)
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
