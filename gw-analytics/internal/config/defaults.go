package config

import "time"

// Значения по умолчанию для конфигурации analytics.
const (
	defaultHTTPPort        = "8083"
	defaultShutdownTimeout = 20 * time.Second

	defaultKafkaBroker        = "localhost:9092"
	defaultKafkaTopic         = "wallet.transactions"
	defaultKafkaGroup         = "gw-analytics"
	defaultKafkaBatchSize     = 1000
	defaultKafkaPollTimeout   = 500 * time.Millisecond
	defaultKafkaCommitTimeout = 5 * time.Second
	defaultKafkaMaxRetries    = 5
	defaultKafkaRetryBackoff  = 200 * time.Millisecond

	defaultClickHouseAddr       = "localhost:9000"
	defaultClickHouseDB         = "analytics"
	defaultClickHouseUser       = "default"
	defaultClickHouseTimeout    = 5 * time.Second
	defaultClickHouseMigrations = "migrations/clickhouse"

	// DefaultStartTimeout — сколько ждём запуска приложения.
	DefaultStartTimeout = 30 * time.Second
	// DefaultStopTimeout — сколько ждём корректной остановки.
	DefaultStopTimeout = 20 * time.Second
)