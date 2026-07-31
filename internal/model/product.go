package model

import "time"

type Product struct {
	ID          int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID    string // UUID, the only identifier exposed to API clients
	Name        string
	SKU         string
	CategoryID  int64
	BrandID     int64
	WeightGram  float64
	Description *string
	IsActive    bool
	CreatedBy   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
