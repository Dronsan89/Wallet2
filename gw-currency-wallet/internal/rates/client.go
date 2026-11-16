// Package rates получает курсы валют у сервиса курсов по gRPC и кэширует их.
package rates

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	exchange "github.com/proto-exchange/exchange_grpc/exchange"
)

// Rates — курсы валют: сколько рублей стоит одна единица валюты (RUB = 1).
type Rates map[string]decimal.Decimal

// Provider — источник курсов: gRPC-клиент, кэш или тестовая реализация.
type Provider interface {
	Rates(ctx context.Context) (Rates, error)
}

// Client — gRPC-клиент сервиса курсов.
type Client struct {
	conn    *grpc.ClientConn
	api     exchange.ExchangeServiceClient
	timeout time.Duration
}

// NewClient создаёт клиента. Соединение устанавливается лениво, при первом запросе,
// поэтому недоступность сервиса курсов не мешает запуску кошелька.
func NewClient(addr string, timeout time.Duration) (*Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("create grpc client: %w", err)
	}

	return NewClientWithConn(conn, timeout), nil
}

// NewClientWithConn создаёт клиента поверх готового gRPC-соединения.
// Используется в тестах (bufconn), чтобы проверить обмен по gRPC без сети.
func NewClientWithConn(conn *grpc.ClientConn, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	return &Client{
		conn:    conn,
		api:     exchange.NewExchangeServiceClient(conn),
		timeout: timeout,
	}
}

// Rates запрашивает курсы всех валют.
func (c *Client) Rates(ctx context.Context) (Rates, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	resp, err := c.api.GetExchangeRates(ctx, &exchange.Empty{})
	if err != nil {
		return nil, fmt.Errorf("get exchange rates: %w", err)
	}

	rates := make(Rates, len(resp.GetRates()))
	for code, value := range resp.GetRates() {
		rates[code] = decimal.NewFromFloat(float64(value))
	}

	return rates, nil
}

// Start ничего не делает: соединение открывается при первом запросе.
func (c *Client) Start(context.Context) error { return nil }

// Stop закрывает gRPC-соединение.
func (c *Client) Stop(context.Context) error { return c.conn.Close() }