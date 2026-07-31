package model

import "time"

type Transaction struct {
	ID              int64
	PublicID        string
	TransactionCode string
	Type            string // SELL | BUY | SELL_SUPPLIER (only SELL/SELL_SUPPLIER created via this API so far)
	CustomerID      *int64
	SupplierID      *int64
	TotalAmount     float64
	TotalWeight     float64
	PaymentMethod   string // CASH | TRANSFER | QRIS
	PaymentRef      *string
	GoldPriceID     *int64 // always nil for now — no gold-price feature yet
	Notes           *string
	InvoiceURL      *string // always nil for now — no invoicing feature yet
	Status          string  // COMPLETED | CANCELLED
	CreatedBy       int64
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

type TransactionItem struct {
	ID            int64
	PublicID      string
	TransactionID int64
	StockItemID   int64
	ProductName   string   // snapshot at sale time
	WeightGram    float64  // snapshot at sale time
	PricePerGram  float64  // derived: PriceTotal / WeightGram
	PriceTotal    float64  // manual input — the negotiated price
	COGS          *float64 // = stock_items.purchase_price at sale time
	CreatedAt     time.Time
}
