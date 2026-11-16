// Package grpcserver содержит gRPC-сервер сервиса курсов.
package grpcserver

import (
	"context"
	"log/slog"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	exchange "github.com/proto-exchange/exchange_grpc/exchange"

	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/storages"
	"github.com/Dronsan89/Wallet2/gw-exchanger/pkg"
)

// Service реализует exchange.ExchangeServiceServer.
type Service struct {
	exchange.UnimplementedExchangeServiceServer

	storage storages.Storage
	log     *slog.Logger
}

var _ exchange.ExchangeServiceServer = (*Service)(nil)

// NewService создаёт обработчик gRPC-методов.
func NewService(storage storages.Storage, log *slog.Logger) *Service {
	return &Service{storage: storage, log: log}
}

// GetExchangeRates отдаёт курсы всех валют: сколько рублей стоит одна единица.
func (s *Service) GetExchangeRates(ctx context.Context, _ *exchange.Empty) (*exchange.ExchangeRatesResponse, error) {
	rates, err := s.storage.Rates(ctx)
	if err != nil {
		s.log.Error("get exchange rates", "error", err)
		return nil, status.Error(codes.Internal, "failed to get exchange rates")
	}

	out := make(map[string]float32, len(rates))
	for currency, value := range rates {
		out[currency] = float32(pkg.RoundRate(value).InexactFloat64())
	}

	return &exchange.ExchangeRatesResponse{Rates: out}, nil
}

// GetExchangeRateForCurrency отдаёт кросс-курс пары валют.
func (s *Service) GetExchangeRateForCurrency(ctx context.Context, req *exchange.CurrencyRequest) (*exchange.ExchangeRateResponse, error) {
	from := pkg.NormalizeCurrency(req.GetFromCurrency())
	to := pkg.NormalizeCurrency(req.GetToCurrency())

	if !pkg.IsSupportedCurrency(from) || !pkg.IsSupportedCurrency(to) {
		return nil, status.Errorf(codes.InvalidArgument, "unsupported currency pair: %s/%s", from, to)
	}

	rates, err := s.storage.Rates(ctx)
	if err != nil {
		s.log.Error("get exchange rate", "from", from, "to", to, "error", err)
		return nil, status.Error(codes.Internal, "failed to get exchange rate")
	}

	rate, err := crossRate(rates, from, to)
	if err != nil {
		return nil, err
	}

	return &exchange.ExchangeRateResponse{
		FromCurrency: from,
		ToCurrency:   to,
		Rate:         float32(rate.InexactFloat64()),
	}, nil
}

// crossRate считает кросс-курс: сколько единиц to дают за одну единицу from.
// Курсы в хранилище заданы как «рублей за единицу валюты», поэтому
// rate(from→to) = rub(from) / rub(to).
func crossRate(rates storages.Rates, from, to string) (decimal.Decimal, error) {
	fromRate, okFrom := rates[from]
	toRate, okTo := rates[to]

	if !okFrom || !okTo || toRate.IsZero() {
		return decimal.Zero, status.Errorf(codes.InvalidArgument, "no rate for %s/%s", from, to)
	}

	return pkg.RoundRate(fromRate.Div(toRate)), nil
}