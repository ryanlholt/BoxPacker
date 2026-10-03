package boxpacker

import "sync"

// boundedCache retains lock-free hits while limiting process-wide retention.
// FIFO eviction affects performance only; every value can be recomputed.
type boundedCache struct {
	values   sync.Map
	mutex    sync.Mutex
	keys     []string
	next     int
	capacity int
}

func newBoundedCache(capacity int) *boundedCache {
	return &boundedCache{capacity: capacity}
}

func (c *boundedCache) Load(key string) (any, bool) { return c.values.Load(key) }

func (c *boundedCache) Store(key string, value any) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if _, exists := c.values.Load(key); exists {
		return
	}
	if len(c.keys) < c.capacity {
		c.keys = append(c.keys, key)
	} else {
		c.values.Delete(c.keys[c.next])
		c.keys[c.next] = key
		c.next = (c.next + 1) % c.capacity
	}
	c.values.Store(key, value)
}
