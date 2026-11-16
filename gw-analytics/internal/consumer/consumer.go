// Package consumer читает события из Kafka и складывает их в ClickHouse.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/Dronsan89/Wallet2/gw-analytics/internal/config"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/model"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/storages"
)

// MessageReader — минимум, который консьюмеру нужен от Kafka-ридера.
// Интерфейс позволяет подменить Kafka в тестах.
type MessageReader interface {
	FetchMessage(ctx context.Context) (kafkago.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafkago.Message) error
	Close() error
}

// Consumer — консьюмер событий: батчи, ретраи, ручной commit.
type Consumer struct {
	cfg     *config.Config
	log     *slog.Logger
	storage storages.Storage
	reader  MessageReader
	now     func() time.Time

	stats  Stats
	cancel context.CancelFunc
	done   chan struct{}
}

// New создаёт консьюмер с настоящим клиентом Kafka.
func New(cfg *config.Config, storage storages.Storage, log *slog.Logger) *Consumer {
	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        cfg.KafkaBrokers,
		GroupID:        cfg.KafkaGroup,
		Topic:          cfg.KafkaTopic,
		MinBytes:       1,
		MaxBytes:       10 << 20,
		MaxWait:        cfg.KafkaPollTimeout,
		CommitInterval: 0, // коммитим вручную, только после записи в ClickHouse
		StartOffset:    kafkago.FirstOffset,
	})

	return NewWithDeps(cfg, storage, log, reader)
}

// NewWithDeps создаёт консьюмер с переданными зависимостями (используется в тестах).
func NewWithDeps(cfg *config.Config, storage storages.Storage, log *slog.Logger, reader MessageReader) *Consumer {
	if log == nil {
		log = slog.Default()
	}

	return &Consumer{
		cfg:     cfg,
		log:     log,
		storage: storage,
		reader:  reader,
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
		"batch_size", c.cfg.KafkaBatchSize,
	)

	return nil
}

// Stop останавливает цикл, дожидается сброса текущего батча и закрывает ридер.
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

	if err := c.reader.Close(); err != nil {
		c.log.Error("close kafka reader", "error", err)
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

	for {
		msg, err := c.fetch(ctx)

		switch {
		case err == nil:
			c.stats.Received.Add(1)
			batch = append(batch, msg)
			if len(batch) >= c.cfg.KafkaBatchSize {
				c.flush(ctx, &batch)
			}

		case errors.Is(err, context.DeadlineExceeded):
			c.flush(ctx, &batch)

		default:
			if ctx.Err() != nil {
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

	return c.reader.FetchMessage(pollCtx)
}

// flush разбирает батч, пишет события в ClickHouse и коммитит сообщения.
func (c *Consumer) flush(ctx context.Context, batch *[]kafkago.Message) {
	msgs := *batch
	if len(msgs) == 0 {
		return
	}
	*batch = (*batch)[:0]

	events := make([]model.Event, 0, len(msgs))

	for _, msg := range msgs {
		event, err := c.decode(msg)
		if err != nil {
			c.stats.Poison.Add(1)
			c.log.Warn("poison message",
				"partition", msg.Partition,
				"offset", msg.Offset,
				"error", err,
			)

			continue
		}

		events = append(events, event)
	}

	if len(events) > 0 {
		if err := c.store(ctx, events); err != nil {
			// Аналитика — производные данные: лучше потерять батч и не блокировать
			// партицию навсегда, чем остановить обработку. Считаем и логируем.
			c.stats.Dropped.Add(int64(len(events)))
			c.log.Error("batch dropped", "batch", len(events), "error", err)
		}
	}

	if err := c.commit(ctx, msgs); err != nil {
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
		inserted, err := c.storage.InsertBatch(ctx, events)
		if err == nil {
			c.stats.Stored.Add(int64(inserted))

			c.log.Info("batch stored", "batch", len(events), "inserted", inserted)

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

// commit фиксирует смещения после успешной записи.
func (c *Consumer) commit(ctx context.Context, msgs []kafkago.Message) error {
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.KafkaCommitTimeout)
	defer cancel()

	if err := c.reader.CommitMessages(commitCtx, msgs...); err != nil {
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