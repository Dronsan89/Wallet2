package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/auth"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/rates"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/service"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
)

const testJWTSecret = "test-secret"

// testRates — курсы, с которыми работает тест: 1 USD = 90 RUB, 1 EUR = 100 RUB.
func testRates() rates.Rates {
	return rates.Rates{
		"USD": decimal.NewFromInt(90),
		"RUB": decimal.NewFromInt(1),
		"EUR": decimal.NewFromInt(100),
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeStorage — хранилище в памяти.
type fakeStorage struct {
	mu           sync.Mutex
	users        map[string]storages.User
	balances     map[uuid.UUID]storages.Balances
	transactions []storages.Transaction
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{
		users:    make(map[string]storages.User),
		balances: make(map[uuid.UUID]storages.Balances),
	}
}

func (f *fakeStorage) CreateUser(_ context.Context, username, email, passwordHash string) (storages.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, u := range f.users {
		if u.Username == username || u.Email == email {
			return storages.User{}, storages.ErrUserExists
		}
	}

	user := storages.User{
		ID:           uuid.New(),
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now().UTC(),
	}
	f.users[username] = user

	balances := make(storages.Balances, 3)
	for _, currency := range []string{"USD", "RUB", "EUR"} {
		balances[currency] = decimal.Zero
	}
	f.balances[user.ID] = balances

	return user, nil
}

func (f *fakeStorage) UserByUsername(_ context.Context, username string) (storages.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	user, ok := f.users[username]
	if !ok {
		return storages.User{}, storages.ErrUserNotFound
	}

	return user, nil
}

func (f *fakeStorage) Balances(_ context.Context, userID uuid.UUID) (storages.Balances, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.copyBalances(userID), nil
}

func (f *fakeStorage) Deposit(_ context.Context, tx storages.Transaction) (storages.Balances, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.balances[tx.UserID][tx.FromCurrency] = f.balances[tx.UserID][tx.FromCurrency].Add(tx.Amount)
	f.transactions = append(f.transactions, tx)

	return f.copyBalances(tx.UserID), nil
}

func (f *fakeStorage) Withdraw(_ context.Context, tx storages.Transaction) (storages.Balances, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	current := f.balances[tx.UserID][tx.FromCurrency]
	if current.LessThan(tx.Amount) {
		return nil, storages.ErrInsufficientFunds
	}

	f.balances[tx.UserID][tx.FromCurrency] = current.Sub(tx.Amount)
	f.transactions = append(f.transactions, tx)

	return f.copyBalances(tx.UserID), nil
}

func (f *fakeStorage) Exchange(_ context.Context, tx storages.Transaction) (storages.Balances, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	current := f.balances[tx.UserID][tx.FromCurrency]
	if current.LessThan(tx.Amount) {
		return nil, storages.ErrInsufficientFunds
	}

	f.balances[tx.UserID][tx.FromCurrency] = current.Sub(tx.Amount)
	f.balances[tx.UserID][tx.ToCurrency] = f.balances[tx.UserID][tx.ToCurrency].Add(tx.ResultAmount)
	f.transactions = append(f.transactions, tx)

	return f.copyBalances(tx.UserID), nil
}

func (f *fakeStorage) copyBalances(userID uuid.UUID) storages.Balances {
	out := make(storages.Balances, len(f.balances[userID]))
	for currency, amount := range f.balances[userID] {
		out[currency] = amount
	}

	return out
}

// fakeRates — провайдер курсов с возможностью вернуть ошибку.
type fakeRates struct {
	mu    sync.Mutex
	rates rates.Rates
	err   error
	calls int
}

func (f *fakeRates) Rates(context.Context) (rates.Rates, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if f.err != nil {
		return nil, f.err
	}

	return f.rates, nil
}

func (f *fakeRates) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// fakePublisher — продюсер, который складывает события в канал.
type fakePublisher struct {
	events chan service.Event
	err    error
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{events: make(chan service.Event, 16)}
}

func (p *fakePublisher) Publish(_ context.Context, _ string, payload []byte) error {
	if p.err != nil {
		return p.err
	}

	var event service.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}

	p.events <- event

	return nil
}

// nextEvent ждёт событие из продюсера.
func (p *fakePublisher) nextEvent(t *testing.T) service.Event {
	t.Helper()

	select {
	case event := <-p.events:
		return event
	case <-time.After(time.Second):
		t.Fatal("ожидалось событие в Kafka, но его нет")
		return service.Event{}
	}
}

// expectNoEvent проверяет, что событие не публиковалось.
func (p *fakePublisher) expectNoEvent(t *testing.T) {
	t.Helper()

	select {
	case event := <-p.events:
		t.Fatalf("событие не ожидалось, но получено: %+v", event)
	case <-time.After(100 * time.Millisecond):
	}
}

// testEnv — собранные зависимости для тестов.
type testEnv struct {
	service   *service.Service
	storage   *fakeStorage
	rates     *fakeRates
	publisher *fakePublisher
	auth      *auth.Manager
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	storage := newFakeStorage()
	rateProvider := &fakeRates{rates: testRates()}
	publisher := newFakePublisher()
	authManager := auth.NewManager(testJWTSecret, time.Hour)

	svc := service.New(service.Deps{
		Storage:   storage,
		Rates:     rateProvider,
		Auth:      authManager,
		Publisher: publisher,
		Log:       testLogger(),
		Threshold: decimal.NewFromInt(30000),
	})

	return &testEnv{
		service:   svc,
		storage:   storage,
		rates:     rateProvider,
		publisher: publisher,
		auth:      authManager,
	}
}

// failRatesRateProvider возвращает провайдера, который всегда падает.
var errRatesDown = errors.New("exchanger is down")