package cache

import (
	"container/list"
	"sync"
	"time"
)

// CacheEntry represents a cached response
type CacheEntry struct {
	Key        string
	Value      []byte
	Expiration time.Time
}

// LRUCache implements a thread-safe LRU (Least Recently Used) cache
type LRUCache struct {
	capacity  int
	items     map[string]*list.Element
	evictList *list.List
	mu        sync.RWMutex
	hits      int64
	misses    int64
}

// NewLRUCache creates a new LRU cache with the specified capacity
func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity:  capacity,
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

// Get retrieves a value from the cache
func (c *LRUCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*CacheEntry)

		// Check if expired
		if !entry.Expiration.IsZero() && time.Now().After(entry.Expiration) {
			c.removeElement(elem)
			c.misses++
			return nil, false
		}

		// Move to front (most recently used)
		c.evictList.MoveToFront(elem)
		c.hits++
		return entry.Value, true
	}

	c.misses++
	return nil, false
}

// Set stores a value in the cache with optional TTL
func (c *LRUCache) Set(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update and move to front
	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*CacheEntry)
		entry.Value = value
		if ttl > 0 {
			entry.Expiration = time.Now().Add(ttl)
		} else {
			entry.Expiration = time.Time{}
		}
		c.evictList.MoveToFront(elem)
		return
	}

	// Add new entry
	entry := &CacheEntry{
		Key:        key,
		Value:      value,
		Expiration: time.Time{},
	}
	if ttl > 0 {
		entry.Expiration = time.Now().Add(ttl)
	}

	elem := c.evictList.PushFront(entry)
	c.items[key] = elem

	// Evict if over capacity
	if c.evictList.Len() > c.capacity {
		c.evictOldest()
	}
}

// Delete removes a key from the cache
func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
	}
}

// Clear removes all entries from the cache
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.evictList.Init()
	c.hits = 0
	c.misses = 0
}

// Size returns the number of items in the cache
func (c *LRUCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.evictList.Len()
}

// Stats returns cache hit/miss statistics
func (c *LRUCache) Stats() (hits, misses int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hits, c.misses
}

// removeElement removes an element from the cache
func (c *LRUCache) removeElement(elem *list.Element) {
	c.evictList.Remove(elem)
	entry := elem.Value.(*CacheEntry)
	delete(c.items, entry.Key)
}

// evictOldest removes the oldest (least recently used) item
func (c *LRUCache) evictOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// CleanExpired removes all expired entries
func (c *LRUCache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for _, elem := range c.items {
		entry := elem.Value.(*CacheEntry)
		if !entry.Expiration.IsZero() && now.After(entry.Expiration) {
			c.removeElement(elem)
		}
	}
}
