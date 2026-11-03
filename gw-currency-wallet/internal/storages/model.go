package storages

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Операции над кошельком.
const (
	OperationDeposit  = "DEPOSIT"
	OperationWithdraw = "WITHDRAW"
	OperationExchange = "EXCHANGE"
)

// Статусы операций.
const (
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
)

// User — пользователь сервиса.
type User struct {
	ID           uuid.UUID
	Username     string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// Balances — суммы по валютам (USD, RUB, EUR).
type Balances map[string]decimal.Decimal

// Transaction — запись об операции, попавшая в историю.
type Transaction struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Operation     string
	Status        string
	FromCurrency  string
	ToCurrency    string
	Amount        decimal.Decimal
	ResultAmount  decimal.Decimal
	Rate          decimal.Decimal
	RubEquivalent decimal.Decimal
	CreatedAt     time.Time
}