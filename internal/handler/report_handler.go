package handler

import (
	"net/http"
	"strconv"
	"time"

	"gold-track-be/internal/repository"
	"gold-track-be/internal/service"
	"gold-track-be/pkg/response"
)

type ReportHandler struct {
	reportService service.ReportService
}

func NewReportHandler(reportService service.ReportService) *ReportHandler {
	return &ReportHandler{reportService: reportService}
}

type transactionTypeBreakdownResponse struct {
	Type             string  `json:"type"`
	TransactionCount int     `json:"transaction_count"`
	TotalAmount      float64 `json:"total_amount"`
	TotalWeight      float64 `json:"total_weight"`
}

type transactionReportTotalsResponse struct {
	TransactionCount int     `json:"transaction_count"`
	TotalAmount      float64 `json:"total_amount"`
	TotalWeight      float64 `json:"total_weight"`
}

type transactionReportResponse struct {
	From      string                             `json:"from"`
	To        string                             `json:"to"`
	Breakdown []transactionTypeBreakdownResponse `json:"breakdown"`
	Total     transactionReportTotalsResponse    `json:"total"`
}

func toTransactionTypeBreakdownResponse(b repository.TransactionTypeBreakdown) transactionTypeBreakdownResponse {
	return transactionTypeBreakdownResponse{
		Type:             b.Type,
		TransactionCount: b.TransactionCount,
		TotalAmount:      b.TotalAmount,
		TotalWeight:      b.TotalWeight,
	}
}

func toTransactionReportTotalsResponse(t service.TransactionReportTotals) transactionReportTotalsResponse {
	return transactionReportTotalsResponse{
		TransactionCount: t.TransactionCount,
		TotalAmount:      t.TotalAmount,
		TotalWeight:      t.TotalWeight,
	}
}

func (h *ReportHandler) Transactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	result, err := h.reportService.TransactionReport(r.Context(), service.TransactionReportInput{
		DateFrom: q.Get("from"),
		DateTo:   q.Get("to"),
		Type:     q.Get("type"),
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	breakdown := make([]transactionTypeBreakdownResponse, 0, len(result.Breakdown))
	for _, b := range result.Breakdown {
		breakdown = append(breakdown, toTransactionTypeBreakdownResponse(b))
	}

	response.JSON(w, http.StatusOK, transactionReportResponse{
		From:      q.Get("from"),
		To:        q.Get("to"),
		Breakdown: breakdown,
		Total:     toTransactionReportTotalsResponse(result.Total),
	})
}

type stockReportProductRefResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	SKU  string `json:"sku"`
}

type stockReportItemResponse struct {
	Product        stockReportProductRefResponse `json:"product"`
	AvailableCount int                           `json:"available_count"`
	GoodCount      int                           `json:"good_count"`
	BadCount       int                           `json:"bad_count"`
	LowStock       bool                          `json:"low_stock"`
}

type stockReportResponse struct {
	Threshold int                       `json:"threshold"`
	Items     []stockReportItemResponse `json:"items"`
}

func toStockReportItemResponse(it service.StockReportItem) stockReportItemResponse {
	return stockReportItemResponse{
		Product: stockReportProductRefResponse{
			ID:   it.ProductPublicID,
			Name: it.ProductName,
			SKU:  it.ProductSKU,
		},
		AvailableCount: it.AvailableCount,
		GoodCount:      it.GoodCount,
		BadCount:       it.BadCount,
		LowStock:       it.LowStock,
	}
}

func (h *ReportHandler) Stock(w http.ResponseWriter, r *http.Request) {
	threshold, _ := strconv.Atoi(r.URL.Query().Get("threshold"))

	result, err := h.reportService.StockReport(r.Context(), threshold)
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]stockReportItemResponse, 0, len(result.Items))
	for _, it := range result.Items {
		items = append(items, toStockReportItemResponse(it))
	}

	response.JSON(w, http.StatusOK, stockReportResponse{
		Threshold: result.Threshold,
		Items:     items,
	})
}

type salesTypeProfitResponse struct {
	Type             string  `json:"type"`
	TransactionCount int     `json:"transaction_count"`
	TotalRevenue     float64 `json:"total_revenue"`
	TotalCOGS        float64 `json:"total_cogs"`
	GrossProfit      float64 `json:"gross_profit"`
}

// expenseCategoryBreakdownResponse.Category reuses productRefResponse
// ({id, name}) — same generic ref shape expenseResponse.Category already
// uses, no need for a third identical type.
type expenseCategoryBreakdownResponse struct {
	Category    productRefResponse `json:"category"`
	TotalAmount float64            `json:"total_amount"`
}

// financeSummaryResponse holds the finance figures without From/To — used
// standalone (embedded flat into financeReportResponse) and nested (under
// Dashboard's "finance" key), so From/To isn't duplicated in the dashboard.
type financeSummaryResponse struct {
	SalesBreakdown     []salesTypeProfitResponse          `json:"sales_breakdown"`
	ExpenseBreakdown   []expenseCategoryBreakdownResponse `json:"expense_breakdown"`
	TotalRevenue       float64                            `json:"total_revenue"`
	TotalCOGS          float64                            `json:"total_cogs"`
	GrossProfit        float64                            `json:"gross_profit"`
	GrossMarginPercent float64                            `json:"gross_margin_percent"`
	TotalExpenses      float64                            `json:"total_expenses"`
	NetProfit          float64                            `json:"net_profit"`
}

func toFinanceSummaryResponse(result service.FinanceReportSummary) financeSummaryResponse {
	salesBreakdown := make([]salesTypeProfitResponse, 0, len(result.SalesBreakdown))
	for _, s := range result.SalesBreakdown {
		salesBreakdown = append(salesBreakdown, salesTypeProfitResponse{
			Type:             s.Type,
			TransactionCount: s.TransactionCount,
			TotalRevenue:     s.TotalRevenue,
			TotalCOGS:        s.TotalCOGS,
			GrossProfit:      s.GrossProfit,
		})
	}

	expenseBreakdown := make([]expenseCategoryBreakdownResponse, 0, len(result.ExpenseBreakdown))
	for _, e := range result.ExpenseBreakdown {
		expenseBreakdown = append(expenseBreakdown, expenseCategoryBreakdownResponse{
			Category:    productRefResponse{ID: e.CategoryPublicID, Name: e.CategoryName},
			TotalAmount: e.TotalAmount,
		})
	}

	return financeSummaryResponse{
		SalesBreakdown:     salesBreakdown,
		ExpenseBreakdown:   expenseBreakdown,
		TotalRevenue:       result.TotalRevenue,
		TotalCOGS:          result.TotalCOGS,
		GrossProfit:        result.GrossProfit,
		GrossMarginPercent: result.GrossMarginPercent,
		TotalExpenses:      result.TotalExpenses,
		NetProfit:          result.NetProfit,
	}
}

// financeReportResponse embeds financeSummaryResponse so the standalone
// /api/reports/finance JSON shape stays flat (unchanged from before this
// was extracted into a reusable helper).
type financeReportResponse struct {
	From string `json:"from"`
	To   string `json:"to"`
	financeSummaryResponse
}

func (h *ReportHandler) Finance(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	result, err := h.reportService.FinanceReport(r.Context(), service.FinanceReportInput{
		DateFrom: q.Get("from"),
		DateTo:   q.Get("to"),
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	response.JSON(w, http.StatusOK, financeReportResponse{
		From:                   q.Get("from"),
		To:                     q.Get("to"),
		financeSummaryResponse: toFinanceSummaryResponse(result),
	})
}

// cashSummaryResponse is the "where the shop's money lives" snapshot.
// total_gold_value is a live aggregate over stock_items, not a stored field.
type cashSummaryResponse struct {
	TotalGoldValue     float64 `json:"total_gold_value"`
	TotalBalance       float64 `json:"total_balance"`
	TotalExternalFunds float64 `json:"total_external_funds"`
	TotalExternalDebts float64 `json:"total_external_debts"`
}

func toCashSummaryResponse(c service.CashSummary) cashSummaryResponse {
	return cashSummaryResponse{
		TotalGoldValue:     c.TotalGoldValue,
		TotalBalance:       c.TotalBalance,
		TotalExternalFunds: c.TotalExternalFunds,
		TotalExternalDebts: c.TotalExternalDebts,
	}
}

type pendingPurchaseOrderResponse struct {
	ID           string    `json:"id"`
	POCode       string    `json:"po_code"`
	SupplierName string    `json:"supplier_name"`
	TotalAmount  float64   `json:"total_amount"`
	CreatedAt    time.Time `json:"created_at"`
}

// dashboardResponse composes the other three reports plus pending POs
// into a single payload — the "what should the dashboard show" answer.
type dashboardResponse struct {
	From                       string                             `json:"from"`
	To                         string                             `json:"to"`
	Finance                    financeSummaryResponse             `json:"finance"`
	TransactionBreakdown       []transactionTypeBreakdownResponse `json:"transaction_breakdown"`
	TransactionTotal           transactionReportTotalsResponse    `json:"transaction_total"`
	LowStockThreshold          int                                `json:"low_stock_threshold"`
	LowStockItems              []stockReportItemResponse          `json:"low_stock_items"`
	PendingPurchaseOrders      []pendingPurchaseOrderResponse     `json:"pending_purchase_orders"`
	PendingPurchaseOrdersTotal int                                `json:"pending_purchase_orders_total"`
	CashSummary                cashSummaryResponse                `json:"cash_summary"`
}

// Dashboard composes TransactionReport/StockReport/FinanceReport plus a
// pending-purchase-orders list into one payload, so the FE doesn't need to
// fire 4 requests and merge them client-side. Defaults to the current
// month when no ?from=/?to= given.
func (h *ReportHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	threshold, _ := strconv.Atoi(q.Get("threshold"))
	pendingLimit, _ := strconv.Atoi(q.Get("pending_limit"))

	result, err := h.reportService.Dashboard(r.Context(), service.DashboardInput{
		DateFrom:     q.Get("from"),
		DateTo:       q.Get("to"),
		Threshold:    threshold,
		PendingLimit: pendingLimit,
	})
	if err != nil {
		response.Error(w, err)
		return
	}

	breakdown := make([]transactionTypeBreakdownResponse, 0, len(result.TransactionBreakdown))
	for _, b := range result.TransactionBreakdown {
		breakdown = append(breakdown, toTransactionTypeBreakdownResponse(b))
	}

	lowStockItems := make([]stockReportItemResponse, 0, len(result.LowStockItems))
	for _, it := range result.LowStockItems {
		lowStockItems = append(lowStockItems, toStockReportItemResponse(it))
	}

	pending := make([]pendingPurchaseOrderResponse, 0, len(result.PendingPurchaseOrders))
	for _, p := range result.PendingPurchaseOrders {
		pending = append(pending, pendingPurchaseOrderResponse{
			ID:           p.PublicID,
			POCode:       p.POCode,
			SupplierName: p.SupplierName,
			TotalAmount:  p.TotalAmount,
			CreatedAt:    p.CreatedAt,
		})
	}

	response.JSON(w, http.StatusOK, dashboardResponse{
		From:                       result.From,
		To:                         result.To,
		Finance:                    toFinanceSummaryResponse(result.Finance),
		TransactionBreakdown:       breakdown,
		TransactionTotal:           toTransactionReportTotalsResponse(result.TransactionTotal),
		LowStockThreshold:          result.LowStockThreshold,
		LowStockItems:              lowStockItems,
		PendingPurchaseOrders:      pending,
		PendingPurchaseOrdersTotal: result.PendingPurchaseOrdersTotal,
		CashSummary:                toCashSummaryResponse(result.Cash),
	})
}
