package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/auth"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/rates"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/pkg"
)

// publishTimeout — сколько ждём отправки события в Kafka, не задерживая ответ клиенту.
const publishTimeout = 2 * time.Second

// Deps — зависимости сервиса.
type Deps struct {
	Storage   storages.Storage
	Rates     rates.Provider
	Auth      *auth.Manager
	Publisher EventPublisher
	Log       *slog.Logger
	Threshold decimal.Decimal
	Now       func() time.Time
}

// Service — бизнес-логика кошелька.
type Service struct {
	storage   storages.Storage
	rates     rates.Provider
	auth      *auth.Manager
	publisher EventPublisher
	log       *slog.Logger
	threshold decimal.Decimal
	now       func() time.Time
}

// New создаёт сервис.
func New(deps Deps) *Service {
	s := &Service{
		storage:   deps.Storage,
		rates:     deps.Rates,
		auth:      deps.Auth,
		publisher: deps.Publisher,
		log:       deps.Log,
		threshold: deps.Threshold,
		now:       deps.Now,
	}

	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}

	return s
}

// crossRate считает кросс-курс: сколько единиц to дают за одну единицу from.
func (s *Service) crossRate(ctx context.Context, from, to string) (decimal.Decimal, error) {
	all, err := s.rates.Rates(ctx)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%w: %v", ErrRatesUnavailable, err)
	}

	fromRate, okFrom := all[from]
	toRate, okTo := all[to]
	if !okFrom || !okTo || toRate.IsZero() {
		return decimal.Zero, fmt.Errorf("%w: no rate for %s/%s", ErrRatesUnavailable, from, to)
	}

	return fromRate.Div(toRate), nil
}

// rubEquivalent считает рублёвый эквивалент суммы по актуальному курсу.
// Если курсы недоступны, эквивалент считается нулевым: это не должно ломать операцию.
func (s *Service) rubEquivalent(ctx context.Context, currency string, amount decimal.Decimal) decimal.Decimal {
	if currency == "RUB" {
		return pkg.RoundMoney(amount)
	}

	all, err := s.rates.Rates(ctx)
	if err != nil {
		s.log.Warn("rub equivalent unavailable", "currency", currency, "error", err)
		return decimal.Zero
	}

	rate, ok := all[currency]
	if !ok {
		return decimal.Zero
	}

	return pkg.RoundMoney(amount.Mul(rate))
}

// publishEvent отправляет событие, если операция достаточно крупная.
// Публикация идёт в фоне и не влияет на ответ клиенту.
func (s *Service) publishEvent(ctx context.Context, tx storages.Transaction) {
	if s.publisher == nil || tx.RubEquivalent.LessThan(s.threshold) {
		return
	}

	event := newEvent(tx)
	payload, err := json.Marshal(event)
	if err != nil {
		s.log.Error("marshal transaction event", "transaction_id", tx.ID.String(), "error", err)
		return
	}

	go func() {
		pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
		defer cancel()

		if err := s.publisher.Publish(pubCtx, event.UserID, payload); err != nil {
			s.log.Error("publish transaction event",
				"transaction_id", event.TransactionID,
				"rub_equivalent", event.RubEquivalent,
				"error", err,
			)
			return
		}

		s.log.Info("transaction event published",
			"transaction_id", event.TransactionID,
			"rub_equivalent", event.RubEquivalent,
		)
	}()
}

// validateAmount проверяет сумму операции.
func validateAmount(amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) || amount.GreaterThan(maxOperationAmount) {
		return ErrInvalidInput
	}
	return nil
}

// validateRegistration проверяет данные при регистрации.
func validateRegistration(username, email, password string) error {
	if len([]rune(username)) < 3 || len([]rune(username)) > 32 {
		return ErrInvalidInput
	}
	if strings.ContainsAny(username, " \t\n") {
		return ErrInvalidInput
	}
	if len([]rune(password)) < 6 {
		return ErrInvalidInput
	}
	if !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return ErrInvalidInput
	}

	return nil
}

// maxOperationAmount — верхняя граница суммы операции (защита от опечаток).
var maxOperationAmount = decimal.NewFromInt(1_000_000_000)

