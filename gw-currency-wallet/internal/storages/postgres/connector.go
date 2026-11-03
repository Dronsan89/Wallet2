// Package postgres — реализация storages.Storage поверх PostgreSQL.
package postgres

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/config"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages"
)

// Connector реализует storages.Storage и управляет пулом соединений.
type Connector struct {
	cfg  *config.Config
	log  *slog.Logger
	pool *pgxpool.Pool
}

var _ storages.Storage = (*Connector)(nil)

// New создаёт коннектор к PostgreSQL. Подключение происходит в Start.
func New(cfg *config.Config, log *slog.Logger) *Connector {
	return &Connector{cfg: cfg, log: log}
}

// Start подключается к базе, проверяет соединение и применяет миграции.
func (c *Connector) Start(ctx context.Context) error {
	poolCfg, err := pgxpool.ParseConfig(c.cfg.DatabaseDSN)
	if err != nil {
		return fmt.Errorf("parse database dsn: %w", err)
	}

	poolCfg.MaxConns = c.cfg.DBMaxConns
	poolCfg.MinConns = c.cfg.DBMinConns

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("ping postgres: %w", err)
	}

	if err := migrate(ctx, pool, c.cfg.MigrationsPath, c.log); err != nil {
		pool.Close()
		return fmt.Errorf("apply migrations: %w", err)
	}

	c.pool = pool
	c.log.Info("postgres connected")

	return nil
}

// Stop закрывает пул соединений.
func (c *Connector) Stop(context.Context) error {
	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
	}
	return nil
}