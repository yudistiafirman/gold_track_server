package model

import "time"

type BalanceAccount struct {
	ID        int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID  string // UUID, the only identifier exposed to API clients
	Name      string
	Balance   float64
	CreatedAt time.Time
}
