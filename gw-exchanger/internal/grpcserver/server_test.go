package grpcserver

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"

	exchange "github.com/proto-exchange/exchange_grpc/exchange"

	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/storages"
)

// TestGRPCServerOverBufconn поднимает настоящий gRPC-сервер на in-memory
// listener (bufconn) и дергает его клиентом — проверка регистрации сервиса,
// интерсепторов и health-статуса без сети и портов.
func TestGRPCServerOverBufconn(t *testing.T) {
	storage := &fakeStorage{rates: storages.Rates{
		"USD": decimal.NewFromInt(90),
		"EUR": decimal.NewFromInt(100),
		"RUB": decimal.NewFromInt(1),
	}}

	lis := bufconn.Listen(1024 * 1024)
	server, healthSrv := NewGRPCServer(storage, testLogger())
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	go func() {
		_ = server.Serve(lis)
	}()
	t.Cleanup(server.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := exchange.NewExchangeServiceClient(conn)

	ratesResp, err := client.GetExchangeRates(ctx, &exchange.Empty{})
	if err != nil {
		t.Fatalf("GetExchangeRates: %v", err)
	}
	if len(ratesResp.GetRates()) != 3 {
		t.Fatalf("курсов = %d, want 3", len(ratesResp.GetRates()))
	}

	rateResp, err := client.GetExchangeRateForCurrency(ctx, &exchange.CurrencyRequest{
		FromCurrency: "USD",
		ToCurrency:   "EUR",
	})
	if err != nil {
		t.Fatalf("GetExchangeRateForCurrency: %v", err)
	}
	if rateResp.GetRate() != float32(0.9) {
		t.Fatalf("rate = %v, want 0.9", rateResp.GetRate())
	}

	healthResp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	if healthResp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health = %v, want SERVING", healthResp.GetStatus())
	}
}
