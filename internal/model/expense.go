package model

import "time"

type Expense struct {
	ID          int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID    string // UUID, the only identifier exposed to API clients
	CategoryID  int64
	Amount      float64
	Description *string
	ExpenseDate time.Time
	CreatedBy   int64
	CreatedAt   time.Time
}
