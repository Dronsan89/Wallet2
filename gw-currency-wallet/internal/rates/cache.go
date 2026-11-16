package rates

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Cache — TTL-кэш курсов. Пока данные свежие, обмен берёт курс из памяти
// и не обращается к сервису курсов (требование ТЗ).
type Cache struct {
	provider Provider
	ttl      time.Duration
	log      *slog.Logger
	now      func() time.Time

	mu        sync.RWMutex
	cached    Rates
	expiresAt time.Time

	hits   atomic.Int64
	misses atomic.Int64
}

// NewCache создаёт кэш с временем жизни ttl. log может быть nil — тогда
// используется логгер по умолчанию.
func NewCache(provider Provider, ttl time.Duration, log *slog.Logger) *Cache {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}

	return &Cache{
		provider: provider,
		ttl:      ttl,
		log:      log,
		now:      time.Now,
	}
}

// Rates возвращает курсы из кэша или запрашивает их у провайдера.
func (c *Cache) Rates(ctx context.Context) (Rates, error) {
	now := c.now()

	c.mu.RLock()
	fresh := c.cached != nil && now.Before(c.expiresAt)
	cached := c.cached
	expiresAt := c.expiresAt
	c.mu.RUnlock()

	if fresh {
		c.hits.Add(1)
		c.log.Debug("rates cache hit",
			"expires_in", expiresAt.Sub(now).Round(time.Millisecond).String(),
		)

		return cached, nil
	}

	c.misses.Add(1)
	c.log.Debug("rates cache miss", "ttl", c.ttl.String())

	rates, err := c.provider.Rates(ctx)
	if err != nil {
		c.log.Warn("rates provider failed", "error", err)
		return nil, err
	}

	c.mu.Lock()
	c.cached = rates
	c.expiresAt = now.Add(c.ttl)
	c.mu.Unlock()

	c.log.Debug("rates cache updated",
		"currencies", len(rates),
		"ttl", c.ttl.String(),
	)

	return rates, nil
}

// Stats возвращает количество попаданий и промахов кэша.
func (c *Cache) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// TTL возвращает время жизни записи кэша.
func (c *Cache) TTL() time.Duration { return c.ttl }
