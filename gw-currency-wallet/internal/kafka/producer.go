// Package kafka публикует события о крупных операциях в топик wallet.transactions.
package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

// Producer — продюсер Kafka: подтверждения от всех реплик, ключ = user_id,
// поэтому события одного пользователя попадают в одну партицию и не теряют порядок.
type Producer struct {
	writer *kafkago.Writer
	topic  string
	log    *slog.Logger
}

// New создаёт продюсера.
func New(brokers []string, topic string, log *slog.Logger) *Producer {
	return &Producer{
		topic: topic,
		log:   log,
		writer: &kafkago.Writer{
			Addr:         kafkago.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafkago.Hash{},
			RequiredAcks: kafkago.RequireAll,
			BatchSize:    100,
			BatchTimeout: 10 * time.Millisecond,
		},
	}
}

// Publish отправляет сообщение в топик.
func (p *Producer) Publish(ctx context.Context, key string, payload []byte) error {
	err := p.writer.WriteMessages(ctx, kafkago.Message{
		Key:   []byte(key),
		Value: payload,
	})
	if err != nil {
		return fmt.Errorf("publish to %s: %w", p.topic, err)
	}

	return nil
}

// Start ничего не делает: соединение с брокером открывается при первой записи.
func (p *Producer) Start(context.Context) error { return nil }

// Stop закрывает продюсера.
func (p *Producer) Stop(context.Context) error { return p.writer.Close() }