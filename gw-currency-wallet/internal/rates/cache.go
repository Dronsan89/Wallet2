package rates

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Cache — TTL-кэш курсов. Пока данные свежие, обмен берёт курс из памяти
// и не обращается к сервису курсов (требование ТЗ).
type Cache struct {
	provider Provider
	ttl      time.Duration
	now      func() time.Time

	mu        sync.RWMutex
	cached    Rates
	expiresAt time.Time

	hits   atomic.Int64
	misses atomic.Int64
}

// NewCache создаёт кэш с временем жизни ttl.
func NewCache(provider Provider, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}

	return &Cache{
		provider: provider,
		ttl:      ttl,
		now:      time.Now,
	}
}

// Rates возвращает курсы из кэша или запрашивает их у провайдера.
func (c *Cache) Rates(ctx context.Context) (Rates, error) {
	now := c.now()

	c.mu.RLock()
	fresh := c.cached != nil && now.Before(c.expiresAt)
	cached := c.cached
	c.mu.RUnlock()

	if fresh {
		c.hits.Add(1)
		return cached, nil
	}

	c.misses.Add(1)

	rates, err := c.provider.Rates(ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cached = rates
	c.expiresAt = now.Add(c.ttl)
	c.mu.Unlock()

	return rates, nil
}

// Stats возвращает количество попаданий и промахов кэша.
func (c *Cache) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// TTL возвращает время жизни записи кэша.
func (c *Cache) TTL() time.Duration { return c.ttl }