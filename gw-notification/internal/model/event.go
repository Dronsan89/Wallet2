// Package model содержит модель события о крупной операции.
package model

import (
	"errors"
	"strings"
	"time"
)

// Статусы операции в событии.
const (
	StatusSuccess = "SUCCESS"
	StatusFailed  = "FAILED"
)

// Event — событие из топика wallet.transactions.
// Поля соответствуют тому, что публикует gw-currency-wallet.
type Event struct {
	EventID       string    `bson:"event_id" json:"event_id"`
	TransactionID string    `bson:"transaction_id" json:"transaction_id"`
	UserID        string    `bson:"user_id" json:"user_id"`
	Operation     string    `bson:"operation" json:"operation"`
	Status        string    `bson:"status" json:"status"`
	FromCurrency  string    `bson:"from_currency,omitempty" json:"from_currency,omitempty"`
	ToCurrency    string    `bson:"to_currency,omitempty" json:"to_currency,omitempty"`
	Amount        float64   `bson:"amount" json:"amount"`
	Rate          float64   `bson:"rate,omitempty" json:"rate,omitempty"`
	ResultAmount  float64   `bson:"result_amount,omitempty" json:"result_amount,omitempty"`
	RubEquivalent float64   `bson:"rub_equivalent" json:"rub_equivalent"`
	CreatedAt     time.Time `bson:"created_at" json:"created_at"`
	Source        string    `bson:"source" json:"source"`
	SchemaVersion int       `bson:"schema_version" json:"schema_version"`

	// Поля, которые добавляет сам сервис при получении.
	ReceivedAt time.Time `bson:"received_at" json:"received_at"`
	LatencyMS  int64     `bson:"latency_ms" json:"latency_ms"`
}

// Validate проверяет обязательные поля события.
func (e Event) Validate() error {
	if strings.TrimSpace(e.TransactionID) == "" {
		return errors.New("transaction_id is empty")
	}
	if strings.TrimSpace(e.UserID) == "" {
		return errors.New("user_id is empty")
	}
	if strings.TrimSpace(e.Operation) == "" {
		return errors.New("operation is empty")
	}

	switch e.Status {
	case StatusSuccess, StatusFailed:
	default:
		return errors.New("unknown status: " + e.Status)
	}

	if e.CreatedAt.IsZero() {
		return errors.New("created_at is empty")
	}

	return nil
}

// FillComputed дополняет событие полями, которые считает сервис:
// идентификатор события и задержку от создания до получения.
func (e *Event) FillComputed(receivedAt time.Time) {
	if e.EventID == "" {
		e.EventID = e.TransactionID
	}

	e.ReceivedAt = receivedAt.UTC()

	latency := e.ReceivedAt.Sub(e.CreatedAt)
	if latency < 0 {
		latency = 0
	}
	e.LatencyMS = latency.Milliseconds()
}