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

	"github.com/Dronsan89/Wallet2/gw-notification/internal/config"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/model"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/storages"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() *config.Config {
	return &config.Config{
		KafkaTopic:         "wallet.transactions",
		KafkaGroup:         "gw-notification",
		KafkaDLQTopic:      "wallet.transactions.dlq",
		KafkaBatchSize:     3,
		KafkaPollTimeout:   20 * time.Millisecond,
		KafkaCommitTimeout: time.Second,
		KafkaMaxRetries:    3,
		KafkaRetryBackoff:  time.Millisecond,
	}
}

// fakeReader отдаёт заранее заготовленные сообщения, запоминает коммиты.
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

func (r *fakeReader) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.closed
}

func (r *fakeReader) committedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.committed)
}

// fakeDLQ собирает «мёртвые» сообщения.
type fakeDLQ struct {
	mu     sync.Mutex
	msgs   []kafkago.Message
	closed bool
}

func (d *fakeDLQ) WriteMessages(_ context.Context, msgs ...kafkago.Message) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.msgs = append(d.msgs, msgs...)

	return nil
}

func (d *fakeDLQ) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.closed = true

	return nil
}

func (d *fakeDLQ) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return len(d.msgs)
}

// fakeStorage запоминает события и умеет имитировать ошибку и дубликаты.
type fakeStorage struct {
	mu         sync.Mutex
	events     []model.Event
	duplicates int64
	err        error
}

func (s *fakeStorage) InsertMany(_ context.Context, events []model.Event) (storages.InsertResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return storages.InsertResult{}, s.err
	}

	s.events = append(s.events, events...)

	inserted := int64(len(events)) - s.duplicates
	if inserted < 0 {
		inserted = 0
	}

	return storages.InsertResult{Inserted: inserted, Duplicates: s.duplicates}, nil
}

func (s *fakeStorage) Ping(context.Context) error { return nil }

func (s *fakeStorage) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.events)
}

// eventMessage собирает сообщение Kafka из события.
func eventMessage(t *testing.T, event model.Event) kafkago.Message {
	t.Helper()

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	return kafkago.Message{Value: payload}
}

func successEvent(transactionID string) model.Event {
	return model.Event{
		EventID:       transactionID,
		TransactionID: transactionID,
		UserID:        "user-1",
		Operation:     "DEPOSIT",
		Status:        model.StatusSuccess,
		FromCurrency:  "USD",
		Amount:        1000,
		RubEquivalent: 90000,
		CreatedAt:     time.Now().UTC(),
		Source:        "gw-currency-wallet",
		SchemaVersion: 1,
	}
}

// waitFor ждёт выполнения условия, но не дольше двух секунд.
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
		eventMessage(t, successEvent("tx-1")),
		eventMessage(t, successEvent("tx-2")),
		eventMessage(t, successEvent("tx-3")),
	}}
	storage := &fakeStorage{}
	dlq := &fakeDLQ{}

	c := NewWithDeps(testConfig(), storage, testLogger(), reader, dlq)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return storage.count() == 3 })
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := reader.committedCount(); got != 3 {
		t.Fatalf("закоммичено %d сообщений, want 3", got)
	}
	if got := dlq.count(); got != 0 {
		t.Fatalf("в DLQ %d сообщений, want 0", got)
	}

	stats := c.Stats()
	if stats.Stored != 3 {
		t.Fatalf("stored = %d, want 3", stats.Stored)
	}
	if stats.Committed != 3 {
		t.Fatalf("committed = %d, want 3", stats.Committed)
	}
	if !reader.closed || !dlq.closed {
		t.Fatal("Stop() должен закрывать reader и DLQ-writer")
	}
}

func TestPoisonMessageGoesToDLQ(t *testing.T) {
	reader := &fakeReader{queue: []kafkago.Message{
		{Value: []byte("not a json")},
		eventMessage(t, successEvent("tx-1")),
	}}
	storage := &fakeStorage{}
	dlq := &fakeDLQ{}

	c := NewWithDeps(testConfig(), storage, testLogger(), reader, dlq)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return storage.count() == 1 && dlq.count() == 1 })
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

func TestFailedStatusSkipped(t *testing.T) {
	failed := successEvent("tx-failed")
	failed.Status = model.StatusFailed

	reader := &fakeReader{queue: []kafkago.Message{eventMessage(t, failed)}}
	storage := &fakeStorage{}
	dlq := &fakeDLQ{}

	c := NewWithDeps(testConfig(), storage, testLogger(), reader, dlq)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return reader.committedCount() == 1 })
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := storage.count(); got != 0 {
		t.Fatalf("в историю попало %d событий, want 0 (неуспешные пропускаем)", got)
	}
	if got := c.Stats().Skipped; got != 1 {
		t.Fatalf("skipped = %d, want 1", got)
	}
}

func TestStorageFailureGoesToDLQAfterRetries(t *testing.T) {
	reader := &fakeReader{queue: []kafkago.Message{eventMessage(t, successEvent("tx-1"))}}
	storage := &fakeStorage{err: errors.New("mongo is down")}
	dlq := &fakeDLQ{}

	cfg := testConfig()
	cfg.KafkaMaxRetries = 3

	c := NewWithDeps(cfg, storage, testLogger(), reader, dlq)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return dlq.count() == 1 })
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := c.Stats().Retries; got < 3 {
		t.Fatalf("ретраев = %d, want >= 3", got)
	}
	if got := reader.committedCount(); got != 1 {
		t.Fatalf("закоммичено %d, want 1 (сообщение не должно залипать)", got)
	}
}

func TestDuplicatesCounted(t *testing.T) {
	reader := &fakeReader{queue: []kafkago.Message{
		eventMessage(t, successEvent("tx-1")),
		eventMessage(t, successEvent("tx-2")),
	}}
	storage := &fakeStorage{duplicates: 2}
	dlq := &fakeDLQ{}

	c := NewWithDeps(testConfig(), storage, testLogger(), reader, dlq)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return c.Stats().Duplicates == 2 })
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := c.Stats().Stored; got != 0 {
		t.Fatalf("stored = %d, want 0 (оба документа — дубликаты)", got)
	}
}

func TestStopFlushesPendingBatch(t *testing.T) {
	cfg := testConfig()
	cfg.KafkaBatchSize = 100                      // батч не наберётся по размеру
	cfg.KafkaPollTimeout = 500 * time.Millisecond // и по таймауту тоже не успеет

	reader := &fakeReader{queue: []kafkago.Message{
		eventMessage(t, successEvent("tx-1")),
		eventMessage(t, successEvent("tx-2")),
	}}
	storage := &fakeStorage{}
	dlq := &fakeDLQ{}

	c := NewWithDeps(cfg, storage, testLogger(), reader, dlq)
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	waitFor(t, func() bool { return c.Stats().Received == 2 })

	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := storage.count(); got != 2 {
		t.Fatalf("после остановки сохранено %d событий, want 2", got)
	}
	if got := reader.committedCount(); got != 2 {
		t.Fatalf("после остановки закоммичено %d, want 2", got)
	}
}

// TestStalledReaderIsRecreated проверяет самовосстановление: если reader
// перестал отдавать сообщения (зависшая сессия группы после сбоя брокера),
// консьюмер пересоздаёт его и продолжает читать с коммитнутых offset'ов.
func TestStalledReaderIsRecreated(t *testing.T) {
	cfg := testConfig()
	cfg.KafkaIdleReconnect = 30 * time.Millisecond

	stalled := &fakeReader{} // очередь пуста: всегда DeadlineExceeded
	replacement := &fakeReader{queue: []kafkago.Message{eventMessage(t, successEvent("tx-reconnect"))}}
	storage := &fakeStorage{}

	c := NewWithDeps(cfg, storage, testLogger(), stalled, &fakeDLQ{})
	c.newReader = func() (MessageReader, error) { return replacement, nil }

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	waitFor(t, func() bool { return storage.count() >= 1 })
	waitFor(t, func() bool { return stalled.isClosed() })

	if got := c.Stats().Stored; got < 1 {
		t.Fatalf("после пересоздания reader событие не сохранено: stored=%d", got)
	}
}
