package app

import "time"

type Person struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	SalaryCents         int64  `json:"salary_cents"`
	ContributionPercent int64  `json:"contribution_percent"`
}

type Expense struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	PaidByID    int64     `json:"paid_by_id"`
	AmountCents int64     `json:"amount_cents"`
	Date        time.Time `json:"date"`
	IsPaid      bool      `json:"is_paid"`
}

type Wallet struct {
	ID            int64          `json:"id"`
	Name          string         `json:"name"`
	People        []Person       `json:"people"`
	Expenses      []Expense      `json:"expenses"`
	Contributions []Contribution `json:"contributions"`
}

type Contribution struct {
	ID          int64     `json:"id"`
	PersonID    int64     `json:"person_id"`
	AmountCents int64     `json:"amount_cents"`
	Date        time.Time `json:"date"`
}

type Funding struct {
	PersonID            int64
	SalaryCents         int64
	ContributionPercent int64
}

type WalletStore interface {
	AllWallets() ([]Wallet, error)
	GetWallet(id int64) (Wallet, error)
	AddWallet(name string) (Wallet, error)
	DeleteWallet(id int64) error
	AddPerson(walletID int64, name string) error
	DeletePerson(walletID, personID int64) error
	FundWallet(walletID int64, funding []Funding) error
	AddExpense(walletID int64, expense Expense) error
	DeleteExpense(walletID, expenseID int64) error
}

type WalletSummary struct {
	PlannedContributionCents int64
	TotalExpensesCents       int64
	PaidExpensesCents        int64
	OutstandingExpensesCents int64
	AverageMonthlyCents      int64
	MonthsTracked            int
	BalanceCents             int64
}
