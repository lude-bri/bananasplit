/* SQLITE STORE

Author: Luigi Piantavinha

This file provides the storage layer of BananaSplit: a concrete implementation
of the app.WalletStore interface that persists wallets, people and expenses to a
SQLite database on disk.

It uses the pure-Go driver modernc.org/sqlite, so it needs no CGO/gcc toolchain.

Content:
  - SQLiteStore:  wraps the *sql.DB handle.
  - Constructor:  NewSQLiteStore (opens the DB and creates the schema).
  - Wallets:      AllWallets, GetWallet, AddWallet, DeleteWallet.
  - People:       AddPerson, DeletePerson.
  - Expenses:     AddExpense, DeleteExpense.

Foreign keys are enabled with ON DELETE CASCADE so deleting a wallet also
removes its people and expenses automatically.

*/

package store

import (
	"database/sql"
	"fmt"
	"time"

	"bananasplit/internal/app"

	_ "modernc.org/sqlite"
)

/* *****************************************************************************
*
* Struct
*
*******************************************************************************/

type SQLiteStore struct {
	db *sql.DB
}

/* *****************************************************************************
*
* Constructor
*
*******************************************************************************/

/* NewSQLiteStore - opens (or creates) the SQLite database and its schema.
*
* It opens the database file at the given path, enables foreign-key enforcement,
* and creates the wallets/people/expenses tables if they do not exist yet.
*
* Parameters:
*   - path: the file system path of the SQLite database file.
*
* Returns:
*   - A pointer to the ready-to-use SQLiteStore, or an error on failure.
 */

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return nil, err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS wallets (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		name        TEXT NOT NULL,
		period_type TEXT NOT NULL DEFAULT 'monthly'
	);

	-- Physically named budget_months for backwards compatibility, but the
	-- "month" column stores a period key of any granularity (day/month/year).
	CREATE TABLE IF NOT EXISTS budget_months (
		wallet_id INTEGER NOT NULL,
		month     TEXT NOT NULL,
		PRIMARY KEY (wallet_id, month),
		FOREIGN KEY (wallet_id) REFERENCES wallets(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS people (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		wallet_id INTEGER NOT NULL,
		name      TEXT NOT NULL,
		salary_cents INTEGER NOT NULL DEFAULT 0,
		contribution_percent INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (wallet_id) REFERENCES wallets(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS expenses (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		wallet_id    INTEGER NOT NULL,
		description  TEXT NOT NULL,
		category     TEXT NOT NULL,
		paid_by_id   INTEGER NOT NULL,
		amount_cents INTEGER NOT NULL,
		date         TEXT NOT NULL,
		is_paid      INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY (wallet_id) REFERENCES wallets(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS contributions (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		wallet_id    INTEGER NOT NULL,
		person_id    INTEGER NOT NULL,
		amount_cents INTEGER NOT NULL,
		date         TEXT NOT NULL,
		FOREIGN KEY (wallet_id) REFERENCES wallets(id) ON DELETE CASCADE,
		FOREIGN KEY (person_id) REFERENCES people(id) ON DELETE CASCADE
	);`

	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	// Keep databases created by previous versions compatible with the new
	// funding fields. SQLite has no ADD COLUMN IF NOT EXISTS syntax.
	if err := addColumnIfMissing(db, "wallets", "period_type", "TEXT NOT NULL DEFAULT 'monthly'"); err != nil {
		return nil, err
	}
	if err := addColumnIfMissing(db, "people", "salary_cents", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	if err := addColumnIfMissing(db, "people", "contribution_percent", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}
	if err := addColumnIfMissing(db, "expenses", "is_paid", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

/* Close - closes the underlying database handle. */
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

/* *****************************************************************************
*
* Wallets
*
*******************************************************************************/

/* AllWallets - returns every wallet with its people and expenses loaded. */
func (s *SQLiteStore) AllWallets() ([]app.Wallet, error) {
	rows, err := s.db.Query(`SELECT id, name, period_type FROM wallets ORDER BY name;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wallets []app.Wallet
	for rows.Next() {
		var w app.Wallet
		if err := rows.Scan(&w.ID, &w.Name, &w.PeriodType); err != nil {
			return nil, err
		}
		wallets = append(wallets, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range wallets {
		periods, err := s.periodsOf(wallets[i].ID)
		if err != nil {
			return nil, err
		}
		wallets[i].Periods = periods
		people, err := s.peopleOf(wallets[i].ID)
		if err != nil {
			return nil, err
		}
		expenses, err := s.expensesOf(wallets[i].ID)
		if err != nil {
			return nil, err
		}
		wallets[i].People = people
		wallets[i].Expenses = expenses
		contributions, err := s.contributionsOf(wallets[i].ID)
		if err != nil {
			return nil, err
		}
		wallets[i].Contributions = contributions
	}

	return wallets, nil
}

/* GetWallet - returns a single wallet (with people and expenses) by ID. */
func (s *SQLiteStore) GetWallet(id int64) (app.Wallet, error) {
	var w app.Wallet
	err := s.db.QueryRow(`SELECT id, name, period_type FROM wallets WHERE id = ?;`, id).
		Scan(&w.ID, &w.Name, &w.PeriodType)
	if err == sql.ErrNoRows {
		return app.Wallet{}, fmt.Errorf("wallet %d not found", id)
	}
	if err != nil {
		return app.Wallet{}, err
	}

	periods, err := s.periodsOf(id)
	if err != nil {
		return app.Wallet{}, err
	}
	w.Periods = periods

	people, err := s.peopleOf(id)
	if err != nil {
		return app.Wallet{}, err
	}
	expenses, err := s.expensesOf(id)
	if err != nil {
		return app.Wallet{}, err
	}
	w.People = people
	w.Expenses = expenses
	contributions, err := s.contributionsOf(id)
	if err != nil {
		return app.Wallet{}, err
	}
	w.Contributions = contributions

	return w, nil
}

/* AddWallet - inserts a new wallet and returns it with its assigned ID. */
func (s *SQLiteStore) AddWallet(name, periodType string) (app.Wallet, error) {
	res, err := s.db.Exec(`INSERT INTO wallets (name, period_type) VALUES (?, ?);`, name, periodType)
	if err != nil {
		return app.Wallet{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return app.Wallet{}, err
	}

	return app.Wallet{ID: id, Name: name, PeriodType: periodType}, nil
}

/* DeleteWallet - removes a wallet (its people and expenses cascade away). */
func (s *SQLiteStore) DeleteWallet(id int64) error {
	_, err := s.db.Exec(`DELETE FROM wallets WHERE id = ?;`, id)
	return err
}

// AddPeriod makes a period (pane) available in a wallet. It is idempotent so
// selecting an existing period never duplicates it.
func (s *SQLiteStore) AddPeriod(walletID int64, periodKey string) error {
	_, err := s.db.Exec(
		`INSERT INTO budget_months (wallet_id, month) VALUES (?, ?) ON CONFLICT(wallet_id, month) DO NOTHING`,
		walletID, periodKey,
	)
	return err
}

// DeletePeriod removes a pane and every expense that falls inside it. Expenses
// carry a full RFC3339 date whose prefix is the period key (e.g. "2026-07" for
// a monthly pane), so a prefix match scopes the deletion to that period.
func (s *SQLiteStore) DeletePeriod(walletID int64, periodKey string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`DELETE FROM expenses WHERE wallet_id = ? AND date LIKE ? || '%'`,
		walletID, periodKey,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM budget_months WHERE wallet_id = ? AND month = ?`,
		walletID, periodKey,
	); err != nil {
		return err
	}

	return tx.Commit()
}

/* *****************************************************************************
*
* People
*
*******************************************************************************/

/* AddPerson - adds a person to a wallet. */
func (s *SQLiteStore) AddPerson(walletID int64, name string) error {
	_, err := s.db.Exec(
		`INSERT INTO people (wallet_id, name) VALUES (?, ?);`,
		walletID, name,
	)
	return err
}

/* DeletePerson - removes a person from a wallet. */
func (s *SQLiteStore) DeletePerson(walletID, personID int64) error {
	_, err := s.db.Exec(
		`DELETE FROM people WHERE id = ? AND wallet_id = ?;`,
		personID, walletID,
	)
	return err
}

// FundWallet saves each participant's income settings. The planned wallet
// amount is derived from these settings, so repeat submissions never accrue.
func (s *SQLiteStore) FundWallet(walletID int64, funding []app.Funding) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, f := range funding {
		result, err := tx.Exec(
			`UPDATE people SET salary_cents = ?, contribution_percent = ? WHERE id = ? AND wallet_id = ?`,
			f.SalaryCents, f.ContributionPercent, f.PersonID, walletID,
		)
		if err != nil {
			return err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if updated != 1 {
			return fmt.Errorf("person %d does not belong to wallet %d", f.PersonID, walletID)
		}
	}

	return tx.Commit()
}

/* *****************************************************************************
*
* Expenses
*
*******************************************************************************/

/* AddExpense - adds an expense to a wallet. */
func (s *SQLiteStore) AddExpense(walletID int64, e app.Expense) error {
	_, err := s.db.Exec(
		`INSERT INTO expenses
			(wallet_id, description, category, paid_by_id, amount_cents, date, is_paid)
		 VALUES (?, ?, ?, ?, ?, ?, ?);`,
		walletID, e.Description, e.Category, e.PaidByID, e.AmountCents,
		e.Date.Format(time.RFC3339), e.IsPaid,
	)
	return err
}

// MarkExpensePaid marks a pending wallet expense as paid exactly once.
func (s *SQLiteStore) MarkExpensePaid(walletID, expenseID int64) error {
	_, err := s.db.Exec(
		`UPDATE expenses SET is_paid = 1 WHERE id = ? AND wallet_id = ? AND is_paid = 0`,
		expenseID, walletID,
	)
	return err
}

/* DeleteExpense - removes an expense from a wallet. */
func (s *SQLiteStore) DeleteExpense(walletID, expenseID int64) error {
	_, err := s.db.Exec(
		`DELETE FROM expenses WHERE id = ? AND wallet_id = ?;`,
		expenseID, walletID,
	)
	return err
}

/* *****************************************************************************
*
* Private helpers
*
*******************************************************************************/

/* peopleOf - loads every person belonging to a wallet. */
func (s *SQLiteStore) peopleOf(walletID int64) ([]app.Person, error) {
	rows, err := s.db.Query(
		`SELECT id, name, salary_cents, contribution_percent FROM people WHERE wallet_id = ? ORDER BY id;`,
		walletID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var people []app.Person
	for rows.Next() {
		var p app.Person
		if err := rows.Scan(&p.ID, &p.Name, &p.SalaryCents, &p.ContributionPercent); err != nil {
			return nil, err
		}
		people = append(people, p)
	}

	return people, rows.Err()
}

func (s *SQLiteStore) periodsOf(walletID int64) ([]app.Period, error) {
	rows, err := s.db.Query(
		`SELECT month FROM budget_months WHERE wallet_id = ? ORDER BY month DESC`,
		walletID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var periods []app.Period
	for rows.Next() {
		var period app.Period
		if err := rows.Scan(&period.Key); err != nil {
			return nil, err
		}
		periods = append(periods, period)
	}
	return periods, rows.Err()
}

func (s *SQLiteStore) contributionsOf(walletID int64) ([]app.Contribution, error) {
	rows, err := s.db.Query(
		`SELECT id, person_id, amount_cents, date FROM contributions WHERE wallet_id = ? ORDER BY date DESC, id DESC;`,
		walletID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contributions []app.Contribution
	for rows.Next() {
		var (
			contribution app.Contribution
			rawDate      string
		)
		if err := rows.Scan(&contribution.ID, &contribution.PersonID, &contribution.AmountCents, &rawDate); err != nil {
			return nil, err
		}
		date, err := time.Parse(time.RFC3339, rawDate)
		if err != nil {
			return nil, err
		}
		contribution.Date = date
		contributions = append(contributions, contribution)
	}

	return contributions, rows.Err()
}

func addColumnIfMissing(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			typeName   string
			notNull    int
			defaultV   any
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultV, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

/* expensesOf - loads every expense belonging to a wallet (newest first). */
func (s *SQLiteStore) expensesOf(walletID int64) ([]app.Expense, error) {
	rows, err := s.db.Query(
		`SELECT id, description, category, paid_by_id, amount_cents, date, is_paid
		 FROM expenses WHERE wallet_id = ? ORDER BY date DESC, id DESC;`,
		walletID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expenses []app.Expense
	for rows.Next() {
		var (
			e       app.Expense
			rawDate string
			isPaid  int
		)
		if err := rows.Scan(
			&e.ID, &e.Description, &e.Category,
			&e.PaidByID, &e.AmountCents, &rawDate, &isPaid,
		); err != nil {
			return nil, err
		}

		date, err := time.Parse(time.RFC3339, rawDate)
		if err != nil {
			return nil, err
		}
		e.Date = date
		e.IsPaid = isPaid != 0

		expenses = append(expenses, e)
	}

	return expenses, rows.Err()
}
