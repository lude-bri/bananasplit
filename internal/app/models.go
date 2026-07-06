/* MODELS

Author: Luigi Piantavinha

This file defines the core domain types that the whole application is built
around, plus the contract (interface) that any storage backend must satisfy.

Content:
  - Expense:        a single shared expense (who paid, how much, when).
  - ExpenseStore:   the storage contract used by the web layer. It hides the
                    concrete persistence mechanism (JSON file, database, ...)
                    behind three simple operations.
  - MonthlySummary: the pre-computed numbers shown on the monthly dashboard
                    (totals, per-person shares and the settlement).

These types are intentionally free of any HTTP or storage logic so they can be
shared by every layer of the app.

*/

package app

import "time"

/* Expense - represents a single shared expense.
*
* Each field is tagged with `json:"..."` so the type can be marshalled to and
* from the JSON file used for persistence.
*
* Fields:
*   - ID:          unique identifier assigned by the store.
*   - Description: free-text label (e.g. "Supermercado").
*   - Category:    grouping label (e.g. "Comida"); defaults to "Geral".
*   - PaidBy:      who actually paid ("Luigi" or "Parceira").
*   - AmountCents: the value in integer cents to avoid floating-point errors.
*   - Date:        when the expense happened.
 */

type Expense struct {
	ID          int64     `json:"id"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	PaidBy      string    `json:"paid_by"`
	AmountCents int64     `json:"amount_cents"`
	Date        time.Time `json:"date"`
}

/* ExpenseStore - the storage contract for expenses.
*
* Any type that provides these three methods can be used as the application's
* data store. The web layer depends only on this interface, never on a concrete
* implementation, which makes the storage backend swappable and easy to fake in
* tests (dependency inversion).
*
* Methods:
*   - All():        returns every stored expense, or an error.
*   - Add(expense): persists a new expense and returns it (with its assigned
*                   ID), or an error.
*   - Delete(id):   removes the expense with the given ID, or returns an error.
 */

type ExpenseStore interface {
	All() ([]Expense, error)
	Add(expense Expense) (Expense, error)
	Delete(id int64) error
}

/* MonthlySummary - the aggregated figures for a single month.
*
* This is a plain data holder produced by summarize() and consumed by the HTML
* template. All monetary fields are in integer cents.
*
* Fields:
*   - Month:              the month being summarised, as "2006-01".
*   - TotalCents:         sum of every expense in the month.
*   - LuigiPaidCents:     total actually paid by Luigi.
*   - PartnerPaidCents:   total actually paid by the partner.
*   - PartnerShareCents:  the partner's fair share (half of the total).
*   - LuigiShareCents:    Luigi's fair share (half of the total).
*   - SettlementCents:    how much must change hands to make things even.
*   - SettlementSentence: human-readable explanation of who owes whom.
 */

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
