// Package mongo — реализация storages.Storage поверх MongoDB.
package mongo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/Dronsan89/Wallet2/gw-notification/internal/config"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/storages"
)

// Connector реализует storages.Storage и управляет подключением к MongoDB.
type Connector struct {
	cfg *config.Config
	log *slog.Logger

	client     *mongo.Client
	collection *mongo.Collection
}

var _ storages.Storage = (*Connector)(nil)

// New создаёт коннектор. Подключение происходит в Start.
func New(cfg *config.Config, log *slog.Logger) *Connector {
	return &Connector{cfg: cfg, log: log}
}

// Start подключается к MongoDB, проверяет соединение и создаёт индексы.
func (c *Connector) Start(ctx context.Context) error {
	connectCtx, cancel := context.WithTimeout(ctx, c.cfg.MongoConnectTimeout)
	defer cancel()

	client, err := mongo.Connect(connectCtx, options.Client().ApplyURI(c.cfg.MongoURI))
	if err != nil {
		return fmt.Errorf("connect to mongo: %w", err)
	}

	if err := client.Ping(connectCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return fmt.Errorf("ping mongo: %w", err)
	}

	c.client = client
	c.collection = client.Database(c.cfg.MongoDB).Collection(c.cfg.MongoCollection)

	if err := c.ensureIndexes(connectCtx); err != nil {
		_ = client.Disconnect(context.Background())
		c.client = nil
		return err
	}

	c.log.Info("mongo connected",
		"database", c.cfg.MongoDB,
		"collection", c.cfg.MongoCollection,
	)

	return nil
}

// ensureIndexes создаёт уникальный индекс по transaction_id (идемпотентность)
// и индекс для выборки истории пользователя.
func (c *Connector) ensureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "transaction_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_transaction_id"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
			Options: options.Index().SetName("user_created"),
		},
	}

	if _, err := c.collection.Indexes().CreateMany(ctx, indexes); err != nil {
		return fmt.Errorf("create indexes: %w", err)
	}

	return nil
}

// Stop закрывает подключение к MongoDB.
func (c *Connector) Stop(context.Context) error {
	if c.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.client.Disconnect(ctx)
	c.client = nil
	c.collection = nil

	return err
}

// Ping проверяет доступность хранилища.
func (c *Connector) Ping(ctx context.Context) error {
	if c.client == nil {
		return errors.New("mongo is not connected")
	}

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	return c.client.Ping(pingCtx, nil)
}