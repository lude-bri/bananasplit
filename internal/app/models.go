/* MODELS

Author: Luigi Piantavinha

This file defines the core domain types the whole application is built around,
plus the contract (interface) that any storage backend must satisfy.

Content:
  - Person:        someone who shares a wallet, with their funding settings.
  - Expense:       a single shared expense inside a wallet.
  - Period:        one pane of the wallet (a day, month or year).
  - Contribution:  money a person has put into the wallet.
  - Wallet:        a shared account (e.g. "House"), organised into periods, with
                   its people, expenses and contributions.
  - Funding:       one person's income settings submitted from the funding form.
  - WalletStore:   the storage contract used by the web layer. It hides the
                   concrete persistence mechanism (SQLite, JSON, ...) behind a
                   small set of operations.
  - WalletSummary: the pre-computed numbers shown on the wallet page (planned
                   contributions, expense totals and the monthly average).

These types are intentionally free of any HTTP or storage logic so they can be
shared by every layer of the app.

*/

package app

import "time"

/* Person - someone who shares a wallet.
*
* Fields:
*   - ID:                  unique identifier assigned by the store.
*   - Name:                display name (e.g. "Luigi").
*   - SalaryCents:         the person's monthly income in integer cents.
*   - ContributionPercent: the percentage of their income (1-100) they commit
*                          to the shared wallet each month.
 */

type Person struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	SalaryCents         int64  `json:"salary_cents"`
	ContributionPercent int64  `json:"contribution_percent"`
}

/* Expense - a single shared expense inside a wallet.
*
* Fields:
*   - ID:          unique identifier assigned by the store.
*   - Description: free-text label (e.g. "Supermarket").
*   - Category:    grouping label (e.g. "Food"); defaults to "General".
*   - PaidByID:    the ID of the Person who actually paid.
*   - AmountCents: the value in integer cents to avoid floating-point errors.
*   - Date:        when the expense happened.
*   - IsPaid:      whether the expense has already been settled.
 */

type Expense struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	PaidByID    int64     `json:"paid_by_id"`
	AmountCents int64     `json:"amount_cents"`
	Date        time.Time `json:"date"`
	IsPaid      bool      `json:"is_paid"`
}

/* Period granularity a wallet can be organised by. A wallet is split into panes
* of one of these units; every expense belongs to the pane its date falls in. */

const (
	PeriodDaily   = "daily"
	PeriodMonthly = "monthly"
	PeriodYearly  = "yearly"
)

/* Period - one pane of a wallet (a day, month or year, depending on the
* wallet's PeriodType).
*
* Fields:
*   - Key: the period key, formatted for the wallet's granularity:
*          daily "2006-01-02", monthly "2006-01", yearly "2006".
 */

type Period struct {
	Key string `json:"key"`
}

/* Contribution - money a person has put into the wallet.
*
* Fields:
*   - ID:          unique identifier assigned by the store.
*   - PersonID:    the ID of the Person who contributed.
*   - AmountCents: how much was contributed, in integer cents.
*   - Date:        when the contribution was made.
 */

type Contribution struct {
	ID          int64     `json:"id"`
	PersonID    int64     `json:"person_id"`
	AmountCents int64     `json:"amount_cents"`
	Date        time.Time `json:"date"`
}

/* Wallet - a shared account with its periods, people, expenses and contributions.
*
* Fields:
*   - ID:            unique identifier assigned by the store.
*   - Name:          display name (e.g. "House").
*   - PeriodType:    how the wallet is organised: PeriodDaily, PeriodMonthly or
*                    PeriodYearly. Chosen when the wallet is created.
*   - Periods:       the periods (panes) this wallet is tracking, newest first.
*   - People:        everyone sharing this wallet.
*   - Expenses:      every expense recorded in this wallet.
*   - Contributions: every contribution recorded in this wallet.
 */

type Wallet struct {
	ID            int64          `json:"id"`
	Name          string         `json:"name"`
	PeriodType    string         `json:"period_type"`
	Periods       []Period       `json:"periods"`
	People        []Person       `json:"people"`
	Expenses      []Expense      `json:"expenses"`
	Contributions []Contribution `json:"contributions"`
}

/* Funding - one person's income settings submitted from the funding form.
*
* This is the input the web layer hands to WalletStore.FundWallet; it is not a
* stored entity in itself, it updates the matching Person's funding fields.
*
* Fields:
*   - PersonID:            the ID of the Person these settings belong to.
*   - SalaryCents:         the person's monthly income in integer cents.
*   - ContributionPercent: the percentage of their income (1-100) committed to
*                          the wallet each month.
 */

type Funding struct {
	PersonID            int64
	SalaryCents         int64
	ContributionPercent int64
}

/* WalletStore - the storage contract for wallets, months, people, expenses and
* contributions.
*
* Any type that provides these methods can be used as the application's data
* store. The web layer depends only on this interface, never on a concrete
* implementation, which makes the storage backend swappable and easy to fake in
* tests (dependency inversion).
 */

type WalletStore interface {
	// Wallets
	AllWallets() ([]Wallet, error)
	GetWallet(id int64) (Wallet, error)
	AddWallet(name, periodType string) (Wallet, error)
	DeleteWallet(id int64) error

	// Periods
	AddPeriod(walletID int64, periodKey string) error
	DeletePeriod(walletID int64, periodKey string) error

	// People
	AddPerson(walletID int64, name string) error
	DeletePerson(walletID, personID int64) error
	FundWallet(walletID int64, funding []Funding) error

	// Expenses
	AddExpense(walletID int64, expense Expense) error
	MarkExpensePaid(walletID, expenseID int64) error
	DeleteExpense(walletID, expenseID int64) error
}

/* WalletSummary - the aggregated figures for a single wallet's selected month.
*
* This is a plain data holder produced by summarize() and consumed by the HTML
* template. All monetary fields are in integer cents.
*
* Fields:
*   - PlannedContributionCents: sum of every person's income × contribution %.
*   - TotalExpensesCents:       sum of the selected month's expenses.
*   - PaidExpensesCents:        sum of the selected month's paid expenses.
*   - OutstandingExpensesCents: TotalExpensesCents - PaidExpensesCents.
*   - AveragePeriodCents:       all expenses divided by the periods tracked.
*   - BalanceCents:             PlannedContributionCents - PaidExpensesCents.
*   - PeriodsTracked:           how many distinct periods the wallet has data for.
 */

type WalletSummary struct {
	PlannedContributionCents int64
	TotalExpensesCents       int64
	PaidExpensesCents        int64
	OutstandingExpensesCents int64
	AveragePeriodCents       int64
	BalanceCents             int64
	PeriodsTracked           int
	CategoryAverages         []CategoryAverage
}

/* CategoryAverage - the spending figures for one expense category across the
* whole wallet. All monetary fields are in integer cents.
*
* Fields:
*   - Name:         the category label (e.g. "Housing").
*   - TotalCents:   every expense in this category, summed across all periods.
*   - AverageCents: TotalCents divided by the number of periods tracked, i.e.
*                   the typical spend on this category per period.
 */

type CategoryAverage struct {
	Name         string
	TotalCents   int64
	AverageCents int64
}
