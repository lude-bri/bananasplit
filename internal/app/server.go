package app

import (
	"html/template"
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
