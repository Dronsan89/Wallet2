// Package pkg содержит общие утилиты сервиса: чтение переменных окружения
// и разбор значений из config.env.
package pkg

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// GetEnv возвращает значение переменной окружения или fallback, если она не задана.
func GetEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// GetEnvInt читает целое число; при ошибке разбора возвращает fallback.
func GetEnvInt(key string, fallback int) int {
	v := GetEnv(key, "")
	if v == "" {
		return fallback
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// GetEnvBool читает булево значение; при ошибке разбора возвращает fallback.
func GetEnvBool(key string, fallback bool) bool {
	v := GetEnv(key, "")
	if v == "" {
		return fallback
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// GetEnvDuration читает длительность в формате time.ParseDuration (например, "5s").
func GetEnvDuration(key string, fallback time.Duration) time.Duration {
	v := GetEnv(key, "")
	if v == "" {
		return fallback
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// GetEnvList читает список значений через запятую (например, адреса Kafka).
func GetEnvList(key string, fallback []string) []string {
	v := GetEnv(key, "")
	if v == "" {
		return fallback
	}

	parts := strings.Split(v, ",")
	values := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			values = append(values, p)
		}
	}

	if len(values) == 0 {
		return fallback
	}
	return values
}