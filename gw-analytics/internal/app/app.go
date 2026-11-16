// Package app собирает компоненты сервиса gw-analytics и управляет их жизненным циклом.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Dronsan89/Wallet2/gw-analytics/internal/config"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/consumer"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/httpserver"
	"github.com/Dronsan89/Wallet2/gw-analytics/internal/storages/clickhouse"
	pkgapp "github.com/Dronsan89/Wallet2/gw-analytics/pkg/app"
)

// App — приложение gw-analytics: ClickHouse, Kafka-консьюмер и HTTP API.
type App struct {
	cfg *config.Config
	log *slog.Logger

	components []component

	startTimeout time.Duration
	stopTimeout  time.Duration
}

// component — именованный компонент: имя попадает в лог и в порядок остановки.
type component struct {
	name string
	svc  pkgapp.Component
}

// New создаёт приложение.
func New(cfg *config.Config, log *slog.Logger) *App {
	if log == nil {
		log = slog.Default()
	}

	return &App{
		cfg:          cfg,
		log:          log,
		startTimeout: config.DefaultStartTimeout,
		stopTimeout:  cfg.ShutdownTimeout,
	}
}

// Start поднимает ClickHouse, затем консьюмер, затем HTTP API.
func (a *App) Start(ctx context.Context) error {
	storage := clickhouse.New(a.cfg, a.log)
	eventsConsumer := consumer.New(a.cfg, storage, a.log)
	api := httpserver.New(":"+a.cfg.HTTPPort, storage, eventsConsumer, a.log)

	a.log.Info("service configured",
		"http_port", a.cfg.HTTPPort,
		"kafka_topic", a.cfg.KafkaTopic,
		"kafka_group", a.cfg.KafkaGroup,
		"clickhouse_addr", a.cfg.ClickHouseAddr,
		"clickhouse_db", a.cfg.ClickHouseDB,
		"batch_size", a.cfg.KafkaBatchSize,
	)

	a.components = []component{
		{name: "clickhouse", svc: storage},
		{name: "kafka-consumer", svc: eventsConsumer},
		{name: "http-server", svc: api},
	}

	for _, c := range a.components {
		if err := c.svc.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", c.name, err)
		}
		a.log.Info("component started", "component", c.name)
	}

	return nil
}

// Stop останавливает компоненты в обратном порядке: сервер, консьюмер, база.
func (a *App) Stop(ctx context.Context) error {
	for i := len(a.components) - 1; i >= 0; i-- {
		c := a.components[i]
		a.log.Info("stopping component", "component", c.name)

		if err := c.svc.Stop(ctx); err != nil {
			return fmt.Errorf("stop %s: %w", c.name, err)
		}
	}

	return nil
}

// StartTimeout — сколько ждать запуска приложения.
func (a *App) StartTimeout() time.Duration { return a.startTimeout }

// StopTimeout — сколько ждать корректной остановки.
func (a *App) StopTimeout() time.Duration { return a.stopTimeout }