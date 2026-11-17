// Package consumer читает события о крупных переводах из Kafka и сохраняет их.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/Dronsan89/Wallet2/gw-notification/internal/config"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/model"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/storages"
)

// MessageReader — минимум, который консьюмеру нужен от Kafka-ридера.
// Интерфейс позволяет подменить Kafka в тестах.
type MessageReader interface {
	FetchMessage(ctx context.Context) (kafkago.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafkago.Message) error
	Close() error
}

// DeadLetterSender — минимум для отправки «ядовитых» сообщений в DLQ.
type DeadLetterSender interface {
	WriteMessages(ctx context.Context, msgs ...kafkago.Message) error
	Close() error
}

// Consumer — консьюмер событий: батчи, ретраи, DLQ, ручной commit.
type Consumer struct {
	cfg     *config.Config
	log     *slog.Logger
	storage storages.Storage
	reader  MessageReader
	dlq     DeadLetterSender
	now     func() time.Time

	readerMu  sync.Mutex
	newReader func() (MessageReader, error)

	stats  Stats
	cancel context.CancelFunc
	done   chan struct{}
}

// New создаёт консьюмер с настоящими клиентами Kafka.
func New(cfg *config.Config, storage storages.Storage, log *slog.Logger) *Consumer {
	reader := newKafkaReader(cfg)

	dlq := &kafkago.Writer{
		Addr:         kafkago.TCP(cfg.KafkaBrokers...),
		Topic:        cfg.KafkaDLQTopic,
		Balancer:     &kafkago.Hash{},
		RequiredAcks: kafkago.RequireAll,
		BatchTimeout: 10 * time.Millisecond,
	}

	c := NewWithDeps(cfg, storage, log, reader, dlq)
	c.newReader = func() (MessageReader, error) { return newKafkaReader(cfg), nil }

	return c
}

// newKafkaReader создаёт reader consumer group. Отдельная функция нужна,
// чтобы пересоздать клиента, если сессия группы зависла после сбоя брокера.
func newKafkaReader(cfg *config.Config) MessageReader {
	return kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        cfg.KafkaBrokers,
		GroupID:        cfg.KafkaGroup,
		Topic:          cfg.KafkaTopic,
		MinBytes:       1,
		MaxBytes:       10 << 20,
		MaxWait:        cfg.KafkaPollTimeout,
		CommitInterval: 0, // коммитим вручную, только после записи в Mongo
		StartOffset:    kafkago.FirstOffset,
	})
}

// currentReader возвращает текущий reader (подменяется при пересоздании).
func (c *Consumer) currentReader() MessageReader {
	c.readerMu.Lock()
	defer c.readerMu.Unlock()

	return c.reader
}

// swapReader подменяет reader и закрывает предыдущий.
func (c *Consumer) swapReader(reader MessageReader) {
	c.readerMu.Lock()
	old := c.reader
	c.reader = reader
	c.readerMu.Unlock()

	if old != nil {
		if err := old.Close(); err != nil {
			c.log.Error("close stale kafka reader", "error", err)
		}
	}
}

// reconnectIfStalled пересоздаёт reader, если сообщений нет дольше
// KAFKA_IDLE_RECONNECT: так консьюмер сам лечится после сбоя брокера.
func (c *Consumer) reconnectIfStalled(lastMessage *time.Time, limit *time.Duration) {
	if c.cfg.KafkaIdleReconnect <= 0 || c.newReader == nil {
		return
	}
	if time.Since(*lastMessage) < *limit {
		return
	}

	c.log.Warn("kafka reader idle too long, reconnecting",
		"idle", time.Since(*lastMessage).Round(time.Second).String(),
		"limit", limit.String(),
	)

	reader, err := c.newReader()
	if err != nil {
		c.log.Error("recreate kafka reader", "error", err)
		*lastMessage = time.Now()

		return
	}

	c.swapReader(reader)
	*lastMessage = time.Now()
	// Если поток просто редкий, не пересоздаём reader каждые N минут:
	// пауза удваивается до 30 минут и сбрасывается при первом сообщении.
	if next := *limit * 2; next <= 30*time.Minute {
		*limit = next
	}

	c.log.Info("kafka reader recreated", "next_check", limit.String())
}

// NewWithDeps создаёт консьюмер с переданными зависимостями (используется в тестах).
func NewWithDeps(
	cfg *config.Config,
	storage storages.Storage,
	log *slog.Logger,
	reader MessageReader,
	dlq DeadLetterSender,
) *Consumer {
	if log == nil {
		log = slog.Default()
	}

	return &Consumer{
		cfg:     cfg,
		log:     log,
		storage: storage,
		reader:  reader,
		dlq:     dlq,
		now:     time.Now,
	}
}

// Start запускает цикл обработки в отдельной горутине.
func (c *Consumer) Start(_ context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.done = make(chan struct{})

	go c.run(ctx)

	c.log.Info("consumer started",
		"topic", c.cfg.KafkaTopic,
		"group", c.cfg.KafkaGroup,
		"dlq_topic", c.cfg.KafkaDLQTopic,
		"batch_size", c.cfg.KafkaBatchSize,
	)

	return nil
}

// Stop останавливает цикл, дожидается сброса текущего батча и закрывает клиентов.
func (c *Consumer) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}

	if c.done != nil {
		select {
		case <-c.done:
		case <-ctx.Done():
			c.log.Warn("consumer stop timed out", "error", ctx.Err())
		}
	}

	if reader := c.currentReader(); reader != nil {
		if err := reader.Close(); err != nil {
			c.log.Error("close kafka reader", "error", err)
		}
	}
	if err := c.dlq.Close(); err != nil {
		c.log.Error("close dlq writer", "error", err)
	}

	c.log.Info("consumer stopped", "stats", c.stats.Snapshot())

	return nil
}

// Stats возвращает снимок счётчиков обработки.
func (c *Consumer) Stats() Snapshot { return c.stats.Snapshot() }

// run — основной цикл: набираем батч и сбрасываем его по размеру или по таймауту.
func (c *Consumer) run(ctx context.Context) {
	defer close(c.done)

	batch := make([]kafkago.Message, 0, c.cfg.KafkaBatchSize)
	lastMessage := time.Now()
	stallLimit := c.cfg.KafkaIdleReconnect

	for {
		msg, err := c.fetch(ctx)

		switch {
		case err == nil:
			c.stats.Received.Add(1)
			lastMessage = time.Now()
			stallLimit = c.cfg.KafkaIdleReconnect // пришло сообщение — сбрасываем паузу
			batch = append(batch, msg)
			if len(batch) >= c.cfg.KafkaBatchSize {
				c.flush(ctx, &batch)
			}

		case errors.Is(err, context.DeadlineExceeded):
			// Новых сообщений нет — сбрасываем накопленное, чтобы не ждать вечно.
			c.flush(ctx, &batch)
			c.reconnectIfStalled(&lastMessage, &stallLimit)

		default:
			if ctx.Err() != nil {
				// Остановка: досылаем батч в фоне (контекст уже отменён).
				c.flush(context.Background(), &batch)
				c.log.Info("consumer loop finished", "stats", c.stats.Snapshot())
				return
			}

			c.log.Error("kafka fetch failed", "error", err)
			c.sleep(ctx, c.cfg.KafkaRetryBackoff)
		}
	}
}

// fetch читает одно сообщение с таймаутом, чтобы цикл был отзывчивым к остановке.
func (c *Consumer) fetch(ctx context.Context) (kafkago.Message, error) {
	pollCtx, cancel := context.WithTimeout(ctx, c.cfg.KafkaPollTimeout)
	defer cancel()

	return c.currentReader().FetchMessage(pollCtx)
}

// flush разбирает батч, пишет события в хранилище и коммитит сообщения.
func (c *Consumer) flush(ctx context.Context, batch *[]kafkago.Message) {
	msgs := *batch
	if len(msgs) == 0 {
		return
	}
	*batch = (*batch)[:0]

	events := make([]model.Event, 0, len(msgs))
	eventMsgs := make([]kafkago.Message, 0, len(msgs))
	poison := make([]kafkago.Message, 0)

	for _, msg := range msgs {
		event, err := c.decode(msg)
		if err != nil {
			c.stats.Poison.Add(1)
			c.log.Warn("poison message",
				"partition", msg.Partition,
				"offset", msg.Offset,
				"error", err,
			)
			poison = append(poison, msg)

			continue
		}

		if event.Status != model.StatusSuccess {
			// В историю переводов пишем только успешные операции;
			// неуспешные нужны аналитике, но не этому сервису.
			c.stats.Skipped.Add(1)
			continue
		}

		events = append(events, event)
		eventMsgs = append(eventMsgs, msg)
	}

	if len(poison) > 0 {
		c.sendToDLQ(ctx, poison, "invalid message")
	}

	if len(events) > 0 {
		if err := c.store(ctx, events); err != nil {
			c.log.Error("store batch failed, sending to dlq", "batch", len(events), "error", err)
			c.sendToDLQ(ctx, eventMsgs, "store failed")
		}
	}

	if err := c.commit(ctx, msgs); err != nil {
		// Не коммитим — Kafka отдаст сообщения повторно, идемпотентность спасёт.
		c.log.Error("commit failed", "batch", len(msgs), "error", err)
	}
}

// decode превращает сообщение Kafka в событие и дополняет вычисляемые поля.
func (c *Consumer) decode(msg kafkago.Message) (model.Event, error) {
	var event model.Event

	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return event, fmt.Errorf("decode json: %w", err)
	}

	if err := event.Validate(); err != nil {
		return event, fmt.Errorf("validate: %w", err)
	}

	event.FillComputed(c.now())

	return event, nil
}

// store сохраняет события с повторами и экспоненциальной паузой.
func (c *Consumer) store(ctx context.Context, events []model.Event) error {
	var lastErr error

	backoff := c.cfg.KafkaRetryBackoff

	for attempt := 1; attempt <= c.cfg.KafkaMaxRetries; attempt++ {
		result, err := c.storage.InsertMany(ctx, events)
		if err == nil {
			c.stats.Stored.Add(result.Inserted)
			c.stats.Duplicates.Add(result.Duplicates)

			c.log.Info("batch stored",
				"batch", len(events),
				"stored", result.Inserted,
				"duplicates", result.Duplicates,
			)

			return nil
		}

		lastErr = err
		c.stats.Retries.Add(1)

		c.log.Warn("store batch attempt failed",
			"attempt", attempt,
			"batch", len(events),
			"error", err,
		)

		if attempt == c.cfg.KafkaMaxRetries {
			break
		}

		c.sleep(ctx, backoff)
		backoff *= 2
	}

	return lastErr
}

// sendToDLQ отправляет сообщения в топик «мёртвых» сообщений.
func (c *Consumer) sendToDLQ(ctx context.Context, msgs []kafkago.Message, reason string) {
	if len(msgs) == 0 {
		return
	}

	dlqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.KafkaCommitTimeout)
	defer cancel()

	if err := c.dlq.WriteMessages(dlqCtx, msgs...); err != nil {
		c.log.Error("dlq write failed", "batch", len(msgs), "reason", reason, "error", err)
		return
	}

	c.stats.DLQ.Add(int64(len(msgs)))

	c.log.Warn("messages sent to dlq",
		"batch", len(msgs),
		"reason", reason,
		"topic", c.cfg.KafkaDLQTopic,
	)
}

// commit фиксирует смещения после успешной записи.
func (c *Consumer) commit(ctx context.Context, msgs []kafkago.Message) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.KafkaCommitTimeout)
	defer cancel()

	if err := c.currentReader().CommitMessages(commitCtx, msgs...); err != nil {
		return err
	}

	c.stats.Committed.Add(int64(len(msgs)))

	return nil
}

// sleep ждёт d или отмену контекста.
func (c *Consumer) sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
