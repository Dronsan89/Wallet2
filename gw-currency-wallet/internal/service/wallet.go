package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/pkg"
)

// Balance возвращает баланс пользователя по всем валютам.
func (s *Service) Balance(ctx context.Context, userID uuid.UUID) (storages.Balances, error) {
	balances, err := s.storage.Balances(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get balances: %w", err)
	}

	return balances, nil
}

// Deposit пополняет баланс в указанной валюте.
func (s *Service) Deposit(ctx context.Context, userID uuid.UUID, currency string, amount decimal.Decimal) (storages.Balances, error) {
	currency = pkg.NormalizeCurrency(currency)
	amount = pkg.RoundMoney(amount)

	if err := validateAmount(amount); err != nil {
		return nil, err
	}
	if !pkg.IsSupportedCurrency(currency) {
		return nil, ErrInvalidInput
	}

	tx := storages.Transaction{
		ID:            uuid.New(),
		UserID:        userID,
		Operation:     storages.OperationDeposit,
		Status:        storages.StatusSuccess,
		FromCurrency:  currency,
		Amount:        amount,
		RubEquivalent: s.rubEquivalent(ctx, currency, amount),
		CreatedAt:     s.now(),
	}

	balances, err := s.storage.Deposit(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("deposit: %w", err)
	}

	s.log.Info("deposit completed",
		"user_id", userID.String(),
		"transaction_id", tx.ID.String(),
		"currency", currency,
		"amount", amount.String(),
	)

	s.publishEvent(ctx, tx)

	return balances, nil
}

// Withdraw списывает средства с баланса.
func (s *Service) Withdraw(ctx context.Context, userID uuid.UUID, currency string, amount decimal.Decimal) (storages.Balances, error) {
	currency = pkg.NormalizeCurrency(currency)
	amount = pkg.RoundMoney(amount)

	if err := validateAmount(amount); err != nil {
		return nil, err
	}
	if !pkg.IsSupportedCurrency(currency) {
		return nil, ErrInvalidInput
	}

	tx := storages.Transaction{
		ID:            uuid.New(),
		UserID:        userID,
		Operation:     storages.OperationWithdraw,
		Status:        storages.StatusSuccess,
		FromCurrency:  currency,
		Amount:        amount,
		RubEquivalent: s.rubEquivalent(ctx, currency, amount),
		CreatedAt:     s.now(),
	}

	balances, err := s.storage.Withdraw(ctx, tx)
	if err != nil {
		if errors.Is(err, storages.ErrInsufficientFunds) {
			tx.Status = storages.StatusFailed
			s.publishEvent(ctx, tx)

			s.log.Warn("withdraw rejected",
				"user_id", userID.String(),
				"currency", currency,
				"amount", amount.String(),
			)

			return nil, ErrInsufficientFunds
		}
		return nil, fmt.Errorf("withdraw: %w", err)
	}

	s.log.Info("withdraw completed",
		"user_id", userID.String(),
		"transaction_id", tx.ID.String(),
		"currency", currency,
		"amount", amount.String(),
	)

	s.publishEvent(ctx, tx)

	return balances, nil
}