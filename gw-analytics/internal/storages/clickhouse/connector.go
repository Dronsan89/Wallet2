// Package clickhouse — реализация storages.Storage поверх ClickHouse.
package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/Dronsan89/Wallet2/gw-analytics/internal/config"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/storages"
)

// tableName — имя таблицы событий внутри базы из CLICKHOUSE_DB.
const tableName = "events"

// Connector реализует storages.Storage и управляет подключением к ClickHouse.
type Connector struct {
	cfg  *config.Config
	log  *slog.Logger
	conn driver.Conn
}

var _ storages.Storage = (*Connector)(nil)

// New создаёт коннектор. Подключение происходит в Start.
func New(cfg *config.Config, log *slog.Logger) *Connector {
	return &Connector{cfg: cfg, log: log}
}

// Start подключается к ClickHouse, проверяет соединение и применяет DDL.
func (c *Connector) Start(ctx context.Context) error {
	opts := &clickhouse.Options{
		Addr: []string{c.cfg.ClickHouseAddr},
		Auth: clickhouse.Auth{
			Username: c.cfg.ClickHouseUser,
			Password: c.cfg.ClickHousePassword,
		},
		DialTimeout: c.cfg.ClickHouseTimeout,
		Compression: &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return fmt.Errorf("open clickhouse: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, c.cfg.ClickHouseTimeout)
	defer cancel()

	if err := conn.Ping(pingCtx); err != nil {
		_ = conn.Close()
		return fmt.Errorf("ping clickhouse: %w", err)
	}

	c.conn = conn

	if err := c.migrate(ctx); err != nil {
		_ = conn.Close()
		c.conn = nil

		return err
	}

	c.log.Info("clickhouse connected",
		"addr", c.cfg.ClickHouseAddr,
		"database", c.cfg.ClickHouseDB,
		"table", tableName,
	)

	return nil
}

// Stop закрывает соединение.
func (c *Connector) Stop(context.Context) error {
	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil

	return err
}

// Ping проверяет доступность ClickHouse.
func (c *Connector) Ping(ctx context.Context) error {
	if c.conn == nil {
		return errors.New("clickhouse is not connected")
	}

	pingCtx, cancel := context.WithTimeout(ctx, c.cfg.ClickHouseTimeout)
	defer cancel()

	return c.conn.Ping(pingCtx)
}

// table возвращает полное имя таблицы: база задаётся через CLICKHOUSE_DB.
func (c *Connector) table() string {
	return quoteIdent(c.cfg.ClickHouseDB) + "." + quoteIdent(tableName)
}

// migrate создаёт базу и применяет *.sql из каталога миграций.
// Плейсхолдер {database} заменяется на имя базы, чтобы DDL не зависел от окружения.
func (c *Connector) migrate(ctx context.Context) error {
	if err := c.conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteIdent(c.cfg.ClickHouseDB)); err != nil {
		return fmt.Errorf("create database %s: %w", c.cfg.ClickHouseDB, err)
	}

	entries, err := os.ReadDir(c.cfg.ClickHouseMigrations)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", c.cfg.ClickHouseMigrations, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(c.cfg.ClickHouseMigrations, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		ddl := strings.ReplaceAll(string(body), "{database}", quoteIdent(c.cfg.ClickHouseDB))
		if err := c.conn.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		c.log.Info("clickhouse migration applied", "migration", name)
	}

	return nil
}

// quoteIdent экранирует идентификатор (имя базы/таблицы) обратными кавычками.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
