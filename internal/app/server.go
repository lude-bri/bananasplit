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

type Server struct {
	store     ExpenseStore
	templates *template.Template
}

type IndexPageData struct {
	Expenses []Expense
	Summary  MonthlySummary
	Month    string
	Error    string
}

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

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	mux.HandleFunc("GET /", s.handleIndex)
	mux.HandleFunc("POST /expenses", s.handleCreateExpense)
	mux.HandleFunc("POST /expenses/delete", s.handleDeleteExpense)

	return mux
}

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

func filterByMonth(expenses []Expense, month string) []Expense {
	var filtered []Expense
	for _, expense := range expenses {
		if expense.Date.Format("2006-01") == month {
			filtered = append(filtered, expense)
		}
	}

	return filtered
}

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

func formatMoney(cents int64) string {
	return fmt.Sprintf("%d,%02d €", cents/100, cents%100)
}

func formatDate(date time.Time) string {
	return date.Format("02/01/2006")
}

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

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}

	return value
}
