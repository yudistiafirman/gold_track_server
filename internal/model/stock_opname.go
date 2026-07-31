package model

import "time"

type StockOpname struct {
	ID         int64
	PublicID   string
	OpnameCode string
	OpnameDate time.Time
	Status     string // IN_PROGRESS | COMPLETED | CANCELLED
	Notes      *string
	CreatedBy  int64
	CreatedAt  time.Time
}

type StockOpnameItem struct {
	ID             int64
	PublicID       string
	OpnameID       int64
	StockItemID    int64
	SystemStatus   string  // AVAILABLE | SOLD — snapshot of stock_items.status at scan/complete time
	PhysicalStatus *string // FOUND | NOT_FOUND
	Result         string  // MATCH | MISSING | UNEXPECTED
	Notes          *string
}
