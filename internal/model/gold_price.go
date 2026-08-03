package model

import "time"

// GoldPrice is a snapshot of the reference gold price. Rows are append-only:
// is_active=true marks the current snapshot, and syncing a new price flips
// the old row to is_active=false rather than updating it in place — see
// GoldPriceRepository.ReplaceActive.
type GoldPrice struct {
	ID             int64
	PublicID       string
	PriceBuy       float64
	PriceSell      float64
	PriceReference *float64
	Spread         *float64
	EffectiveDate  time.Time
	EffectiveFrom  time.Time
	EffectiveUntil *time.Time
	IsActive       bool
	Source         *string
	Notes          *string
	CreatedBy      *int64 // nil when the row was created by the sync job, not a human
	CreatedAt      time.Time
}
