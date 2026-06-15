package app

import (
	"html/template"
	"net/http"
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

}

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {

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
