package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/service"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
)

func TestRegisterAndLogin(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if err := env.service.Register(ctx, "alice", "secret123", "Alice@Example.com"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := env.service.Register(ctx, "alice", "secret123", "other@example.com"); !errors.Is(err, service.ErrUserExists) {
		t.Fatalf("повторная регистрация: error = %v, want ErrUserExists", err)
	}

	if err := env.service.Register(ctx, "bo", "secret123", "bo@example.com"); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("короткий логин: error = %v, want ErrInvalidInput", err)
	}

	if err := env.service.Register(ctx, "bob", "123", "bob@example.com"); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("короткий пароль: error = %v, want ErrInvalidInput", err)
	}

	token, err := env.service.Login(ctx, "alice", "secret123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token == "" {
		t.Fatal("Login() вернул пустой токен")
	}

	if _, err := env.service.Login(ctx, "alice", "wrong"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("неверный пароль: error = %v, want ErrInvalidCredentials", err)
	}

	if _, err := env.service.Login(ctx, "nobody", "secret123"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Fatalf("неизвестный пользователь: error = %v, want ErrInvalidCredentials", err)
	}
}

func TestDepositAndWithdraw(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if err := env.service.Register(ctx, "alice", "secret123", "alice@example.com"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	user, err := env.storage.UserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("UserByUsername() error = %v", err)
	}

	balances, err := env.service.Deposit(ctx, user.ID, "usd", decimal.NewFromInt(100))
	if err != nil {
		t.Fatalf("Deposit() error = %v", err)
	}
	if got := balances["USD"]; !got.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("баланс USD = %s, want 100", got)
	}

	if _, err := env.service.Deposit(ctx, user.ID, "GBP", decimal.NewFromInt(10)); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("неподдерживаемая валюта: error = %v, want ErrInvalidInput", err)
	}
	if _, err := env.service.Deposit(ctx, user.ID, "USD", decimal.NewFromInt(-5)); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("отрицательная сумма: error = %v, want ErrInvalidInput", err)
	}

	balances, err = env.service.Withdraw(ctx, user.ID, "USD", decimal.NewFromInt(40))
	if err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
	if got := balances["USD"]; !got.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("баланс USD = %s, want 60", got)
	}

	if _, err := env.service.Withdraw(ctx, user.ID, "USD", decimal.NewFromInt(1000)); !errors.Is(err, service.ErrInsufficientFunds) {
		t.Fatalf("недостаточно средств: error = %v, want ErrInsufficientFunds", err)
	}
}

func TestExchange(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if err := env.service.Register(ctx, "alice", "secret123", "alice@example.com"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	user, err := env.storage.UserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("UserByUsername() error = %v", err)
	}

	if _, err := env.service.Deposit(ctx, user.ID, "USD", decimal.NewFromInt(100)); err != nil {
		t.Fatalf("Deposit() error = %v", err)
	}

	// 1 USD = 90 RUB, 1 EUR = 100 RUB => 1 USD = 0.9 EUR.
	exchanged, balances, err := env.service.Exchange(ctx, user.ID, "USD", "EUR", decimal.NewFromInt(10))
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if !exchanged.Equal(decimal.NewFromInt(9)) {
		t.Fatalf("exchanged = %s, want 9", exchanged)
	}
	if !balances["EUR"].Equal(decimal.NewFromInt(9)) {
		t.Fatalf("баланс EUR = %s, want 9", balances["EUR"])
	}
	if !balances["USD"].Equal(decimal.NewFromInt(90)) {
		t.Fatalf("баланс USD = %s, want 90", balances["USD"])
	}

	if _, _, err := env.service.Exchange(ctx, user.ID, "USD", "USD", decimal.NewFromInt(1)); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("обмен в ту же валюту: error = %v, want ErrInvalidInput", err)
	}
	if _, _, err := env.service.Exchange(ctx, user.ID, "USD", "EUR", decimal.NewFromInt(1000)); !errors.Is(err, service.ErrInsufficientFunds) {
		t.Fatalf("обмен без средств: error = %v, want ErrInsufficientFunds", err)
	}
}

func TestExchangeWithoutRates(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	env.rates.err = errRatesDown

	if err := env.service.Register(ctx, "alice", "secret123", "alice@example.com"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	user, err := env.storage.UserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("UserByUsername() error = %v", err)
	}

	if _, _, err := env.service.Exchange(ctx, user.ID, "USD", "EUR", decimal.NewFromInt(10)); !errors.Is(err, service.ErrRatesUnavailable) {
		t.Fatalf("exchange без курсов: error = %v, want ErrRatesUnavailable", err)
	}

	if _, err := env.service.Rates(ctx); !errors.Is(err, service.ErrRatesUnavailable) {
		t.Fatalf("Rates(): error = %v, want ErrRatesUnavailable", err)
	}
}

func TestKafkaEventOnlyForBigOperations(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if err := env.service.Register(ctx, "alice", "secret123", "alice@example.com"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	user, err := env.storage.UserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("UserByUsername() error = %v", err)
	}

	// 10 USD = 900 RUB — ниже порога 30 000.
	if _, err := env.service.Deposit(ctx, user.ID, "USD", decimal.NewFromInt(10)); err != nil {
		t.Fatalf("Deposit() error = %v", err)
	}
	env.publisher.expectNoEvent(t)

	// 1000 USD = 90 000 RUB — выше порога.
	if _, err := env.service.Deposit(ctx, user.ID, "USD", decimal.NewFromInt(1000)); err != nil {
		t.Fatalf("Deposit() error = %v", err)
	}

	event := env.publisher.nextEvent(t)
	if event.Operation != storages.OperationDeposit {
		t.Fatalf("operation = %q, want %q", event.Operation, storages.OperationDeposit)
	}
	if event.Status != storages.StatusSuccess {
		t.Fatalf("status = %q, want %q", event.Status, storages.StatusSuccess)
	}
	if event.UserID != user.ID.String() {
		t.Fatalf("user_id = %q, want %q", event.UserID, user.ID.String())
	}
	if event.RubEquivalent < 90000 {
		t.Fatalf("rub_equivalent = %v, want >= 90000", event.RubEquivalent)
	}
}

func TestFailedWithdrawPublishesFailedEvent(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if err := env.service.Register(ctx, "alice", "secret123", "alice@example.com"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	user, err := env.storage.UserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("UserByUsername() error = %v", err)
	}

	if _, err := env.service.Withdraw(ctx, user.ID, "USD", decimal.NewFromInt(1000)); !errors.Is(err, service.ErrInsufficientFunds) {
		t.Fatalf("Withdraw() error = %v, want ErrInsufficientFunds", err)
	}

	event := env.publisher.nextEvent(t)
	if event.Status != storages.StatusFailed {
		t.Fatalf("status = %q, want %q", event.Status, storages.StatusFailed)
	}
	if event.Operation != storages.OperationWithdraw {
		t.Fatalf("operation = %q, want %q", event.Operation, storages.OperationWithdraw)
	}
}

func TestRateProviderIsCalled(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	if _, err := env.service.Rates(ctx); err != nil {
		t.Fatalf("Rates() error = %v", err)
	}
	if env.rates.Calls() != 1 {
		t.Fatalf("провайдер курсов вызван %d раз, want 1", env.rates.Calls())
	}
}