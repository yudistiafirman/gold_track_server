package model

import "time"

type ExpenseCategory struct {
	ID        int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID  string // UUID, the only identifier exposed to API clients
	Name      string
	CreatedAt time.Time
}
