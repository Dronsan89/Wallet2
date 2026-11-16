// Package model содержит модель события и структуры аналитических ответов.
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
type Event struct {
	EventID       string    `json:"event_id"`
	TransactionID string    `json:"transaction_id"`
	UserID        string    `json:"user_id"`
	Operation     string    `json:"operation"`
	Status        string    `json:"status"`
	FromCurrency  string    `json:"from_currency,omitempty"`
	ToCurrency    string    `json:"to_currency,omitempty"`
	Amount        float64   `json:"amount"`
	Rate          float64   `json:"rate,omitempty"`
	ResultAmount  float64   `json:"result_amount,omitempty"`
	RubEquivalent float64   `json:"rub_equivalent"`
	CreatedAt     time.Time `json:"created_at"`
	Source        string    `json:"source"`
	SchemaVersion int       `json:"schema_version"`

	// Поля, которые добавляет сервис при получении.
	ReceivedAt time.Time `json:"received_at"`
	LatencyMS  int64     `json:"latency_ms"`
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

// FillComputed дополняет событие идентификатором и задержкой доставки.
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

// LatencyStats — статистика задержки доставки.
type LatencyStats struct {
	Avg float64 `json:"avg_ms"`
	P95 float64 `json:"p95_ms"`
	Max float64 `json:"max_ms"`
}

// Summary — сводные метрики за период.
type Summary struct {
	Period      string           `json:"period"`
	From        time.Time        `json:"from"`
	To          time.Time        `json:"to"`
	Total       int64            `json:"total"`
	Failed      int64            `json:"failed"`
	ErrorRate   float64          `json:"error_rate"`
	ByOperation map[string]int64 `json:"by_operation"`
	ByStatus    map[string]int64 `json:"by_status"`
	Latency     LatencyStats     `json:"latency"`
}

// SeriesPoint — одна точка временного ряда.
type SeriesPoint struct {
	Bucket       time.Time `json:"bucket"`
	Total        int64     `json:"total"`
	Failed       int64     `json:"failed"`
	LatencyAvgMS float64   `json:"latency_avg_ms"`
}

// TimeSeries — метрики в бакетах за период.
type TimeSeries struct {
	Period string        `json:"period"`
	Step   string        `json:"step"`
	From   time.Time     `json:"from"`
	To     time.Time     `json:"to"`
	Points []SeriesPoint `json:"points"`
}