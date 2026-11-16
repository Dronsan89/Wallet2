// Package app запускает сервис как приложение с корректным завершением по сигналу.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Component — часть приложения с управляемым жизненным циклом:
// HTTP- или gRPC-сервер, пул БД, Kafka-продюсер или консьюмер.
type Component interface {
	// Start запускает компонент и возвращает управление после готовности.
	Start(ctx context.Context) error
	// Stop останавливает компонент, дожидаясь завершения текущей работы.
	Stop(ctx context.Context) error
}

// Application — приложение, которое умеет стартовать и останавливаться.
type Application interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	// StartTimeout — сколько ждать запуска приложения.
	StartTimeout() time.Duration
	// StopTimeout — сколько ждать корректной остановки.
	StopTimeout() time.Duration
}

// Run запускает приложение, ждёт SIGINT/SIGTERM и выполняет graceful shutdown.
func Run(a Application, log *slog.Logger) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(quit)

	return runWithSignals(a, log, quit)
}

// runWithSignals — реализация Run с внешним каналом сигналов: так её можно
// проверить в тестах, не посылая сигналы процессу.
func runWithSignals(a Application, log *slog.Logger, quit <-chan os.Signal) error {
	if a == nil {
		return errors.New("app: nil application")
	}
	if log == nil {
		log = slog.Default()
	}

	startCtx, cancelStart := context.WithTimeout(context.Background(), a.StartTimeout())
	defer cancelStart()

	if err := a.Start(startCtx); err != nil {
		return fmt.Errorf("app: start: %w", err)
	}
	log.Info("application started", "start_timeout", a.StartTimeout().String())

	sig := <-quit
	log.Info("shutdown signal received", "signal", sig.String())

	stopCtx, cancelStop := context.WithTimeout(context.Background(), a.StopTimeout())
	defer cancelStop()

	if err := a.Stop(stopCtx); err != nil {
		return fmt.Errorf("app: stop: %w", err)
	}
	log.Info("application stopped")

	return nil
}