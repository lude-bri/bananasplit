package app

import "time"

type Expense struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	PaidBy      string    `json:"paid_by"`
	AmountCents int64     `json:"amount_cents"`
	Date        time.Time `json:"date"`
}

type ExpenseStore interface {
	All() ([]Expense, error)
	Add(expense Expense) (Expense, error)
	Delete(id int64) error
}

type MonthlySummary struct {
	Month              string
	TotalCents         int64
	LuigiPaidCents     int64
	PartnerPaidCents   int64
	PartnerShareCents  int64
	LuigiShareCents    int64
	SettlementCents    int64
	SettlementSentence string
}
