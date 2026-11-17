package config

import "time"

// Значения по умолчанию для конфигурации notification.
const (
	defaultHTTPPort        = "8082"
	defaultShutdownTimeout = 20 * time.Second

	defaultKafkaBroker        = "localhost:9092"
	defaultKafkaTopic         = "wallet.transactions"
	defaultKafkaGroup         = "gw-notification"
	defaultKafkaDLQTopic      = "wallet.transactions.dlq"
	defaultKafkaBatchSize     = 500
	defaultKafkaPollTimeout   = 500 * time.Millisecond
	defaultKafkaCommitTimeout = 5 * time.Second
	defaultKafkaMaxRetries    = 5
	defaultKafkaRetryBackoff  = 200 * time.Millisecond
	// defaultKafkaIdleReconnect — через сколько простоя пересоздаём reader:
	// лечит зависшую сессию consumer group после падения брокера.
	defaultKafkaIdleReconnect = 2 * time.Minute

	defaultMongoURI            = "mongodb://localhost:27017"
	defaultMongoDB             = "notification"
	defaultMongoCollection     = "transfers"
	defaultMongoConnectTimeout = 5 * time.Second

	// DefaultStartTimeout — сколько ждём запуска приложения.
	DefaultStartTimeout = 30 * time.Second
	// DefaultStopTimeout — сколько ждём корректной остановки.
	DefaultStopTimeout = 20 * time.Second
)
