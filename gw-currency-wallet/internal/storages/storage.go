// Package storages описывает доступ к данным кошелька (users, balances, transactions).
// Интерфейс изолирует сервисный слой от конкретной БД — требование ТЗ.
package storages

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Ошибки хранилища, которые сервисный слой переводит в доменные.
var (
	ErrUserExists        = errors.New("user already exists")
	ErrUserNotFound      = errors.New("user not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// Storage — операции над пользователями, балансами и транзакциями.
type Storage interface {
	// CreateUser создаёт пользователя с нулевыми балансами по всем валютам.
	CreateUser(ctx context.Context, username, email, passwordHash string) (User, error)
	// UserByUsername возвращает пользователя по имени.
	UserByUsername(ctx context.Context, username string) (User, error)
	// Balances возвращает баланс пользователя по всем поддерживаемым валютам.
	Balances(ctx context.Context, userID uuid.UUID) (Balances, error)
	// Deposit пополняет баланс и записывает транзакцию.
	Deposit(ctx context.Context, tx Transaction) (Balances, error)
	// Withdraw списывает средства и записывает транзакцию.
	Withdraw(ctx context.Context, tx Transaction) (Balances, error)
	// Exchange обменивает одну валюту на другую в одной БД-транзакции.
	Exchange(ctx context.Context, tx Transaction) (Balances, error)
}