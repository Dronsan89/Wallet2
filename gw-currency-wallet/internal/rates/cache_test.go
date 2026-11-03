package rates

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

type stubProvider struct {
	rates Rates
	err   error
	calls int
}

func (s *stubProvider) Rates(context.Context) (Rates, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}

	return s.rates, nil
}

func TestCacheServesFreshRatesFromMemory(t *testing.T) {
	now := time.Now()
	provider := &stubProvider{rates: Rates{"USD": decimal.NewFromInt(90)}}

	cache := NewCache(provider, time.Second)
	cache.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if _, err := cache.Rates(context.Background()); err != nil {
			t.Fatalf("Rates() error = %v", err)
		}
	}

	if provider.calls != 1 {
		t.Fatalf("провайдер вызван %d раз, want 1", provider.calls)
	}

	hits, misses := cache.Stats()
	if hits != 2 || misses != 1 {
		t.Fatalf("stats = (%d, %d), want (2, 1)", hits, misses)
	}
}

func TestCacheRefreshesAfterTTL(t *testing.T) {
	now := time.Now()
	provider := &stubProvider{rates: Rates{"USD": decimal.NewFromInt(90)}}

	cache := NewCache(provider, time.Second)
	cache.now = func() time.Time { return now }

	if _, err := cache.Rates(context.Background()); err != nil {
		t.Fatalf("Rates() error = %v", err)
	}

	now = now.Add(2 * time.Second)

	if _, err := cache.Rates(context.Background()); err != nil {
		t.Fatalf("Rates() после TTL error = %v", err)
	}

	if provider.calls != 2 {
		t.Fatalf("провайдер вызван %d раз, want 2", provider.calls)
	}
}

func TestCacheDoesNotCacheErrors(t *testing.T) {
	provider := &stubProvider{err: errors.New("exchanger is down")}
	cache := NewCache(provider, time.Minute)

	for i := 0; i < 2; i++ {
		if _, err := cache.Rates(context.Background()); err == nil {
			t.Fatal("ожидалась ошибка провайдера")
		}
	}

	if provider.calls != 2 {
		t.Fatalf("провайдер вызван %d раз, want 2", provider.calls)
	}
}