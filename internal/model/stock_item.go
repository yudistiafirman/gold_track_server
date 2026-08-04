package model

import "time"

type StockItem struct {
	ID             int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID       string // UUID, the only identifier exposed to API clients
	ProductID      int64
	Barcode        string
	SerialNumber   string
	Condition      string // GOOD | BAD
	PurchasePrice  float64
	PurchaseDate   time.Time
	ProductionYear *int   // optional — not every product/unit has a known production/mint year
	SupplierID     *int64 // always nil for now — not accepted via API yet
	POID           *int64 // always nil for now — no purchase-order resource yet
	Status         string // AVAILABLE | SOLD
	SoldAt         *time.Time
	Notes          *string
	CreatedBy      int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
