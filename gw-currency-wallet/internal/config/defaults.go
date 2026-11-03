package config

import (
	"time"

	"github.com/shopspring/decimal"
)

// Значения по умолчанию для конфигурации кошелька.
const (
	defaultHTTPPort        = "8080"
	defaultShutdownTimeout = 15 * time.Second
	defaultMigrationsPath  = "migrations"

	defaultDBHost     = "localhost"
	defaultDBPort     = "5432"
	defaultDBUser     = "wallet"
	defaultDBPassword = "wallet"
	defaultDBName     = "wallet"
	defaultDBSSLMode  = "disable"
	defaultDBMaxConns = 20
	defaultDBMinConns = 5

	defaultJWTSecret = "dev-secret-change-me"
	defaultJWTTTL    = 24 * time.Hour

	defaultExchangerAddr    = "localhost:50051"
	defaultExchangerTimeout = 2 * time.Second
	defaultRatesCacheTTL    = 5 * time.Second

	defaultKafkaBroker = "localhost:9092"
	defaultKafkaTopic  = "wallet.transactions"

	// DefaultStartTimeout — сколько ждём запуска приложения.
	DefaultStartTimeout = 30 * time.Second
	// DefaultStopTimeout — сколько ждём корректной остановки.
	DefaultStopTimeout = 15 * time.Second
)

// defaultBigTransferThreshold — порог крупной операции в рублях (требование ТЗ).
var defaultBigTransferThreshold = decimal.NewFromInt(30000)