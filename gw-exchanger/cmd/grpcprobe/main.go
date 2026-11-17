// Command grpcprobe — небольшой gRPC-клиент сервиса курсов.
// Заменяет grpcurl и используется как healthcheck в compose.
//
// Примеры:
//
//	grpcprobe -addr localhost:50051 -все курсы даст
//	grpcprobe -addr localhost:50051 -from USD -to EUR
//	grpcprobe -addr localhost:50051 -health
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	exchange "github.com/proto-exchange/exchange_grpc/exchange"
)

func main() {
	addr := flag.String("addr", "localhost:50051", "адрес gRPC-сервера курсов")
	from := flag.String("from", "", "валюта, из которой переводим (например, USD)")
	to := flag.String("to", "", "валюта, в которую переводим (например, EUR)")
	healthCheck := flag.Bool("health", false, "только проверить health-статус сервера")
	timeout := flag.Duration("timeout", 3*time.Second, "таймаут вызова")
	flag.Parse()

	if err := run(*addr, *from, *to, *healthCheck, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "grpcprobe: %v\n", err)
		os.Exit(1)
	}
}

func run(addr, from, to string, healthCheck bool, timeout time.Duration) error {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableServiceConfig(),
	)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if healthCheck {
		resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
		if err != nil {
			return fmt.Errorf("health check: %w", err)
		}
		fmt.Printf("health: %s\n", resp.GetStatus())
		return nil
	}

	client := exchange.NewExchangeServiceClient(conn)

	if from != "" && to != "" {
		resp, err := client.GetExchangeRateForCurrency(ctx, &exchange.CurrencyRequest{
			FromCurrency: from,
			ToCurrency:   to,
		})
		if err != nil {
			return fmt.Errorf("GetExchangeRateForCurrency: %w", err)
		}

		fmt.Printf("%s -> %s: %g\n", resp.GetFromCurrency(), resp.GetToCurrency(), resp.GetRate())
		return nil
	}

	resp, err := client.GetExchangeRates(ctx, &exchange.Empty{})
	if err != nil {
		return fmt.Errorf("GetExchangeRates: %w", err)
	}

	currencies := make([]string, 0, len(resp.GetRates()))
	for currency := range resp.GetRates() {
		currencies = append(currencies, currency)
	}
	sort.Strings(currencies)

	fmt.Println("курсы (рублей за 1 единицу валюты):")
	for _, currency := range currencies {
		fmt.Printf("  %s: %g\n", currency, resp.GetRates()[currency])
	}

	return nil
}
