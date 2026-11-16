// Package storages описывает доступ к курсам валют.
// Интерфейс изолирует gRPC-слой от конкретной БД — требование ТЗ.
package storages

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"
)

// ErrCurrencyNotFound — курса для валюты нет в хранилище.
var ErrCurrencyNotFound = errors.New("currency not found")

// Storage — источник курсов валют.
type Storage interface {
	// Rates возвращает курсы всех валют, которые есть в хранилище.
	Rates(ctx context.Context) (Rates, error)
	// Rate возвращает курс одной валюты к рублю.
	Rate(ctx context.Context, currency string) (decimal.Decimal, error)
}