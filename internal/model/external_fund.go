package model

import "time"

type ExternalFund struct {
	ID          int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID    string // UUID, the only identifier exposed to API clients
	Description string
	Amount      float64
	CreatedAt   time.Time
}
