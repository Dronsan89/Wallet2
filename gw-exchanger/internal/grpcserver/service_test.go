package grpcserver

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	exchange "github.com/proto-exchange/exchange_grpc/exchange"

	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/storages"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeStorage — хранилище курсов в памяти.
type fakeStorage struct {
	rates storages.Rates
	err   error
}

func (f *fakeStorage) Rates(context.Context) (storages.Rates, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.rates, nil
}

func (f *fakeStorage) Rate(_ context.Context, currency string) (decimal.Decimal, error) {
	if f.err != nil {
		return decimal.Zero, f.err
	}

	value, ok := f.rates[currency]
	if !ok {
		return decimal.Zero, storages.ErrCurrencyNotFound
	}

	return value, nil
}

func newService() *Service {
	return NewService(&fakeStorage{rates: storages.Rates{
		"USD": decimal.NewFromInt(90),
		"EUR": decimal.NewFromInt(100),
		"RUB": decimal.NewFromInt(1),
	}}, testLogger())
}

func almostEqual(t *testing.T, got, want float32) {
	t.Helper()

	if math.Abs(float64(got-want)) > 1e-5 {
		t.Fatalf("rate = %v, want %v", got, want)
	}
}

func TestGetExchangeRates(t *testing.T) {
	resp, err := newService().GetExchangeRates(context.Background(), &exchange.Empty{})
	if err != nil {
		t.Fatalf("GetExchangeRates() error = %v", err)
	}

	rates := resp.GetRates()
	if len(rates) != 3 {
		t.Fatalf("курсов = %d, want 3", len(rates))
	}

	almostEqual(t, rates["USD"], 90)
	almostEqual(t, rates["EUR"], 100)
	almostEqual(t, rates["RUB"], 1)
}

func TestGetExchangeRatesStorageError(t *testing.T) {
	svc := NewService(&fakeStorage{err: errors.New("db is down")}, testLogger())

	_, err := svc.GetExchangeRates(context.Background(), &exchange.Empty{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("code = %v, want Internal", status.Code(err))
	}
}

func TestGetExchangeRateForCurrency(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want float32
	}{
		{name: "USD to EUR", from: "USD", to: "EUR", want: 0.9},
		{name: "USD to RUB", from: "USD", to: "RUB", want: 90},
		{name: "EUR to USD", from: "EUR", to: "USD", want: 1.111111},
		{name: "same currency", from: "RUB", to: "RUB", want: 1},
		{name: "lower case", from: "usd", to: "eur", want: 0.9},
	}

	svc := newService()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.GetExchangeRateForCurrency(context.Background(), &exchange.CurrencyRequest{
				FromCurrency: tt.from,
				ToCurrency:   tt.to,
			})
			if err != nil {
				t.Fatalf("GetExchangeRateForCurrency() error = %v", err)
			}

			almostEqual(t, resp.GetRate(), tt.want)
		})
	}
}

func TestGetExchangeRateForCurrencyInvalid(t *testing.T) {
	svc := newService()

	cases := []struct{ from, to string }{
		{"GBP", "EUR"},
		{"USD", "GBP"},
		{"", "EUR"},
	}

	for _, tt := range cases {
		_, err := svc.GetExchangeRateForCurrency(context.Background(), &exchange.CurrencyRequest{
			FromCurrency: tt.from,
			ToCurrency:   tt.to,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("пара %q/%q: code = %v, want InvalidArgument", tt.from, tt.to, status.Code(err))
		}
	}
}

func TestCrossRateMissingCurrency(t *testing.T) {
	rates := storages.Rates{"USD": decimal.NewFromInt(90)}

	if _, err := crossRate(rates, "USD", "EUR"); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}

	if _, err := crossRate(rates, "USD", "USD"); err != nil {
		t.Fatalf("crossRate() для одинаковых валют вернул ошибку: %v", err)
	}
}