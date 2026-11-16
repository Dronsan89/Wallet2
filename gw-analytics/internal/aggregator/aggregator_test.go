package aggregator

import (
	"errors"
	"testing"
	"time"
)

func TestParsePeriod(t *testing.T) {
	cases := map[string]time.Duration{
		"1m":  time.Minute,
		"5m":  5 * time.Minute,
		"1h":  time.Hour,
		"1d":  24 * time.Hour,
		"1w":  7 * 24 * time.Hour,
		" 1H": time.Hour,
	}

	for raw, want := range cases {
		got, err := ParsePeriod(raw)
		if err != nil {
			t.Fatalf("ParsePeriod(%q) error = %v", raw, err)
		}
		if got != want {
			t.Fatalf("ParsePeriod(%q) = %v, want %v", raw, got, want)
		}
	}

	for _, raw := range []string{"", "2m", "10s", "1y", "week"} {
		if _, err := ParsePeriod(raw); !errors.Is(err, ErrInvalidPeriod) {
			t.Fatalf("ParsePeriod(%q) error = %v, want ErrInvalidPeriod", raw, err)
		}
	}
}

func TestDefaultStep(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		time.Minute:     10 * time.Second, // 1s расчётный -> минимум 10s
		5 * time.Minute: 10 * time.Second, // 5s -> минимум 10s
		time.Hour:       time.Minute,      // 60s
		24 * time.Hour:  24 * time.Minute, // 1440s
		7 * 24 * time.Hour: time.Hour,     // 10080s -> ограничение часом
	}

	for period, want := range cases {
		if got := DefaultStep(period); got != want {
			t.Fatalf("DefaultStep(%v) = %v, want %v", period, got, want)
		}
	}
}

func TestParseStep(t *testing.T) {
	if step, err := ParseStep("", 5*time.Minute); err != nil || step != DefaultStep(5*time.Minute) {
		t.Fatalf("ParseStep(\"\") = %v, %v", step, err)
	}

	if step, err := ParseStep("30s", time.Minute); err != nil || step != 30*time.Second {
		t.Fatalf("ParseStep(30s) = %v, %v", step, err)
	}

	for _, raw := range []string{"5m", "500ms", "abc", "-1s"} {
		if _, err := ParseStep(raw, time.Minute); !errors.Is(err, ErrInvalidStep) {
			t.Fatalf("ParseStep(%q) error = %v, want ErrInvalidStep", raw, err)
		}
	}
}

func TestWindow(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

	from, to := Window(now, 5*time.Minute)
	if !to.Equal(now) {
		t.Fatalf("to = %v, want %v", to, now)
	}
	if !from.Equal(now.Add(-5 * time.Minute)) {
		t.Fatalf("from = %v, want %v", from, now.Add(-5*time.Minute))
	}

	from, to = Window(now, 7*24*time.Hour)
	if to.Sub(from) != 7*24*time.Hour {
		t.Fatalf("ширина окна = %v, want 168h", to.Sub(from))
	}
}