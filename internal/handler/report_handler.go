package handler

import (
	"net/http"
	"strconv"

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
		breakdown = append(breakdown, transactionTypeBreakdownResponse{
			Type:             b.Type,
			TransactionCount: b.TransactionCount,
			TotalAmount:      b.TotalAmount,
			TotalWeight:      b.TotalWeight,
		})
	}

	response.JSON(w, http.StatusOK, transactionReportResponse{
		From:      q.Get("from"),
		To:        q.Get("to"),
		Breakdown: breakdown,
		Total: transactionReportTotalsResponse{
			TransactionCount: result.Total.TransactionCount,
			TotalAmount:      result.Total.TotalAmount,
			TotalWeight:      result.Total.TotalWeight,
		},
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

func (h *ReportHandler) Stock(w http.ResponseWriter, r *http.Request) {
	threshold, _ := strconv.Atoi(r.URL.Query().Get("threshold"))

	result, err := h.reportService.StockReport(r.Context(), threshold)
	if err != nil {
		response.Error(w, err)
		return
	}

	items := make([]stockReportItemResponse, 0, len(result.Items))
	for _, it := range result.Items {
		items = append(items, stockReportItemResponse{
			Product: stockReportProductRefResponse{
				ID:   it.ProductPublicID,
				Name: it.ProductName,
				SKU:  it.ProductSKU,
			},
			AvailableCount: it.AvailableCount,
			GoodCount:      it.GoodCount,
			BadCount:       it.BadCount,
			LowStock:       it.LowStock,
		})
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

type financeReportResponse struct {
	From               string                             `json:"from"`
	To                 string                             `json:"to"`
	SalesBreakdown     []salesTypeProfitResponse          `json:"sales_breakdown"`
	ExpenseBreakdown   []expenseCategoryBreakdownResponse `json:"expense_breakdown"`
	TotalRevenue       float64                            `json:"total_revenue"`
	TotalCOGS          float64                            `json:"total_cogs"`
	GrossProfit        float64                            `json:"gross_profit"`
	GrossMarginPercent float64                            `json:"gross_margin_percent"`
	TotalExpenses      float64                            `json:"total_expenses"`
	NetProfit          float64                            `json:"net_profit"`
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

	response.JSON(w, http.StatusOK, financeReportResponse{
		From:               q.Get("from"),
		To:                 q.Get("to"),
		SalesBreakdown:     salesBreakdown,
		ExpenseBreakdown:   expenseBreakdown,
		TotalRevenue:       result.TotalRevenue,
		TotalCOGS:          result.TotalCOGS,
		GrossProfit:        result.GrossProfit,
		GrossMarginPercent: result.GrossMarginPercent,
		TotalExpenses:      result.TotalExpenses,
		NetProfit:          result.NetProfit,
	})
}
