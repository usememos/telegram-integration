package memogram

import (
	"sync"
	"time"
)

// Cache is a simple cache implementation
type Cache struct {
	sync.RWMutex
	items map[string]*CacheItem
}

type CacheItem struct {
	Value      interface{}
	Expiration time.Time
}

func NewCache() *Cache {
	return &Cache{
		items: make(map[string]*CacheItem),
	}
}

// set adds a key value pair to the cache with a given duration
func (c *Cache) set(key string, value interface{}, duration time.Duration) {
	c.Lock()
	defer c.Unlock()
	c.items[key] = &CacheItem{
		Value:      value,
		Expiration: time.Now().Add(duration),
	}
}

// get returns a value from the cache if it exists
func (c *Cache) get(key string) (interface{}, bool) {
	c.RLock()
	defer c.RUnlock()
	item, found := c.items[key]
	if !found {
		return nil, false
	}
	if time.Now().After(item.Expiration) {
		return nil, false
	}
	return item.Value, true
}

// deleteExpired deletes all expired key value pairs
func (c *Cache) deleteExpired() {
	c.Lock()
	defer c.Unlock()
	for k, v := range c.items {
		if time.Now().After(v.Expiration) {
			delete(c.items, k)
		}
	}
}

// startGC starts a goroutine to clean expired key value pairs
func (c *Cache) startGC() {
	go func() {
		for {
			<-time.After(5 * time.Minute)
			c.deleteExpired()
		}
	}()
}
