package cache

import (
	"testing"
	"time"
)

func TestNewLRUCache(t *testing.T) {
	cache := NewLRUCache(100)
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
	if cache.capacity != 100 {
		t.Errorf("expected capacity 100, got %d", cache.capacity)
	}
	if cache.Size() != 0 {
		t.Errorf("expected size 0, got %d", cache.Size())
	}
}

func TestLRUCache_SetAndGet(t *testing.T) {
	cache := NewLRUCache(10)

	// Set a value
	cache.Set("key1", []byte("value1"), 0)

	// Get the value
	val, found := cache.Get("key1")
	if !found {
		t.Error("expected to find key1")
	}
	if string(val) != "value1" {
		t.Errorf("expected value1, got %s", string(val))
	}
}

func TestLRUCache_GetNonExistent(t *testing.T) {
	cache := NewLRUCache(10)

	val, found := cache.Get("nonexistent")
	if found {
		t.Error("expected not to find nonexistent key")
	}
	if val != nil {
		t.Errorf("expected nil value, got %s", string(val))
	}
}

func TestLRUCache_TTLExpiration(t *testing.T) {
	cache := NewLRUCache(10)

	// Set a value with 100ms TTL
	cache.Set("key1", []byte("value1"), 100*time.Millisecond)

	// Should be found immediately
	val, found := cache.Get("key1")
	if !found {
		t.Error("expected to find key1 before expiration")
	}
	if string(val) != "value1" {
		t.Errorf("expected value1, got %s", string(val))
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should not be found after expiration
	_, found = cache.Get("key1")
	if found {
		t.Error("expected key1 to be expired")
	}
}

func TestLRUCache_CapacityEviction(t *testing.T) {
	cache := NewLRUCache(2)

	// Add 3 items (exceeds capacity)
	cache.Set("key1", []byte("value1"), 0)
	cache.Set("key2", []byte("value2"), 0)
	cache.Set("key3", []byte("value3"), 0)

	// key1 should be evicted (least recently used)
	_, found := cache.Get("key1")
	if found {
		t.Error("expected key1 to be evicted")
	}

	// key2 and key3 should still exist
	_, found = cache.Get("key2")
	if !found {
		t.Error("expected to find key2")
	}
	_, found = cache.Get("key3")
	if !found {
		t.Error("expected to find key3")
	}
}

func TestLRUCache_LRUOrder(t *testing.T) {
	cache := NewLRUCache(2)

	// Add 2 items
	cache.Set("key1", []byte("value1"), 0)
	cache.Set("key2", []byte("value2"), 0)

	// Access key1 to make it more recently used
	cache.Get("key1")

	// Add key3, should evict key2 (least recently used)
	cache.Set("key3", []byte("value3"), 0)

	// key1 should still exist (was accessed)
	_, found := cache.Get("key1")
	if !found {
		t.Error("expected to find key1 (was recently accessed)")
	}

	// key2 should be evicted
	_, found = cache.Get("key2")
	if found {
		t.Error("expected key2 to be evicted")
	}
}

func TestLRUCache_UpdateExisting(t *testing.T) {
	cache := NewLRUCache(10)

	// Set initial value
	cache.Set("key1", []byte("value1"), 0)

	// Update with new value
	cache.Set("key1", []byte("updated"), 0)

	// Should get updated value
	val, found := cache.Get("key1")
	if !found {
		t.Error("expected to find key1")
	}
	if string(val) != "updated" {
		t.Errorf("expected updated, got %s", string(val))
	}

	// Size should still be 1
	if cache.Size() != 1 {
		t.Errorf("expected size 1, got %d", cache.Size())
	}
}

func TestLRUCache_Delete(t *testing.T) {
	cache := NewLRUCache(10)

	cache.Set("key1", []byte("value1"), 0)
	cache.Delete("key1")

	_, found := cache.Get("key1")
	if found {
		t.Error("expected key1 to be deleted")
	}
}

func TestLRUCache_Clear(t *testing.T) {
	cache := NewLRUCache(10)

	cache.Set("key1", []byte("value1"), 0)
	cache.Set("key2", []byte("value2"), 0)
	cache.Set("key3", []byte("value3"), 0)

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", cache.Size())
	}

	_, found := cache.Get("key1")
	if found {
		t.Error("expected key1 to be cleared")
	}
}

func TestLRUCache_Stats(t *testing.T) {
	cache := NewLRUCache(10)

	// Initial stats
	hits, misses := cache.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("expected 0 hits and misses, got %d hits, %d misses", hits, misses)
	}

	// Add and get (hit)
	cache.Set("key1", []byte("value1"), 0)
	cache.Get("key1")

	hits, _ = cache.Stats()
	if hits != 1 {
		t.Errorf("expected 1 hit, got %d", hits)
	}

	// Get non-existent (miss)
	cache.Get("nonexistent")

	hits, misses = cache.Stats()
	if hits != 1 {
		t.Errorf("expected 1 hit, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("expected 1 miss, got %d", misses)
	}
}

func TestLRUCache_ClearResetsStats(t *testing.T) {
	cache := NewLRUCache(10)

	// Generate some hits/misses
	cache.Set("key1", []byte("value1"), 0)
	cache.Get("key1")
	cache.Get("nonexistent")

	// Clear should reset stats
	cache.Clear()

	hits, misses := cache.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("expected stats reset to 0, got %d hits, %d misses", hits, misses)
	}
}

func TestLRUCache_CleanExpired(t *testing.T) {
	cache := NewLRUCache(10)

	// Set expired and non-expired items
	cache.Set("expired", []byte("data"), 1*time.Millisecond)
	cache.Set("active", []byte("data"), 10*time.Minute)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Clean expired
	cache.CleanExpired()

	// Expired should be gone
	_, found := cache.Get("expired")
	if found {
		t.Error("expected expired item to be cleaned")
	}

	// Active should remain
	_, found = cache.Get("active")
	if !found {
		t.Error("expected active item to remain")
	}
}

func TestLRUCache_ConcurrentAccess(t *testing.T) {
	cache := NewLRUCache(100)

	// Run concurrent operations
	done := make(chan bool)

	// Writer
	go func() {
		for i := 0; i < 100; i++ {
			cache.Set(string(rune(i)), []byte("data"), 0)
		}
		done <- true
	}()

	// Reader
	go func() {
		for i := 0; i < 100; i++ {
			cache.Get(string(rune(i)))
		}
		done <- true
	}()

	// Wait for both
	<-done
	<-done

	// Should not panic and should have valid state
	size := cache.Size()
	if size < 0 || size > 100 {
		t.Errorf("unexpected cache size: %d", size)
	}
}
