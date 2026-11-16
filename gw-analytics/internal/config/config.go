// Package config загружает настройки сервиса из config.env и переменных окружения.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/Dronsan89/Wallet2/gw-analytics/pkg"
)

// Config — настройки gw-analytics.
type Config struct {
	HTTPPort        string
	ShutdownTimeout time.Duration

	KafkaBrokers       []string
	KafkaTopic         string
	KafkaGroup         string
	KafkaBatchSize     int
	KafkaPollTimeout   time.Duration
	KafkaCommitTimeout time.Duration
	KafkaMaxRetries    int
	KafkaRetryBackoff  time.Duration

	ClickHouseAddr       string
	ClickHouseDB         string
	ClickHouseUser       string
	ClickHousePassword   string
	ClickHouseTimeout    time.Duration
	ClickHouseMigrations string
}

// New собирает конфигурацию из переменных окружения.
func New() *Config {
	return &Config{
		HTTPPort:        pkg.GetEnv("HTTP_PORT", defaultHTTPPort),
		ShutdownTimeout: pkg.GetEnvDuration("SHUTDOWN_TIMEOUT", defaultShutdownTimeout),

		KafkaBrokers:       pkg.GetEnvList("KAFKA_BROKERS", []string{defaultKafkaBroker}),
		KafkaTopic:         pkg.GetEnv("KAFKA_TOPIC", defaultKafkaTopic),
		KafkaGroup:         pkg.GetEnv("KAFKA_GROUP", defaultKafkaGroup),
		KafkaBatchSize:     pkg.GetEnvInt("KAFKA_BATCH_SIZE", defaultKafkaBatchSize),
		KafkaPollTimeout:   pkg.GetEnvDuration("KAFKA_POLL_TIMEOUT", defaultKafkaPollTimeout),
		KafkaCommitTimeout: pkg.GetEnvDuration("KAFKA_COMMIT_TIMEOUT", defaultKafkaCommitTimeout),
		KafkaMaxRetries:    pkg.GetEnvInt("KAFKA_MAX_RETRIES", defaultKafkaMaxRetries),
		KafkaRetryBackoff:  pkg.GetEnvDuration("KAFKA_RETRY_BACKOFF", defaultKafkaRetryBackoff),

		ClickHouseAddr:       pkg.GetEnv("CLICKHOUSE_ADDR", defaultClickHouseAddr),
		ClickHouseDB:         pkg.GetEnv("CLICKHOUSE_DB", defaultClickHouseDB),
		ClickHouseUser:       pkg.GetEnv("CLICKHOUSE_USER", defaultClickHouseUser),
		ClickHousePassword:   pkg.GetEnv("CLICKHOUSE_PASSWORD", ""),
		ClickHouseTimeout:    pkg.GetEnvDuration("CLICKHOUSE_TIMEOUT", defaultClickHouseTimeout),
		ClickHouseMigrations: pkg.GetEnv("CLICKHOUSE_MIGRATIONS", defaultClickHouseMigrations),
	}
}

// LoadEnv подхватывает переменные из файла конфигурации (обычно config.env).
// Отсутствие файла не считается ошибкой: значения могут прийти из окружения.
func LoadEnv(path string) error {
	if path == "" {
		return nil
	}

	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("check config file %q: %w", path, err)
	}

	if err := godotenv.Load(path); err != nil {
		return fmt.Errorf("load config file %q: %w", path, err)
	}

	return nil
}