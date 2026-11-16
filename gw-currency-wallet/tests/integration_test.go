//go:build integration

// Интеграционные тесты против настоящего PostgreSQL.
//
// Запуск (база должна быть поднята, схема создаётся миграциями автоматически):
//
//	TEST_DATABASE_URL='postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable' \
//	  go test -tags=integration ./...
//
// Без переменной TEST_DATABASE_URL тесты пропускаются (skip), поэтому команду
// можно выполнять и в окружении без базы.

package tests

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/config"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages/postgres"
)

const testDBTimeout = 20 * time.Second

// integrationStorage подключается к базе из TEST_DATABASE_URL и применяет миграции.
func integrationStorage(t *testing.T) *postgres.Connector {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL не задан — интеграционные тесты пропущены")
	}

	store := postgres.New(&config.Config{
		DatabaseDSN:    dsn,
		DBMaxConns:     5,
		DBMinConns:     1,
		MigrationsPath: "../migrations",
	}, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), testDBTimeout)
	defer cancel()

	if err := store.Start(ctx); err != nil {
		t.Fatalf("подключение к базе: %v", err)
	}

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()

		if err := store.Stop(stopCtx); err != nil {
			t.Errorf("остановка коннектора: %v", err)
		}
	})

	return store
}

// uniqueSuffix делает имена уникальными между запусками тестов.
func uniqueSuffix() string { return uuid.NewString()[:8] }

// newUser создаёт пользователя с уникальными именем и email.
func newUser(t *testing.T, store storages.Storage) storages.User {
	t.Helper()

	suffix := uniqueSuffix()

	user, err := store.CreateUser(context.Background(),
		"user-"+suffix, "user-"+suffix+"@example.com", "hash")
	if err != nil {
		t.Fatalf("CreateUser(): %v", err)
	}

	return user
}

// transaction собирает операцию для хранилища.
func transaction(userID uuid.UUID, operation, from, to string, amount, result, rate decimal.Decimal) storages.Transaction {
	return storages.Transaction{
		ID:           uuid.New(),
		UserID:       userID,
		Operation:    operation,
		Status:       storages.StatusSuccess,
		FromCurrency: from,
		ToCurrency:   to,
		Amount:       amount,
		ResultAmount: result,
		Rate:         rate,
		CreatedAt:    time.Now().UTC(),
	}
}

func TestIntegrationUsersAndBalances(t *testing.T) {
	store := integrationStorage(t)
	ctx := context.Background()

	user := newUser(t, store)

	balances, err := store.Balances(ctx, user.ID)
	if err != nil {
		t.Fatalf("Balances(): %v", err)
	}
	if len(balances) != 3 {
		t.Fatalf("баланс по валютам = %d, want 3", len(balances))
	}
	for _, currency := range []string{"USD", "RUB", "EUR"} {
		if amount, ok := balances[currency]; !ok || !amount.IsZero() {
			t.Fatalf("баланс %s = %v, want 0", currency, amount)
		}
	}

	if _, err := store.CreateUser(ctx, user.Username, "other-"+uniqueSuffix()+"@example.com", "hash"); !errors.Is(err, storages.ErrUserExists) {
		t.Fatalf("дубль username: error = %v, want ErrUserExists", err)
	}

	if _, err := store.CreateUser(ctx, "other-"+uniqueSuffix(), user.Email, "hash"); !errors.Is(err, storages.ErrUserExists) {
		t.Fatalf("дубль email: error = %v, want ErrUserExists", err)
	}

	found, err := store.UserByUsername(ctx, user.Username)
	if err != nil {
		t.Fatalf("UserByUsername(): %v", err)
	}
	if found.ID != user.ID {
		t.Fatalf("UserByUsername() вернул %s, want %s", found.ID, user.ID)
	}

	if _, err := store.UserByUsername(ctx, "missing-"+uniqueSuffix()); !errors.Is(err, storages.ErrUserNotFound) {
		t.Fatalf("неизвестный пользователь: error = %v, want ErrUserNotFound", err)
	}
}

func TestIntegrationDepositWithdrawExchange(t *testing.T) {
	store := integrationStorage(t)
	ctx := context.Background()

	user := newUser(t, store)
	zero := decimal.Zero

	balances, err := store.Deposit(ctx, transaction(user.ID, storages.OperationDeposit, "USD", "", decimal.NewFromInt(100), zero, zero))
	if err != nil {
		t.Fatalf("Deposit(): %v", err)
	}
	if !balances["USD"].Equal(decimal.NewFromInt(100)) {
		t.Fatalf("после пополнения USD = %s, want 100", balances["USD"])
	}

	tooMuch := transaction(user.ID, storages.OperationWithdraw, "USD", "", decimal.NewFromInt(150), zero, zero)
	if _, err := store.Withdraw(ctx, tooMuch); !errors.Is(err, storages.ErrInsufficientFunds) {
		t.Fatalf("списание больше баланса: error = %v, want ErrInsufficientFunds", err)
	}

	balances, err = store.Withdraw(ctx, transaction(user.ID, storages.OperationWithdraw, "USD", "", decimal.NewFromInt(40), zero, zero))
	if err != nil {
		t.Fatalf("Withdraw(): %v", err)
	}
	if !balances["USD"].Equal(decimal.NewFromInt(60)) {
		t.Fatalf("после списания USD = %s, want 60", balances["USD"])
	}

	// 1 USD = 0.9 EUR: меняем 60 USD на 54 EUR.
	exchange := transaction(user.ID, storages.OperationExchange, "USD", "EUR",
		decimal.NewFromInt(60), decimal.NewFromInt(54), decimal.RequireFromString("0.9"))

	balances, err = store.Exchange(ctx, exchange)
	if err != nil {
		t.Fatalf("Exchange(): %v", err)
	}
	if !balances["USD"].IsZero() {
		t.Fatalf("после обмена USD = %s, want 0", balances["USD"])
	}
	if !balances["EUR"].Equal(decimal.NewFromInt(54)) {
		t.Fatalf("после обмена EUR = %s, want 54", balances["EUR"])
	}

	emptyExchange := transaction(user.ID, storages.OperationExchange, "USD", "EUR",
		decimal.NewFromInt(10), decimal.NewFromInt(9), decimal.RequireFromString("0.9"))
	if _, err := store.Exchange(ctx, emptyExchange); !errors.Is(err, storages.ErrInsufficientFunds) {
		t.Fatalf("обмен без средств: error = %v, want ErrInsufficientFunds", err)
	}
}

// TestIntegrationConcurrentWithdraw проверяет, что атомарный UPDATE не даёт
// уйти в минус: из 10 параллельных списаний по 20 при балансе 100 проходят ровно 5.
func TestIntegrationConcurrentWithdraw(t *testing.T) {
	store := integrationStorage(t)
	ctx := context.Background()

	user := newUser(t, store)
	zero := decimal.Zero

	if _, err := store.Deposit(ctx, transaction(user.ID, storages.OperationDeposit, "USD", "", decimal.NewFromInt(100), zero, zero)); err != nil {
		t.Fatalf("Deposit(): %v", err)
	}

	const attempts = 10

	var wg sync.WaitGroup
	var succeeded, insufficient, failed int32

	for i := 0; i < attempts; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := store.Withdraw(ctx, transaction(user.ID, storages.OperationWithdraw, "USD", "", decimal.NewFromInt(20), zero, zero))
			switch {
			case err == nil:
				atomic.AddInt32(&succeeded, 1)
			case errors.Is(err, storages.ErrInsufficientFunds):
				atomic.AddInt32(&insufficient, 1)
			default:
				atomic.AddInt32(&failed, 1)
				t.Errorf("неожиданная ошибка списания: %v", err)
			}
		}()
	}

	wg.Wait()

	if failed != 0 {
		t.Fatalf("ошибок, отличных от ErrInsufficientFunds: %d", failed)
	}
	if succeeded != 5 {
		t.Fatalf("успешных списаний %d, want 5", succeeded)
	}
	if insufficient != 5 {
		t.Fatalf("отказов по недостатку средств %d, want 5", insufficient)
	}

	balances, err := store.Balances(ctx, user.ID)
	if err != nil {
		t.Fatalf("Balances(): %v", err)
	}
	if !balances["USD"].IsZero() {
		t.Fatalf("итоговый баланс USD = %s, want 0", balances["USD"])
	}
}
