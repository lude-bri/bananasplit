/* SERVER

Author: Luigi Piantavinha

This file is the web/application layer of BananaSplit. It owns everything that
happens between an incoming HTTP request and an outgoing HTML response.

Content:
  - Server:         holds the store and the parsed HTML templates.
  - View-models:    IndexPageData, WalletPageData.
  - Constructor:    NewServer (parses templates, wires the store).
  - Routing:        Routes (maps URLs/methods to handlers).
  - Handlers:       index + wallet + people + expenses CRUD.
  - Business logic: summarize (equal split + settlement plan).
  - Helpers:        parseMoney, formatMoney, formatDate, redirect helpers.

The layer depends only on the WalletStore interface, so it has no knowledge of
how or where data is actually persisted.

*/

package app

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

/* Server - the application's HTTP server.
*
* Fields:
*   - store:     any implementation of the WalletStore interface.
*   - templates: all templates parsed once at startup, ready to execute.
 */

type Server struct {
	store     WalletStore
	templates *template.Template
}

/* IndexPageData - the view-model handed to index.html. */
type IndexPageData struct {
	Wallets []Wallet
	Error   string
}

/* WalletPageData - the view-model handed to wallet.html. */
type WalletPageData struct {
	Wallet           Wallet
	Summary          WalletSummary
	Period           string
	UnpaidExpenses   []Expense
	PaidExpenses     []Expense
	Categories       []string
	ShowFundingSetup bool
	Error            string
}

/* NewServer - builds a ready-to-use Server.
*
* It parses every HTML template under web/templates once and registers the
* custom template helpers so the views can format values. Parsing at
* construction time means template errors are caught at startup.
*
* Parameters:
*   - store: the WalletStore the server will read from and write to.
*
* Returns:
*   - A pointer to the configured Server, or an error if parsing fails.
 */

func NewServer(store WalletStore) (*Server, error) {
	templates, err := template.New("").Funcs(template.FuncMap{
		"money":        formatMoney,
		"moneyInput":   formatMoneyInput,
		"contribution": func(salary, percent int64) int64 { return salary * percent / 100 },
		"percent": func(part, whole int64) int64 {
			if whole <= 0 {
				return 0
			}
			return part * 100 / whole
		},
		"periodLabel":    periodLabel,
		"periodAdverb":   periodAdverb,
		"periodUnitNoun": periodUnitNoun,
		"date":           formatDate,
		"assetVersion":   assetVersion,
	}).ParseGlob("web/templates/*.html")
	if err != nil {
		return nil, err
	}

	return &Server{store: store, templates: templates}, nil
}

/* assetVersion - the modification time (unix seconds) of a file under
*  web/static, used as a ?v= cache-buster on asset links so a changed CSS/JS
*  file is always re-fetched even by browsers holding a stale cached copy. */
func assetVersion(name string) int64 {
	info, err := os.Stat(filepath.Join("web/static", name))
	if err != nil {
		return 0
	}
	return info.ModTime().Unix()
}

/* noCache - forces the browser to revalidate on every request, so CSS/JS edits
*  and freshly-changed wallet data always show up without a manual hard-refresh
*  (e.g. switching panes always reflects that period's expenses). Static files
*  are still served with 304s when nothing changed. */
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

/* Routes - declares the URL routing table and returns the HTTP handler. */
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Wallet list (home)
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("GET /wallets/new", s.handleNewWallet)
	mux.HandleFunc("POST /wallets", s.handleCreateWallet)
	mux.HandleFunc("POST /wallets/{id}/periods", s.handleAddPeriod)
	mux.HandleFunc("POST /wallets/{id}/periods/delete", s.handleDeletePeriod)
	mux.HandleFunc("POST /wallets/delete", s.handleDeleteWallet)

	// Single wallet
	mux.HandleFunc("GET /wallets/{id}", s.handleWallet)

	// People
	mux.HandleFunc("POST /wallets/{id}/people", s.handleAddPerson)
	mux.HandleFunc("POST /wallets/{id}/people/delete", s.handleDeletePerson)
	mux.HandleFunc("POST /wallets/{id}/fund", s.handleFundWallet)

	// Expenses
	mux.HandleFunc("POST /wallets/{id}/expenses", s.handleAddExpense)
	mux.HandleFunc("POST /wallets/{id}/expenses/{expenseID}/paid", s.handleMarkExpensePaid)
	mux.HandleFunc("POST /wallets/{id}/expenses/delete", s.handleDeleteExpense)

	return noCache(mux)
}

/* *****************************************************************************
*
* Handlers - wallet list (home)
*
*******************************************************************************/

/* handleIndex renders the landing page, listing any wallets already created. */
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	wallets, err := s.store.AllWallets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := IndexPageData{
		Wallets: wallets,
		Error:   r.URL.Query().Get("error"),
	}

	if err := s.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

/* handleNewWallet renders the wallet and participant setup form. */
func (s *Server) handleNewWallet(w http.ResponseWriter, r *http.Request) {
	if err := s.templates.ExecuteTemplate(w, "new_wallet.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

/* handleCreateWallet - creates a new wallet (POST /wallets). */
func (s *Server) handleCreateWallet(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectHomeError(w, r, "Cannot read form")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		redirectHomeError(w, r, "Wallet name is required")
		return
	}

	people := r.Form["people"]
	var participantNames []string
	for _, person := range people {
		if person = strings.TrimSpace(person); person != "" {
			participantNames = append(participantNames, person)
		}
	}
	if len(participantNames) == 0 {
		redirectHomeError(w, r, "Add at least one participant")
		return
	}

	periodType := normalizePeriodType(r.FormValue("period"))
	// The starting pane. Falls back to the current period when left empty or
	// invalid, so the wallet always opens on a valid pane.
	start := readPeriodKey(r, "start", periodType)
	if !validPeriodKey(start, periodType) {
		start = periodKeyOf(time.Now(), periodType)
	}

	wallet, err := s.store.AddWallet(name, periodType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, participant := range participantNames {
		if err := s.store.AddPerson(wallet.ID, participant); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := s.store.AddPeriod(wallet.ID, start); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirectWalletPeriod(w, r, wallet.ID, start)
}

// handleAddPeriod makes a new period (pane) available on a wallet dashboard.
func (s *Server) handleAddPeriod(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}
	wallet, err := s.store.GetWallet(id)
	if err != nil {
		redirectHomeError(w, r, "Wallet not found")
		return
	}
	period := readPeriodKey(r, "period", wallet.PeriodType)
	if !validPeriodKey(period, wallet.PeriodType) {
		redirectWalletError(w, r, id, "Choose a valid "+periodUnitNoun(wallet.PeriodType))
		return
	}
	if err := s.store.AddPeriod(id, period); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectWalletPeriod(w, r, id, period)
}

// handleDeletePeriod removes a pane and the expenses inside it.
func (s *Server) handleDeletePeriod(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}
	period := r.FormValue("period")
	if period == "" {
		redirectWalletError(w, r, id, "Invalid period")
		return
	}
	if err := s.store.DeletePeriod(id, period); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Land on a remaining pane (handleWallet picks the first, or re-creates the
	// current period if this was the last one).
	redirectWallet(w, r, id)
}

/* handleDeleteWallet - deletes a wallet (POST /wallets/delete). */
func (s *Server) handleDeleteWallet(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectHomeError(w, r, "Cannot read form")
		return
	}

	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}

	if err := s.store.DeleteWallet(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

/* *****************************************************************************
*
* Handlers - single wallet
*
*******************************************************************************/

/* handleWallet - renders one wallet's page (GET /wallets/{id}). */
func (s *Server) handleWallet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}

	wallet, err := s.store.GetWallet(id)
	if err != nil {
		redirectHomeError(w, r, "Wallet not found")
		return
	}

	// A wallet with no periods renders an empty state (period == "") that just
	// invites the user to add one. Deleting the last pane is allowed and lands
	// here, rather than silently re-creating the current period.
	period := r.URL.Query().Get("period")
	if !walletHasPeriod(wallet, period) {
		period = ""
		if len(wallet.Periods) > 0 {
			period = wallet.Periods[0].Key
		}
	}
	data := WalletPageData{
		Wallet:           wallet,
		Summary:          summarize(wallet, period),
		Period:           period,
		Categories:       distinctCategories(wallet.Expenses),
		ShowFundingSetup: r.URL.Query().Get("edit-contributions") == "1" || needsFundingSetup(wallet),
		Error:            r.URL.Query().Get("error"),
	}
	for _, expense := range wallet.Expenses {
		if periodKeyOf(expense.Date, wallet.PeriodType) != period {
			continue
		}
		if expense.IsPaid {
			data.PaidExpenses = append(data.PaidExpenses, expense)
		} else {
			data.UnpaidExpenses = append(data.UnpaidExpenses, expense)
		}
	}

	if err := s.templates.ExecuteTemplate(w, "wallet.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

/* *****************************************************************************
*
* Handlers - people
*
*******************************************************************************/

/* handleAddPerson - adds a person to a wallet (POST /wallets/{id}/people). */
func (s *Server) handleAddPerson(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWalletError(w, r, id, "Cannot read form")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		redirectWalletError(w, r, id, "Person name is required")
		return
	}

	if err := s.store.AddPerson(id, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirectWallet(w, r, id)
}

/* handleDeletePerson - removes a person (POST /wallets/{id}/people/delete). */
func (s *Server) handleDeletePerson(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWalletError(w, r, id, "Cannot read form")
		return
	}

	personID, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		redirectWalletError(w, r, id, "Invalid person")
		return
	}

	if err := s.store.DeletePerson(id, personID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirectWallet(w, r, id)
}

/* handleFundWallet records every participant's income contribution. */
func (s *Server) handleFundWallet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWalletError(w, r, id, "Cannot read form")
		return
	}

	personIDs := r.Form["person_id"]
	salaries := r.Form["salary"]
	percentages := r.Form["contribution_percent"]
	if len(personIDs) == 0 || len(personIDs) != len(salaries) || len(personIDs) != len(percentages) {
		redirectWalletError(w, r, id, "Complete the funding details for every participant")
		return
	}

	funding := make([]Funding, 0, len(personIDs))
	for i, rawID := range personIDs {
		personID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			redirectWalletError(w, r, id, "Invalid participant")
			return
		}
		salaryCents, err := parseMoney(salaries[i])
		if err != nil || salaryCents <= 0 {
			redirectWalletError(w, r, id, "Enter a valid monthly income")
			return
		}
		percent, err := strconv.ParseInt(percentages[i], 10, 64)
		if err != nil || percent < 1 || percent > 100 {
			redirectWalletError(w, r, id, "Contribution percentage must be between 1 and 100")
			return
		}
		funding = append(funding, Funding{PersonID: personID, SalaryCents: salaryCents, ContributionPercent: percent})
	}

	if err := s.store.FundWallet(id, funding); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectWalletPeriod(w, r, id, r.FormValue("period"))
}

/* *****************************************************************************
*
* Handlers - expenses
*
*******************************************************************************/

/* handleAddExpense - adds an expense (POST /wallets/{id}/expenses). */
func (s *Server) handleAddExpense(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}

	wallet, err := s.store.GetWallet(id)
	if err != nil {
		redirectHomeError(w, r, "Wallet not found")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWalletError(w, r, id, "Cannot read form")
		return
	}

	amountCents, err := parseMoney(r.FormValue("amount"))
	if err != nil || amountCents <= 0 {
		redirectWalletError(w, r, id, "Invalid amount")
		return
	}

	date, err := time.Parse("2006-01-02", r.FormValue("date"))
	if err != nil {
		redirectWalletError(w, r, id, "Invalid date")
		return
	}
	period := r.FormValue("period")
	if !validPeriodKey(period, wallet.PeriodType) || periodKeyOf(date, wallet.PeriodType) != period {
		redirectWalletError(w, r, id, "Choose a date inside the selected "+periodUnitNoun(wallet.PeriodType))
		return
	}

	paidByID, err := strconv.ParseInt(r.FormValue("paid_by_id"), 10, 64)
	if err != nil {
		redirectWalletError(w, r, id, "Choose who paid")
		return
	}

	description := strings.TrimSpace(r.FormValue("description"))
	if description == "" {
		redirectWalletError(w, r, id, "Description is required")
		return
	}

	category := strings.TrimSpace(r.FormValue("category"))
	if category == "" {
		category = "General"
	}

	expense := Expense{
		Description: description,
		Category:    category,
		PaidByID:    paidByID,
		AmountCents: amountCents,
		Date:        date,
		IsPaid:      false,
	}

	if err := s.store.AddExpense(id, expense); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirectWalletPeriod(w, r, id, period)
}

// handleMarkExpensePaid moves an expense out of the pending payment list.
func (s *Server) handleMarkExpensePaid(w http.ResponseWriter, r *http.Request) {
	walletID, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}
	expenseID, err := strconv.ParseInt(r.PathValue("expenseID"), 10, 64)
	if err != nil {
		redirectWalletError(w, r, walletID, "Invalid expense")
		return
	}
	if err := s.store.MarkExpensePaid(walletID, expenseID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectWalletPeriod(w, r, walletID, r.FormValue("period"))
}

/* handleDeleteExpense - removes an expense (POST /wallets/{id}/expenses/delete). */
func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		redirectHomeError(w, r, "Invalid wallet")
		return
	}

	if err := r.ParseForm(); err != nil {
		redirectWalletError(w, r, id, "Cannot read form")
		return
	}

	expenseID, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil {
		redirectWalletError(w, r, id, "Invalid expense")
		return
	}

	if err := s.store.DeleteExpense(id, expenseID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	redirectWalletPeriod(w, r, id, r.FormValue("period"))
}

/* *****************************************************************************
*
* Business logic
*
*******************************************************************************/

func summarize(wallet Wallet, selectedPeriod string) WalletSummary {
	summary := WalletSummary{}
	for _, person := range wallet.People {
		summary.PlannedContributionCents += person.SalaryCents * person.ContributionPercent / 100
	}

	periods := make(map[string]struct{}, len(wallet.Periods))
	for _, period := range wallet.Periods {
		periods[period.Key] = struct{}{}
	}
	categoryTotals := make(map[string]int64)
	var allExpensesCents int64
	for _, e := range wallet.Expenses {
		key := periodKeyOf(e.Date, wallet.PeriodType)
		periods[key] = struct{}{}
		allExpensesCents += e.AmountCents
		categoryTotals[e.Category] += e.AmountCents
		if key == selectedPeriod {
			summary.TotalExpensesCents += e.AmountCents
			if e.IsPaid {
				summary.PaidExpensesCents += e.AmountCents
			}
		}
	}
	summary.PeriodsTracked = len(periods)
	if summary.PeriodsTracked > 0 {
		summary.AveragePeriodCents = allExpensesCents / int64(summary.PeriodsTracked)
		summary.CategoryAverages = categoryAverages(categoryTotals, summary.PeriodsTracked)
	}
	summary.OutstandingExpensesCents = summary.TotalExpensesCents - summary.PaidExpensesCents
	summary.BalanceCents = summary.PlannedContributionCents - summary.PaidExpensesCents
	return summary
}

/* categoryAverages - turns per-category totals into a slice sorted by average
*  spend (highest first), so the biggest categories lead the report. */
func categoryAverages(totals map[string]int64, periodsTracked int) []CategoryAverage {
	averages := make([]CategoryAverage, 0, len(totals))
	for name, total := range totals {
		averages = append(averages, CategoryAverage{
			Name:         name,
			TotalCents:   total,
			AverageCents: total / int64(periodsTracked),
		})
	}
	sort.Slice(averages, func(i, j int) bool {
		if averages[i].AverageCents != averages[j].AverageCents {
			return averages[i].AverageCents > averages[j].AverageCents
		}
		return averages[i].Name < averages[j].Name
	})
	return averages
}

/* distinctCategories - the wallet's category labels, de-duplicated and sorted,
*  used to suggest previously-typed categories on the expense form. */
func distinctCategories(expenses []Expense) []string {
	seen := make(map[string]struct{}, len(expenses))
	var categories []string
	for _, e := range expenses {
		if e.Category == "" {
			continue
		}
		if _, ok := seen[e.Category]; ok {
			continue
		}
		seen[e.Category] = struct{}{}
		categories = append(categories, e.Category)
	}
	sort.Strings(categories)
	return categories
}

func walletHasPeriod(wallet Wallet, period string) bool {
	for _, candidate := range wallet.Periods {
		if candidate.Key == period {
			return true
		}
	}
	return false
}

/* *****************************************************************************
*
* Period helpers - map a wallet's granularity to key formats and labels.
*
*******************************************************************************/

/* normalizePeriodType - coerces free-form input to a supported granularity,
*  defaulting to monthly. */
func normalizePeriodType(periodType string) string {
	switch periodType {
	case PeriodDaily, PeriodMonthly, PeriodYearly:
		return periodType
	default:
		return PeriodMonthly
	}
}

/* periodKeyLayout - the time layout used to render/parse a period key. */
func periodKeyLayout(periodType string) string {
	switch normalizePeriodType(periodType) {
	case PeriodDaily:
		return "2006-01-02"
	case PeriodYearly:
		return "2006"
	default:
		return "2006-01"
	}
}

/* periodKeyOf - renders a time as the period key for a granularity. */
func periodKeyOf(t time.Time, periodType string) string {
	return t.Format(periodKeyLayout(periodType))
}

/* readPeriodKey - reads a period key from a form for a given granularity.
*
* Monthly panes are picked with an English month dropdown plus a year field
* (the native <input type="month"> can't be forced out of the browser locale),
* submitted as "<field>_month" + "<field>_year" and joined into "YYYY-MM".
* Daily and yearly panes come straight from a single "<field>" input. */
func readPeriodKey(r *http.Request, field, periodType string) string {
	if normalizePeriodType(periodType) == PeriodMonthly {
		year := strings.TrimSpace(r.FormValue(field + "_year"))
		month := strings.TrimSpace(r.FormValue(field + "_month"))
		if year == "" || month == "" {
			return ""
		}
		return year + "-" + month
	}
	return strings.TrimSpace(r.FormValue(field))
}

/* validPeriodKey - reports whether key is well-formed for the granularity. */
func validPeriodKey(key, periodType string) bool {
	_, err := time.Parse(periodKeyLayout(periodType), key)
	return err == nil
}

/* periodLabel - a human-friendly label for a period key (template helper). */
func periodLabel(periodType, key string) string {
	t, err := time.Parse(periodKeyLayout(periodType), key)
	if err != nil {
		return key
	}
	switch normalizePeriodType(periodType) {
	case PeriodDaily:
		return t.Format("2 Jan 2006")
	case PeriodYearly:
		return t.Format("2006")
	default:
		return t.Format("January 2006")
	}
}

/* periodAdverb - "daily" / "monthly" / "yearly" (template helper). */
func periodAdverb(periodType string) string {
	switch normalizePeriodType(periodType) {
	case PeriodDaily:
		return "daily"
	case PeriodYearly:
		return "yearly"
	default:
		return "monthly"
	}
}

/* periodUnitNoun - "day" / "month" / "year" (template helper). */
func periodUnitNoun(periodType string) string {
	switch normalizePeriodType(periodType) {
	case PeriodDaily:
		return "day"
	case PeriodYearly:
		return "year"
	default:
		return "month"
	}
}

func needsFundingSetup(wallet Wallet) bool {
	for _, person := range wallet.People {
		if person.SalaryCents <= 0 || person.ContributionPercent <= 0 {
			return true
		}
	}
	return false
}

/* *****************************************************************************
*
* Formatting / parsing helpers
*
*******************************************************************************/

/* parseMoney - converts a user-typed amount into integer cents.
*
* It accepts both comma and dot as the decimal separator (e.g. "42,50" or
* "42.50"), pads a single decimal digit, and rejects empty values, more than one
* separator, or more than two decimal places.
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

/* formatMoney - formats cents for display, e.g. 4250 -> "42,50 €". */
func formatMoney(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d,%02d €", sign, cents/100, cents%100)
}

/* formatMoneyInput - like formatMoney but without the currency symbol, so it can
*  be placed in an <input value="..."> (e.g. 30000 -> "300,00"). */
func formatMoneyInput(cents int64) string {
	return fmt.Sprintf("%d,%02d", cents/100, cents%100)
}

/* formatDate - renders a time.Time as "02/01/2006". */
func formatDate(date time.Time) string {
	return date.Format("02/01/2006")
}

/* *****************************************************************************
*
* Redirect helpers
*
*******************************************************************************/

/* pathID - reads the {id} path segment from the request as an int64. */
func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

/* redirectWallet - redirects (303) back to a wallet's page. */
func redirectWallet(w http.ResponseWriter, r *http.Request, id int64) {
	http.Redirect(w, r, fmt.Sprintf("/wallets/%d", id), http.StatusSeeOther)
}

// redirectWalletPeriod returns to the selected period when one is given.
func redirectWalletPeriod(w http.ResponseWriter, r *http.Request, id int64, period string) {
	if period == "" {
		redirectWallet(w, r, id)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/wallets/%d?period=%s", id, period), http.StatusSeeOther)
}

/* redirectWalletError - redirects back to a wallet's page with an error banner. */
func redirectWalletError(w http.ResponseWriter, r *http.Request, id int64, message string) {
	http.Redirect(w, r,
		fmt.Sprintf("/wallets/%d?error=%s", id, urlEscape(message)),
		http.StatusSeeOther)
}

/* redirectHomeError - redirects back to the home page with an error banner. */
func redirectHomeError(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/?error="+urlEscape(message), http.StatusSeeOther)
}

/* urlEscape - minimal query-value escaping for our short error messages. */
func urlEscape(s string) string {
	return strings.ReplaceAll(s, " ", "%20")
}
