package cache

import (
	"sync"
	"time"
)

// Item holds a cached value with expiration.
type Item struct {
	Value  any
	Expiry time.Time
}

// Cache is a simple TTL-based in-memory cache.
type Cache struct {
	items map[string]Item
	mu    sync.RWMutex
	ttl   time.Duration
	stop  chan struct{}
}

// New creates a new cache with the given default TTL and starts a cleanup
// goroutine that evicts expired entries every minute.
func New(ttl time.Duration) *Cache {
	c := &Cache{
		items: make(map[string]Item),
		ttl:   ttl,
		stop:  make(chan struct{}),
	}
	go c.cleanup()
	return c
}

// Get retrieves a cached value by key. Returns nil and false if not found or expired.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !item.Expiry.IsZero() && time.Now().After(item.Expiry) {
		c.Delete(key)
		return nil, false
	}
	return item.Value, true
}

// Set stores a value with the default TTL.
func (c *Cache) Set(key string, value any) {
	c.mu.Lock()
	c.items[key] = Item{
		Value:  value,
		Expiry: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// SetWithTTL stores a value with a specific TTL.
func (c *Cache) SetWithTTL(key string, value any, ttl time.Duration) {
	c.mu.Lock()
	c.items[key] = Item{
		Value:  value,
		Expiry: time.Now().Add(ttl),
	}
	c.mu.Unlock()
}

// Delete removes a key from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// Clear empties the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	c.items = make(map[string]Item)
	c.mu.Unlock()
}

// Stop terminates the cleanup goroutine.
func (c *Cache) Stop() {
	close(c.stop)
}

// cleanup periodically evicts expired entries.
func (c *Cache) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.evictExpired()
		case <-c.stop:
			return
		}
	}
}

func (c *Cache) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	for k, v := range c.items {
		if !v.Expiry.IsZero() && now.After(v.Expiry) {
			delete(c.items, k)
		}
	}
	c.mu.Unlock()
}
