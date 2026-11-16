//go:build integration

// Интеграционные тесты против настоящей MongoDB.
//
// Запуск:
//
//	TEST_MONGO_URI='mongodb://localhost:27017' go test -tags=integration ./internal/storages/mongo
//
// Без переменной TEST_MONGO_URI тесты пропускаются. Каждый прогон использует
// отдельную базу (суффикс из UUID), поэтому данные не пересекаются.

package mongo

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"

	"github.com/Dronsan89/Wallet2/gw-notification/internal/config"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// integrationConnector подключается к MongoDB и удаляет свою базу после теста.
func integrationConnector(t *testing.T) (*Connector, *config.Config) {
	t.Helper()

	uri := os.Getenv("TEST_MONGO_URI")
	if uri == "" {
		t.Skip("TEST_MONGO_URI не задан — интеграционные тесты пропущены")
	}

	cfg := &config.Config{
		MongoURI:            uri,
		MongoDB:             "notification_test_" + uuid.NewString()[:8],
		MongoCollection:     "transfers",
		MongoConnectTimeout: 5 * time.Second,
	}

	connector := New(cfg, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := connector.Start(ctx); err != nil {
		t.Fatalf("Start(): %v", err)
	}

	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dropCancel()

		_ = connector.client.Database(cfg.MongoDB).Drop(dropCtx)
		_ = connector.Stop(dropCtx)
	})

	return connector, cfg
}

// event собирает валидное событие о крупном переводе.
func event(transactionID string) model.Event {
	createdAt := time.Now().UTC().Add(-250 * time.Millisecond)

	e := model.Event{
		EventID:       transactionID,
		TransactionID: transactionID,
		UserID:        "user-" + transactionID,
		Operation:     "DEPOSIT",
		Status:        model.StatusSuccess,
		FromCurrency:  "USD",
		Amount:        1000,
		RubEquivalent: 90000,
		CreatedAt:     createdAt,
		Source:        "gw-currency-wallet",
		SchemaVersion: 1,
	}
	e.FillComputed(time.Now().UTC())

	return e
}

func TestIntegrationIndexesCreated(t *testing.T) {
	connector, _ := integrationConnector(t)

	cursor, err := connector.collection.Indexes().List(context.Background())
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	defer cursor.Close(context.Background())

	names := make(map[string]bool)
	for cursor.Next(context.Background()) {
		var index struct {
			Name string `bson:"name"`
		}
		if err := cursor.Decode(&index); err != nil {
			t.Fatalf("decode index: %v", err)
		}
		names[index.Name] = true
	}

	if !names["uniq_transaction_id"] {
		t.Fatalf("нет уникального индекса uniq_transaction_id, есть: %v", names)
	}
	if !names["user_created"] {
		t.Fatalf("нет индекса user_created, есть: %v", names)
	}
}

func TestIntegrationInsertAndIdempotency(t *testing.T) {
	connector, cfg := integrationConnector(t)
	ctx := context.Background()

	first := []model.Event{event("tx-1"), event("tx-2")}

	result, err := connector.InsertMany(ctx, first)
	if err != nil {
		t.Fatalf("InsertMany() error = %v", err)
	}
	if result.Inserted != 2 || result.Duplicates != 0 {
		t.Fatalf("первая вставка: %+v, want Inserted=2 Duplicates=0", result)
	}

	// Повторная отправка тех же событий (at-least-once): дубли отсекает индекс.
	result, err = connector.InsertMany(ctx, first)
	if err != nil {
		t.Fatalf("повторный InsertMany() error = %v", err)
	}
	if result.Inserted != 0 || result.Duplicates != 2 {
		t.Fatalf("повторная вставка: %+v, want Inserted=0 Duplicates=2", result)
	}

	count, err := connector.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if count != 2 {
		t.Fatalf("документов в коллекции = %d, want 2 (дубликаты не должны создаваться)", count)
	}

	// Проверяем, что сервисные поля действительно сохранились.
	withLatency, err := connector.collection.CountDocuments(ctx, bson.M{"latency_ms": bson.M{"$gte": 0}})
	if err != nil {
		t.Fatalf("count with latency: %v", err)
	}
	if withLatency != 2 {
		t.Fatalf("документов с latency_ms = %d, want 2", withLatency)
	}

	if err := connector.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	_ = cfg
}

func TestIntegrationInsertSkipsEmptyBatch(t *testing.T) {
	connector, _ := integrationConnector(t)

	result, err := connector.InsertMany(context.Background(), nil)
	if err != nil {
		t.Fatalf("InsertMany(nil) error = %v", err)
	}
	if result.Inserted != 0 || result.Duplicates != 0 {
		t.Fatalf("InsertMany(nil) = %+v, want нули", result)
	}
}
