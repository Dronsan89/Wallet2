// Package config загружает настройки сервиса из config.env и переменных окружения.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/pkg"
)

// Config — настройки gw-currency-wallet.
type Config struct {
	HTTPPort        string
	ShutdownTimeout time.Duration
	MigrationsPath  string

	DatabaseDSN string
	DBMaxConns  int32
	DBMinConns  int32

	JWTSecret string
	JWTTTL    time.Duration

	ExchangerAddr    string
	ExchangerTimeout time.Duration
	RatesCacheTTL    time.Duration

	KafkaBrokers []string
	KafkaTopic   string

	BigTransferThreshold decimal.Decimal
}

// New собирает конфигурацию из переменных окружения.
func New() *Config {
	return &Config{
		HTTPPort:        pkg.GetEnv("HTTP_PORT", defaultHTTPPort),
		ShutdownTimeout: pkg.GetEnvDuration("SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		MigrationsPath:  pkg.GetEnv("MIGRATIONS_PATH", defaultMigrationsPath),

		DatabaseDSN: buildDSN(),
		DBMaxConns:  int32(pkg.GetEnvInt("DB_MAX_CONNS", defaultDBMaxConns)),
		DBMinConns:  int32(pkg.GetEnvInt("DB_MIN_CONNS", defaultDBMinConns)),

		JWTSecret: pkg.GetEnv("JWT_SECRET", defaultJWTSecret),
		JWTTTL:    pkg.GetEnvDuration("JWT_TTL", defaultJWTTTL),

		ExchangerAddr:    pkg.GetEnv("EXCHANGER_GRPC_ADDR", defaultExchangerAddr),
		ExchangerTimeout: pkg.GetEnvDuration("EXCHANGER_TIMEOUT", defaultExchangerTimeout),
		RatesCacheTTL:    pkg.GetEnvDuration("RATES_CACHE_TTL", defaultRatesCacheTTL),

		KafkaBrokers: pkg.GetEnvList("KAFKA_BROKERS", []string{defaultKafkaBroker}),
		KafkaTopic:   pkg.GetEnv("KAFKA_TOPIC", defaultKafkaTopic),

		BigTransferThreshold: pkg.GetEnvDecimal("BIG_TRANSFER_THRESHOLD", defaultBigTransferThreshold),
	}
}

// buildDSN собирает строку подключения: DATABASE_URL приоритетнее отдельных DB_*.
func buildDSN() string {
	if url := pkg.GetEnv("DATABASE_URL", ""); url != "" {
		return url
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		pkg.GetEnv("DB_USER", defaultDBUser),
		pkg.GetEnv("DB_PASSWORD", defaultDBPassword),
		pkg.GetEnv("DB_HOST", defaultDBHost),
		pkg.GetEnv("DB_PORT", defaultDBPort),
		pkg.GetEnv("DB_NAME", defaultDBName),
		pkg.GetEnv("DB_SSLMODE", defaultDBSSLMode),
	)
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