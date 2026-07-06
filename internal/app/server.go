/* SERVER

Author: Luigi Piantavinha

This file is the web/application layer of BananaSplit. It owns everything that
happens between an incoming HTTP request and an outgoing HTML response:

Content:
  - Server:        holds the store and the parsed HTML templates.
  - IndexPageData: the view-model passed to the index template.
  - Constructor:   NewServer (parses templates, wires the store).
  - Routing:       Routes (maps URLs/methods to handlers).
  - Handlers:      handleIndex, handleCreateExpense, handleDeleteExpense.
  - Business logic helpers: filterByMonth, summarize (the 50/50 split math).
  - Formatting/parsing helpers: parseMoney, formatMoney, formatDate.
  - Utility helpers: redirectWithError, abs.

The layer depends only on the ExpenseStore interface, so it has no knowledge of
how or where expenses are actually persisted.

*/

package app

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

/* Server - the application's HTTP server.
*
* It bundles the two things every request needs: a data store to read/write
* expenses, and the pre-parsed HTML templates used to render pages.
*
* Fields:
*   - store:     any implementation of the ExpenseStore interface.
*   - templates: all templates parsed once at startup, ready to execute.
 */

type Server struct {
	store     ExpenseStore
	templates *template.Template
}

/* IndexPageData - the view-model handed to the index.html template.
*
* It gathers everything the page needs to render in a single struct.
*
* Fields:
*   - Expenses: the expenses for the selected month.
*   - Summary:  the computed monthly totals and settlement.
*   - Month:    the currently selected month ("2006-01").
*   - Error:    an optional error message to display as a banner.
 */

type IndexPageData struct {
	Expenses []Expense
	Summary  MonthlySummary
	Month    string
	Error    string
}

/* NewServer - builds a ready-to-use Server.
*
* It parses every HTML template under web/templates once and registers the
* custom template helpers ("money" and "date") so the views can format values.
* Parsing at construction time means template errors are caught at startup
* rather than on the first request.
*
* Parameters:
*   - store: the ExpenseStore the server will read from and write to.
*
* Returns:
*   - A pointer to the configured Server, or an error if template parsing fails.
 */

func NewServer(store ExpenseStore) (*Server, error) {
	templates, err := template.New("").Funcs(template.FuncMap{
		"money": formatMoney,
		"date":  formatDate,
	}).ParseGlob("web/templates/*.html")

	if err != nil {
		return nil, err
	}

	return &Server{
		store:     store,
		templates: templates,
	}, nil
}

/* Routes - declares the URL routing table and returns the HTTP handler.
*
* It maps each method+path pattern to the handler responsible for it, and also
* serves static assets (CSS) from web/static. It uses Go's method-based routing
* patterns, so no external router is required.
*
* Parameters:
*   - None (method receiver s provides the handlers).
*
* Returns:
*   - An http.Handler (the configured ServeMux) ready to be passed to a server.
 */

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /expenses", s.handleCreateExpense)
	mux.HandleFunc("POST /expenses/delete", s.handleDeleteExpense)

	return mux
}

/* handleIndex - renders the main dashboard page (GET /).
*
* It figures out which month to show, loads all expenses, keeps only those in
* that month, computes the monthly summary, and renders index.html. Any error
* message present in the query string is surfaced to the user.
*
* Parameters:
*   - w: the HTTP response writer used to send the HTML back.
*   - r: the incoming request; its "month" and "error" query params are read.
*
* Returns:
*   - Nothing. On failure it writes an HTTP 500 response directly.
 */

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {

	/* Get the month from the query parameters */
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	/* Get the expenses for the selected month */
	expenses, err := s.store.All()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	monthlyExpenses := filterByMonth(expenses, month)
	data := IndexPageData{
		Expenses: monthlyExpenses,
		Summary:  summarize(monthlyExpenses, month),
		Month:    month,
		Error:    r.URL.Query().Get("error"),
	}

	if err := s.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

/* handleCreateExpense - validates and stores a new expense (POST /expenses).
*
* It parses the submitted form, validates every field (amount, date, who paid,
* description), applies defaults (empty category becomes "Geral"), and saves the
* expense. On any validation problem it redirects back with an error message.
* On success it uses the Post/Redirect/Get pattern so a page refresh cannot
* re-submit the form.
*
* Parameters:
*   - w: the HTTP response writer (used to redirect or report errors).
*   - r: the incoming request carrying the submitted form values.
*
* Returns:
*   - Nothing. It always ends by redirecting or writing an error response.
 */

func (s *Server) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "Cannot read form")
		return
	}

	amountCents, err := parseMoney(r.FormValue("amount"))
	if err != nil || amountCents <= 0 {
		redirectWithError(w, r, "Valor invalido")
		return
	}

	date, err := time.Parse("2006-01-02", r.FormValue("date"))
	if err != nil {
		redirectWithError(w, r, "Data invalida")
		return
	}

	expense := Expense{
		Description: strings.TrimSpace(r.FormValue("description")),
		Category:    strings.TrimSpace(r.FormValue("category")),
		PaidBy:      r.FormValue("paid_by"),
		AmountCents: amountCents,
		Date:        date,
	}

	if expense.Description == "" {
		redirectWithError(w, r, "Descricao obrigatoria")
		return
	}

	if expense.Category == "" {
		expense.Category = "Geral"
	}

	if expense.PaidBy != "Luigi" && expense.PaidBy != "Parceira" {
		redirectWithError(w, r, "Pessoa invalida")
		return
	}

	if _, err := s.store.Add(expense); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/?month="+date.Format("2006-01"), http.StatusSeeOther)
}

/* handleDeleteExpense - removes an expense (POST /expenses/delete).
*
* It reads the hidden "id" field, deletes the matching expense from the store,
* then redirects back to the month the user was viewing (Post/Redirect/Get).
*
* Parameters:
*   - w: the HTTP response writer (used to redirect or report errors).
*   - r: the incoming request; its "id" and "month" form values are read.
*
* Returns:
*   - Nothing. It always ends by redirecting or writing an error response.
 */

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "Nao consegui ler o formulario")
		return
	}

	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		redirectWithError(w, r, "Despesa invalida")
		return
	}

	if err := s.store.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	month := r.FormValue("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	http.Redirect(w, r, "/?month="+month, http.StatusSeeOther)
}

/* filterByMonth - keeps only the expenses that fall in a given month.
*
* It compares each expense's date, formatted as "2006-01", against the target
* month string.
*
* Parameters:
*   - expenses: the full list of expenses to filter.
*   - month:    the target month as "2006-01".
*
* Returns:
*   - A new slice containing only the expenses from that month (may be nil).
 */

func filterByMonth(expenses []Expense, month string) []Expense {
	var filtered []Expense
	for _, expense := range expenses {
		if expense.Date.Format("2006-01") == month {
			filtered = append(filtered, expense)
		}
	}

	return filtered
}

/* summarize - computes the monthly totals and the 50/50 settlement.
*
* It sums every expense, splitting how much each person paid. Each person's fair
* share is half the total; the partner's share is computed as (total - Luigi's
* share) so the two halves always add back to the exact total (no lost cent).
* The settlement is the size of Luigi's imbalance, and a sentence explains who
* owes whom.
*
* Parameters:
*   - expenses: the expenses belonging to the month (already filtered).
*   - month:    the month being summarised, as "2006-01".
*
* Returns:
*   - A fully populated MonthlySummary.
 */

func summarize(expenses []Expense, month string) MonthlySummary {
	summary := MonthlySummary{Month: month}

	for _, expense := range expenses {
		summary.TotalCents += expense.AmountCents
		switch expense.PaidBy {
		case "Luigi":
			summary.LuigiPaidCents += expense.AmountCents
		case "Parceira":
			summary.PartnerPaidCents += expense.AmountCents
		}
	}

	summary.LuigiShareCents = summary.TotalCents / 2
	summary.PartnerShareCents = summary.TotalCents - summary.LuigiShareCents

	luigiBalance := summary.LuigiPaidCents - summary.LuigiShareCents
	summary.SettlementCents = abs(luigiBalance)

	switch {
	case luigiBalance > 0:
		summary.SettlementSentence = "Parceira deve pagar a Luigi"
	case luigiBalance < 0:
		summary.SettlementSentence = "Luigi deve pagar a Parceira"
	default:
		summary.SettlementSentence = "Esta tudo certo entre voces"
	}

	return summary
}

/* parseMoney - converts a user-typed amount into integer cents.
*
* It accepts both comma and dot as the decimal separator (e.g. "42,50" or
* "42.50"), pads a single decimal digit, and rejects empty values, more than one
* separator, or more than two decimal places. Working in cents keeps all money
* arithmetic exact.
*
* Parameters:
*   - value: the raw text from the form (e.g. "42,50").
*
* Returns:
*   - The amount in cents as int64, or an error if the text is invalid.
 */

func parseMoney(value string) (int64, error) {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, ",", ".")
	if cleaned == "" {
		return 0, fmt.Errorf("empty amount")
	}

	parts := strings.Split(cleaned, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid amount")
	}

	euros, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}

	var cents int64
	if len(parts) == 2 {
		decimal := parts[1]
		if len(decimal) == 1 {
			decimal += "0"
		}
		if len(decimal) > 2 {
			return 0, fmt.Errorf("too many decimal places")
		}

		cents, err = strconv.ParseInt(decimal, 10, 64)
		if err != nil {
			return 0, err
		}
	}

	return euros*100 + cents, nil
}

/* formatMoney - formats an amount in cents for display.
*
* It turns integer cents into a euro string with two decimals and the euro sign
* (e.g. 4250 -> "42,50 €"). Registered as the "money" template helper.
*
* Parameters:
*   - cents: the amount in integer cents.
*
* Returns:
*   - The formatted string, e.g. "42,50 €".
 */

func formatMoney(cents int64) string {
	return fmt.Sprintf("%d,%02d €", cents/100, cents%100)
}

/* formatDate - formats a date for display.
*
* It renders a time.Time as "day/month/year" (e.g. "04/07/2026"). Registered as
* the "date" template helper.
*
* Parameters:
*   - date: the date to format.
*
* Returns:
*   - The formatted string, e.g. "04/07/2026".
 */

func formatDate(date time.Time) string {
	return date.Format("02/01/2006")
}

/* redirectWithError - redirects back to the index page carrying an error.
*
* It preserves the current month and attaches the given message as an "error"
* query parameter, so the dashboard can show a banner. Uses HTTP 303 See Other.
*
* Parameters:
*   - w:       the HTTP response writer used to issue the redirect.
*   - r:       the incoming request (its "month" form value is preserved).
*   - message: the human-readable error to display.
*
* Returns:
*   - Nothing. It writes a redirect response.
 */

func redirectWithError(w http.ResponseWriter, r *http.Request, message string) {
	month := r.FormValue("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}

	values := url.Values{}
	values.Set("month", month)
	values.Set("error", message)

	http.Redirect(w, r, "/?"+values.Encode(), http.StatusSeeOther)
}

/* abs - returns the absolute value of an int64.
*
* A small helper used to turn a possibly-negative balance into a positive
* settlement amount.
*
* Parameters:
*   - value: the number whose magnitude is wanted.
*
* Returns:
*   - The non-negative absolute value of value.
 */

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}

	return value
}
