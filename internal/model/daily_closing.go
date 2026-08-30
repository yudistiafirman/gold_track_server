package model

import "time"

// DailyClosing is an immutable snapshot of the shop's cash position ("Saldo")
// recorded when an admin manually closes a day's books — the baseline the
// next reconciliation check compares against.
type DailyClosing struct {
	ID             int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID       string // UUID, the only identifier exposed to API clients
	ClosingDate    time.Time
	TotalBalance   float64
	TotalGoldValue float64
	TotalSaldo     float64
	CreatedBy      *int64
	CreatedAt      time.Time
}
