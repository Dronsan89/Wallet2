// Package app собирает компоненты сервиса gw-exchanger и управляет их жизненным циклом.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/config"
	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/grpcserver"
	"github.com/Dronsan89/Wallet2/gw-exchanger/internal/storages/postgres"
	pkgapp "github.com/Dronsan89/Wallet2/gw-exchanger/pkg/app"
)

// App — приложение gw-exchanger: хранилище курсов и gRPC-сервер.
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

// Start поднимает базу (с миграциями и курсами), затем gRPC-сервер.
func (a *App) Start(ctx context.Context) error {
	storage := postgres.New(a.cfg, a.log)
	server := grpcserver.New(a.cfg, storage, a.log)

	a.log.Info("service configured",
		"grpc_port", a.cfg.GRPCPort,
		"migrations", a.cfg.MigrationsPath,
	)

	a.components = []component{
		{name: "postgres", svc: storage},
		{name: "grpc-server", svc: server},
	}

	for _, c := range a.components {
		if err := c.svc.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", c.name, err)
		}
		a.log.Info("component started", "component", c.name)
	}

	return nil
}

// Stop останавливает компоненты в обратном порядке.
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