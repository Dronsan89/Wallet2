// Package aggregator отвечает за периоды и окна агрегации.
package aggregator

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// SupportedPeriods — периоды, которые требует ТЗ.
var SupportedPeriods = []string{"1m", "5m", "1h", "1d", "1w"}

// Ошибки разбора параметров.
var (
	ErrInvalidPeriod = errors.New("invalid period")
	ErrInvalidStep   = errors.New("invalid step")
)

// minStep — минимальный шаг временного ряда.
const minStep = time.Second

// ParsePeriod переводит строку периода ("1m", "5m", "1h", "1d", "1w") в длительность.
func ParsePeriod(raw string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1m":
		return time.Minute, nil
	case "5m":
		return 5 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "1d":
		return 24 * time.Hour, nil
	case "1w":
		return 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("%w: %q", ErrInvalidPeriod, raw)
	}
}

// DefaultStep подбирает шаг для timeseries: примерно 60 точек на период,
// но не меньше 10 секунд и не больше часа.
func DefaultStep(period time.Duration) time.Duration {
	step := period / 60
	if step < 10*time.Second {
		step = 10 * time.Second
	}
	if step > time.Hour {
		step = time.Hour
	}

	return step.Truncate(time.Second)
}

// ParseStep разбирает шаг ("30s", "1m") и проверяет границы: от 1s до периода.
func ParseStep(raw string, period time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultStep(period), nil
	}

	step, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidStep, raw)
	}

	if step < minStep || step > period {
		return 0, fmt.Errorf("%w: %q (допустимо от 1s до %s)", ErrInvalidStep, raw, period)
	}

	return step, nil
}

// Window возвращает границы окна [from, to): последние period до now.
func Window(now time.Time, period time.Duration) (time.Time, time.Time) {
	to := now.UTC()

	return to.Add(-period), to
}