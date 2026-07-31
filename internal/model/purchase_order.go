package model

import "time"

type PurchaseOrder struct {
	ID          int64
	PublicID    string
	POCode      string
	SupplierID  int64
	TotalAmount float64
	Status      string // BELUM_DITERIMA | DITERIMA | DIBATALKAN
	Notes       *string
	CreatedBy   int64
	CreatedAt   time.Time
	ReceivedAt  *time.Time
}

type PurchaseOrderItem struct {
	ID            int64
	PublicID      string
	POID          int64
	ProductID     int64
	Quantity      int
	PurchasePrice float64
}
