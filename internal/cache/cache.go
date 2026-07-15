// Package cache provides a small TTL cache for assert results.
package cache

import (
	"sync"
	"time"
)

type Result struct {
	Allowed bool
	Reason  string
}

type TTL struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]entry
}

type entry struct {
	result  Result
	expires time.Time
}

func New(ttl time.Duration) *TTL {
	c := &TTL{ttl: ttl, entries: map[string]entry{}}
	if ttl > 0 {
		go func() {
			for range time.Tick(ttl) {
				now := time.Now()
				c.mu.Lock()
				for k, e := range c.entries {
					if now.After(e.expires) {
						delete(c.entries, k)
					}
				}
				c.mu.Unlock()
			}
		}()
	}
	return c
}

func (c *TTL) Get(key string) (Result, bool) {
	if c.ttl <= 0 {
		return Result{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expires) {
		return Result{}, false
	}
	return e.result, true
}

func (c *TTL) Set(key string, r Result) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry{result: r, expires: time.Now().Add(c.ttl)}
}
