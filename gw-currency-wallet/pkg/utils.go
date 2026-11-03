// Package pkg содержит общие утилиты сервиса: чтение переменных окружения,
// работу с кодами валют и деньгами.
package pkg

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// SupportedCurrencies — валюты, которые поддерживает сервис.
var SupportedCurrencies = []string{"USD", "RUB", "EUR"}

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

// GetEnvDecimal читает десятичное число (деньги, курсы); при ошибке возвращает fallback.
func GetEnvDecimal(key string, fallback decimal.Decimal) decimal.Decimal {
	v := GetEnv(key, "")
	if v == "" {
		return fallback
	}

	d, err := decimal.NewFromString(v)
	if err != nil {
		return fallback
	}
	return d
}

// NormalizeCurrency приводит код валюты к верхнему регистру без пробелов.
func NormalizeCurrency(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// IsSupportedCurrency сообщает, поддерживается ли валюта сервисом.
func IsSupportedCurrency(code string) bool {
	code = NormalizeCurrency(code)
	for _, c := range SupportedCurrencies {
		if c == code {
			return true
		}
	}
	return false
}

// RoundMoney округляет денежную сумму half-up до двух знаков.
func RoundMoney(d decimal.Decimal) decimal.Decimal {
	return d.Round(2)
}