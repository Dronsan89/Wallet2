package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/rates"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/pkg"
)

// Rates отдаёт актуальные курсы валют (из кэша или от сервиса курсов).
func (s *Service) Rates(ctx context.Context) (rates.Rates, error) {
	all, err := s.rates.Rates(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRatesUnavailable, err)
	}

	return all, nil
}

// Exchange обменивает одну валюту на другую по кросс-курсу.
func (s *Service) Exchange(
	ctx context.Context,
	userID uuid.UUID,
	fromCurrency, toCurrency string,
	amount decimal.Decimal,
) (decimal.Decimal, storages.Balances, error) {
	fromCurrency = pkg.NormalizeCurrency(fromCurrency)
	toCurrency = pkg.NormalizeCurrency(toCurrency)
	amount = pkg.RoundMoney(amount)

	if err := validateAmount(amount); err != nil {
		return decimal.Zero, nil, err
	}
	if !pkg.IsSupportedCurrency(fromCurrency) || !pkg.IsSupportedCurrency(toCurrency) || fromCurrency == toCurrency {
		return decimal.Zero, nil, ErrInvalidInput
	}

	rate, err := s.crossRate(ctx, fromCurrency, toCurrency)
	if err != nil {
		return decimal.Zero, nil, err
	}

	resultAmount := pkg.RoundMoney(amount.Mul(rate))
	if resultAmount.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, nil, ErrInvalidInput
	}

	tx := storages.Transaction{
		ID:            uuid.New(),
		UserID:        userID,
		Operation:     storages.OperationExchange,
		Status:        storages.StatusSuccess,
		FromCurrency:  fromCurrency,
		ToCurrency:    toCurrency,
		Amount:        amount,
		ResultAmount:  resultAmount,
		Rate:          rate.Round(8),
		RubEquivalent: s.rubEquivalent(ctx, fromCurrency, amount),
		CreatedAt:     s.now(),
	}

	balances, err := s.storage.Exchange(ctx, tx)
	if err != nil {
		if errors.Is(err, storages.ErrInsufficientFunds) {
			tx.Status = storages.StatusFailed
			s.publishEvent(ctx, tx)

			s.log.Warn("exchange rejected",
				"user_id", userID.String(),
				"from", fromCurrency,
				"to", toCurrency,
				"amount", amount.String(),
			)

			return decimal.Zero, nil, ErrInsufficientFunds
		}
		return decimal.Zero, nil, fmt.Errorf("exchange: %w", err)
	}

	s.log.Info("exchange completed",
		"user_id", userID.String(),
		"transaction_id", tx.ID.String(),
		"from", fromCurrency,
		"to", toCurrency,
		"amount", amount.String(),
		"result_amount", resultAmount.String(),
		"rate", tx.Rate.String(),
	)

	s.publishEvent(ctx, tx)

	return resultAmount, balances, nil
}
