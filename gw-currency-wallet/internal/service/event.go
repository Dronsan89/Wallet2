package service

import (
	"context"


	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
)

// eventSchemaVersion — версия схемы события.
const eventSchemaVersion = 1

// EventPublisher публикует событие в брокер сообщений.
// Интерфейс объявлен здесь, чтобы сервис не зависел от конкретного клиента Kafka.
type EventPublisher interface {
	Publish(ctx context.Context, key string, payload []byte) error
}

// Event — событие о крупной операции в топике wallet.transactions.
type Event struct {
	EventID        string  `json:"event_id"`
	TransactionID  string  `json:"transaction_id"`
	UserID         string  `json:"user_id"`
	Operation      string  `json:"operation"`
	Status         string  `json:"status"`
	FromCurrency   string  `json:"from_currency,omitempty"`
	ToCurrency     string  `json:"to_currency,omitempty"`
	Amount         float64 `json:"amount"`
	Rate           float64 `json:"rate,omitempty"`
	ResultAmount   float64 `json:"result_amount,omitempty"`
	RubEquivalent  float64 `json:"rub_equivalent"`
	CreatedAt      string  `json:"created_at"`
	Source         string  `json:"source"`
	SchemaVersion  int     `json:"schema_version"`
}

// newEvent собирает событие из записи об операции.
func newEvent(tx storages.Transaction) Event {
	return Event{
		EventID:       tx.ID.String(),
		TransactionID: tx.ID.String(),
		UserID:        tx.UserID.String(),
		Operation:     tx.Operation,
		Status:        tx.Status,
		FromCurrency:  tx.FromCurrency,
		ToCurrency:    tx.ToCurrency,
		Amount:        tx.Amount.InexactFloat64(),
		Rate:          tx.Rate.InexactFloat64(),
		ResultAmount:  tx.ResultAmount.InexactFloat64(),
		RubEquivalent: tx.RubEquivalent.InexactFloat64(),
		CreatedAt:     tx.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		Source:        "gw-currency-wallet",
		SchemaVersion: eventSchemaVersion,
	}
}
