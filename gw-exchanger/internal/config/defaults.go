package config

import "time"

// Значения по умолчанию для конфигурации exchanger.
const (
	defaultGRPCPort        = "50051"
	defaultShutdownTimeout = 15 * time.Second
	defaultMigrationsPath  = "migrations"

	defaultDBHost     = "localhost"
	defaultDBPort     = "5432"
	defaultDBUser     = "exchanger"
	defaultDBPassword = "exchanger"
	defaultDBName     = "exchanger"
	defaultDBSSLMode  = "disable"
	defaultDBMaxConns = 10
	defaultDBMinConns = 2

	// DefaultStartTimeout — сколько ждём запуска приложения.
	DefaultStartTimeout = 30 * time.Second
	// DefaultStopTimeout — сколько ждём корректной остановки.
	DefaultStopTimeout = 15 * time.Second
)