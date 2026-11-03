// Package app собирает компоненты сервиса gw-currency-wallet и управляет их жизненным циклом.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/auth"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/config"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/handler"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/kafka"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/rates"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/server"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/service"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/storages/postgres"
	pkgapp "github.com/Dronsan89/Wallet2/gw-currency-wallet/pkg/app"
)

// App — приложение gw-currency-wallet.
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

// Start собирает зависимости и поднимает компоненты в порядке добавления:
// сначала база и клиенты, затем HTTP-сервер.
func (a *App) Start(ctx context.Context) error {
	storage := postgres.New(a.cfg, a.log)

	ratesClient, err := rates.NewClient(a.cfg.ExchangerAddr, a.cfg.ExchangerTimeout)
	if err != nil {
		return fmt.Errorf("create rates client: %w", err)
	}
	ratesCache := rates.NewCache(ratesClient, a.cfg.RatesCacheTTL)

	producer := kafka.New(a.cfg.KafkaBrokers, a.cfg.KafkaTopic, a.log)

	authManager := auth.NewManager(a.cfg.JWTSecret, a.cfg.JWTTTL)

	walletService := service.New(service.Deps{
		Storage:   storage,
		Rates:     ratesCache,
		Auth:      authManager,
		Publisher: producer,
		Log:       a.log,
		Threshold: a.cfg.BigTransferThreshold,
	})

	api := handler.New(walletService, authManager, a.log)
	httpServer := server.New(":"+a.cfg.HTTPPort, api.Router(), a.log)

	a.log.Info("service configured",
		"http_port", a.cfg.HTTPPort,
		"exchanger", a.cfg.ExchangerAddr,
		"rates_cache_ttl", a.cfg.RatesCacheTTL.String(),
		"kafka_topic", a.cfg.KafkaTopic,
		"big_transfer_threshold", a.cfg.BigTransferThreshold.String(),
	)

	a.components = []component{
		{name: "postgres", svc: storage},
		{name: "rates-client", svc: ratesClient},
		{name: "kafka-producer", svc: producer},
		{name: "http-server", svc: httpServer},
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