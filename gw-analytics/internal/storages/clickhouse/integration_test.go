//go:build integration

// Интеграционные тесты против настоящего ClickHouse.
//
// Запуск:
//
//	TEST_CLICKHOUSE_ADDR='localhost:9000' go test -tags=integration ./internal/storages/clickhouse
//
// Без переменной TEST_CLICKHOUSE_ADDR тесты пропускаются. Каждый прогон создаёт
// отдельную базу (суффикс из UUID) и удаляет её после себя.

package clickhouse

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Dronsan89/Wallet2/gw-analytics/internal/config"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func integrationConnector(t *testing.T) *Connector {
	t.Helper()

	addr := os.Getenv("TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("TEST_CLICKHOUSE_ADDR не задан — интеграционные тесты пропущены")
	}

	cfg := &config.Config{
		ClickHouseAddr:       addr,
		ClickHouseDB:         "analytics_test_" + strings.ToLower(uuid.NewString()[:8]),
		ClickHouseUser:       envOr("TEST_CLICKHOUSE_USER", "default"),
		ClickHousePassword:   os.Getenv("TEST_CLICKHOUSE_PASSWORD"),
		ClickHouseTimeout:    5 * time.Second,
		ClickHouseMigrations: "../../../migrations/clickhouse",
	}

	connector := New(cfg, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := connector.Start(ctx); err != nil {
		t.Fatalf("Start(): %v", err)
	}

	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()

		_ = connector.conn.Exec(dropCtx, "DROP DATABASE IF EXISTS "+quoteIdent(cfg.ClickHouseDB))
		_ = connector.Stop(dropCtx)
	})

	return connector
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

// event собирает событие с заданным статусом и временем создания.
func event(id, status string, createdAt time.Time) model.Event {
	e := model.Event{
		EventID:       id,
		TransactionID: id,
		UserID:        "user-1",
		Operation:     "DEPOSIT",
		Status:        status,
		FromCurrency:  "USD",
		Amount:        1000,
		RubEquivalent: 90000,
		CreatedAt:     createdAt.UTC(),
		Source:        "gw-currency-wallet",
		SchemaVersion: 1,
	}
	e.FillComputed(time.Now().UTC())

	return e
}

func TestIntegrationInsertAndSummary(t *testing.T) {
	connector := integrationConnector(t)
	ctx := context.Background()

	now := time.Now().UTC()

	events := []model.Event{
		event("tx-1", model.StatusSuccess, now.Add(-30*time.Second)),
		event("tx-2", model.StatusFailed, now.Add(-20*time.Second)),
	}

	inserted, err := connector.InsertBatch(ctx, events)
	if err != nil {
		t.Fatalf("InsertBatch(): %v", err)
	}
	if inserted != 2 {
		t.Fatalf("inserted = %d, want 2", inserted)
	}

	summary, err := connector.Summary(ctx, now.Add(-5*time.Minute), now)
	if err != nil {
		t.Fatalf("Summary(): %v", err)
	}

	if summary.Total != 2 || summary.Failed != 1 {
		t.Fatalf("summary = %+v, want total=2 failed=1", summary)
	}
	if summary.ErrorRate != 0.5 {
		t.Fatalf("error_rate = %v, want 0.5", summary.ErrorRate)
	}
	if summary.ByOperation["DEPOSIT"] != 2 {
		t.Fatalf("by_operation = %v", summary.ByOperation)
	}
	if summary.ByStatus[model.StatusFailed] != 1 {
		t.Fatalf("by_status = %v", summary.ByStatus)
	}

	points, err := connector.TimeSeries(ctx, now.Add(-5*time.Minute), now, time.Minute)
	if err != nil {
		t.Fatalf("TimeSeries(): %v", err)
	}
	if len(points) == 0 {
		t.Fatal("timeseries пустой")
	}
	if points[0].Total != 2 {
		t.Fatalf("первая точка: %+v", points[0])
	}
}

func TestIntegrationDeduplication(t *testing.T) {
	connector := integrationConnector(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// То же событие дважды: ReplacingMergeTree + FINAL должны схлопнуть его в одну строку.
	first := event("tx-dup", model.StatusSuccess, now.Add(-time.Minute))
	if _, err := connector.InsertBatch(ctx, []model.Event{first}); err != nil {
		t.Fatalf("InsertBatch #1: %v", err)
	}

	second := first
	second.ReceivedAt = now
	result, err := connector.InsertBatch(ctx, []model.Event{second})
	if err != nil {
		t.Fatalf("InsertBatch #2: %v", err)
	}
	if result != 1 {
		t.Fatalf("inserted = %d, want 1", result)
	}

	summary, err := connector.Summary(ctx, now.Add(-5*time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Summary(): %v", err)
	}
	if summary.Total != 1 {
		t.Fatalf("total = %d, want 1 (дубль должен схлопнуться FINAL)", summary.Total)
	}
}