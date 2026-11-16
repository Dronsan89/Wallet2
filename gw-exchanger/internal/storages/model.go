package storages

import (
	"time"

	"github.com/shopspring/decimal"
)

// Коды поддерживаемых валют.
const (
	CurrencyUSD = "USD"
	CurrencyRUB = "RUB"
	CurrencyEUR = "EUR"
)

// Rates — курсы валют: сколько рублей стоит одна единица валюты (RUB = 1).
type Rates map[string]decimal.Decimal

// CurrencyRate — запись о курсе валюты в хранилище.
type CurrencyRate struct {
	Currency   string
	RubPerUnit decimal.Decimal
	UpdatedAt  time.Time
}