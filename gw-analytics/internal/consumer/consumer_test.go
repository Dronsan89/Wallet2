package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/Dronsan89/Wallet2/gw-analytics/internal/config"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() *config.Config {
	return &config.Config{
		KafkaTopic:         "wallet.transactions",
		KafkaGroup:         "gw-analytics",
		KafkaBatchSize:     3,
		KafkaPollTimeout:   20 * time.Millisecond,
		KafkaCommitTimeout: time.Second,
		KafkaMaxRetries:    3,
		KafkaRetryBackoff:  time.Millisecond,
	}
}

// fakeReader отдаёт заготовленные сообщения и запоминает коммиты.
type fakeReader struct {
	mu        sync.Mutex
	queue     []kafkago.Message
	committed []kafkago.Message
	closed    bool
}

func (r *fakeReader) FetchMessage(ctx context.Context) (kafkago.Message, error) {
	r.mu.Lock()
	if len(r.queue) > 0 {
		msg := r.queue[0]
		r.queue = r.queue[1:]
		r.mu.Unlock()

		return msg, nil
	}
	r.mu.Unlock()

	select {
	case <-ctx.Done():
		return kafkago.Message{}, ctx.Err()
	case <-time.After(time.Millisecond):
		return kafkago.Message{}, context.DeadlineExceeded
	}
}

func (r *fakeReader) CommitMessages(_ context.Context, msgs ...kafkago.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.committed = append(r.committed, msgs...)

	return nil
}

func (r *fakeReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.closed = true

	return nil
}

func (r *fakeReader) committedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.committed)
}

// fakeStorage запоминает события и умеет имитировать ошибку записи.
type fakeStorage struct {
	mu     sync.Mutex
	events []model.Event
	err    error
}

func (s *fakeStorage) InsertBatch(_ context.Context, events []model.Event) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return 0, s.err
	}

	s.events = append(s.events, events...)

	return len(events), nil
}

func (s *fakeStorage) Summary(context.Context, time.Time, time.Time) (model.Summary, error) {
	return model.Summary{}, nil
}

func (s *fakeStorage) TimeSeries(context.Context, time.Time, time.Time, time.Duration) ([]model.SeriesPoint, error) {
	return nil, nil
}

func (s *fakeStorage) Ping(context.Context) error { return nil }

func (s *fakeStorage) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.events)
}

func (s *fakeStorage) statuses() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(map[string]int, 2)
	for _, event := range s.events {
		result[event.Status]++
	}

	return result
}

func event(transactionID, status string) model.Event {
	return model.Event{
		EventID:       transactionID,
		TransactionID: transactionID,
		UserID:        "user-1",
		Operation:     "DEPOSIT",
		Status:        status,
		FromCurrency:  "USD",
		Amount:        1000,
		RubEquivalent: 90000,
		CreatedAt:     time.Now().UTC(),
		Source:        "gw-currency-wallet",
		SchemaVersion: 1,
	}
}

func eventMessage(t *testing.T, e model.Event) kafkago.Message {
	t.Helper()

	payload, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	return kafkago.Message{Value: payload}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}

	t.Fatal("не дождались условия за 2 секунды")
}

func TestBatchStoredAndCommitted(t *testing.T) {
	reader := &fakeReader{queue: []kafkago.Message{
		eventMessage(t, event("tx-1", model.StatusSuccess)),
		eventMessage(t, event("tx-2", model.StatusSuccess)),
		eventMessage(t, event("tx-3", model.StatusSuccess)),
	}}
	storage := &fakeStorage{}

	c := NewWithDeps(testConfig(), storage, testLogger(), reader)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return storage.count() == 3 })
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := reader.committedCount(); got != 3 {
		t.Fatalf("закоммичено %d, want 3", got)
	}
	if !reader.closed {
		t.Fatal("Stop() должен закрывать reader")
	}

	stats := c.Stats()
	if stats.Stored != 3 || stats.Received != 3 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestFailedStatusIsStored(t *testing.T) {
	// Аналитике нужны и неуспешные операции — для частоты ошибок.
	reader := &fakeReader{queue: []kafkago.Message{
		eventMessage(t, event("tx-ok", model.StatusSuccess)),
		eventMessage(t, event("tx-fail", model.StatusFailed)),
	}}
	storage := &fakeStorage{}

	c := NewWithDeps(testConfig(), storage, testLogger(), reader)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return storage.count() == 2 })
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	statuses := storage.statuses()
	if statuses[model.StatusFailed] != 1 {
		t.Fatalf("FAILED-событий сохранено %d, want 1", statuses[model.StatusFailed])
	}
}

func TestPoisonMessageSkipped(t *testing.T) {
	reader := &fakeReader{queue: []kafkago.Message{
		{Value: []byte("not a json")},
		eventMessage(t, event("tx-1", model.StatusSuccess)),
	}}
	storage := &fakeStorage{}

	c := NewWithDeps(testConfig(), storage, testLogger(), reader)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return storage.count() == 1 })
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := reader.committedCount(); got != 2 {
		t.Fatalf("закоммичено %d, want 2 (включая «ядовитое»)", got)
	}
	if got := c.Stats().Poison; got != 1 {
		t.Fatalf("poison = %d, want 1", got)
	}
}

func TestStoreFailureDropsBatch(t *testing.T) {
	reader := &fakeReader{queue: []kafkago.Message{eventMessage(t, event("tx-1", model.StatusSuccess))}}
	storage := &fakeStorage{err: errors.New("clickhouse is down")}

	cfg := testConfig()
	cfg.KafkaMaxRetries = 3

	c := NewWithDeps(cfg, storage, testLogger(), reader)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return c.Stats().Dropped == 1 })
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := c.Stats().Retries; got < 3 {
		t.Fatalf("ретраев = %d, want >= 3", got)
	}
	if got := reader.committedCount(); got != 1 {
		t.Fatalf("закоммичено %d, want 1 (батч не должен залипать)", got)
	}
}

func TestStopFlushesPendingBatch(t *testing.T) {
	cfg := testConfig()
	cfg.KafkaBatchSize = 100
	cfg.KafkaPollTimeout = 500 * time.Millisecond

	reader := &fakeReader{queue: []kafkago.Message{
		eventMessage(t, event("tx-1", model.StatusSuccess)),
		eventMessage(t, event("tx-2", model.StatusSuccess)),
	}}
	storage := &fakeStorage{}

	c := NewWithDeps(cfg, storage, testLogger(), reader)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return c.Stats().Received == 2 })

	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := storage.count(); got != 2 {
		t.Fatalf("после остановки сохранено %d, want 2", got)
	}
	if got := reader.committedCount(); got != 2 {
		t.Fatalf("после остановки закоммичено %d, want 2", got)
	}
}
