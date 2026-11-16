package rates

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	exchange "github.com/proto-exchange/exchange_grpc/exchange"
)

// stubExchange — сервис курсов, который отвечает фиксированными курсами
// и считает количество вызовов. Поднимается на bufconn (без сети и портов).
type stubExchange struct {
	exchange.UnimplementedExchangeServiceServer

	calls atomic.Int64
}

func (s *stubExchange) GetExchangeRates(context.Context, *exchange.Empty) (*exchange.ExchangeRatesResponse, error) {
	s.calls.Add(1)

	return &exchange.ExchangeRatesResponse{Rates: map[string]float32{
		"USD": 90,
		"RUB": 1,
		"EUR": 100,
	}}, nil
}

// newBufconnClient поднимает gRPC-сервер курсов на bufconn и возвращает
// клиента кошелька, подключённого к нему.
func newBufconnClient(t *testing.T, stub *stubExchange) *Client {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	exchange.RegisterExchangeServiceServer(server, stub)

	go func() {
		_ = server.Serve(lis)
	}()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("подключение к bufconn-серверу: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return NewClientWithConn(conn, time.Second)
}

// TestClientRatesOverGRPC — главный сценарий «кошелёк ↔ курсы по gRPC»
// без базы данных: настоящий gRPC-вызов по сгенерированным стабам.
func TestClientRatesOverGRPC(t *testing.T) {
	stub := &stubExchange{}
	client := newBufconnClient(t, stub)

	all, err := client.Rates(context.Background())
	if err != nil {
		t.Fatalf("Rates() error = %v", err)
	}

	if got := all["USD"].InexactFloat64(); got != 90 {
		t.Fatalf("USD = %v, want 90", got)
	}
	if got := all["EUR"].InexactFloat64(); got != 100 {
		t.Fatalf("EUR = %v, want 100", got)
	}
	if got := all["RUB"].InexactFloat64(); got != 1 {
		t.Fatalf("RUB = %v, want 1", got)
	}
	if stub.calls.Load() != 1 {
		t.Fatalf("сервис курсов вызван %d раз, want 1", stub.calls.Load())
	}
}

// TestCacheOverGRPCClientServesSecondCallFromMemory — требование ТЗ:
// если курсы запрашивали недавно, обмен берёт их из кэша.
func TestCacheOverGRPCClientServesSecondCallFromMemory(t *testing.T) {
	stub := &stubExchange{}
	client := newBufconnClient(t, stub)
	cache := NewCache(client, time.Minute, nil)

	for i := 0; i < 3; i++ {
		if _, err := cache.Rates(context.Background()); err != nil {
			t.Fatalf("Rates() error = %v", err)
		}
	}

	if got := stub.calls.Load(); got != 1 {
		t.Fatalf("сервис курсов вызван %d раз, want 1 (остальное — из кэша)", got)
	}

	hits, misses := cache.Stats()
	if hits != 2 || misses != 1 {
		t.Fatalf("stats = (%d, %d), want (2, 1)", hits, misses)
	}
}
