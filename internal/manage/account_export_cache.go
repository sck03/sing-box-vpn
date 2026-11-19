package manage

import (
	"container/list"
	"context"
	"sync"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"golang.org/x/sync/singleflight"
)

const accountCacheBytes = 16 << 20
const accountCacheEntries = 128
const accountCacheTTL = 5 * time.Minute

type accountCompilation struct {
	artifact sub.Artifact
	option   AccountClientOption
}

type accountCacheEntry struct {
	key     string
	value   accountCompilation
	expires time.Time
	bytes   int
}

// Only immutable compiler results are cached. Authorization, usage and core
// validation are evaluated for each request, outside this cache.
type accountExportCache struct {
	mu      sync.Mutex
	entries map[string]*list.Element
	lru     list.List
	bytes   int
	flights singleflight.Group
}

func (c *accountExportCache) remove(e *list.Element) {
	v := e.Value.(accountCacheEntry)
	delete(c.entries, v.key)
	c.bytes -= v.bytes
	c.lru.Remove(e)
}

func (c *accountExportCache) get(key string) (accountCompilation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.entries[key]; e != nil {
		v := e.Value.(accountCacheEntry)
		if time.Now().Before(v.expires) {
			c.lru.MoveToFront(e)
			return v.value, true
		}
		c.remove(e)
	}
	return accountCompilation{}, false
}

func (c *accountExportCache) remember(key string, value accountCompilation) {
	size := len(value.artifact.Data) + 1024
	for _, d := range value.option.Diagnostics {
		size += len(d.NodeID) + len(d.Name) + len(d.Stage) + len(d.Message) + 64
	}
	if size > accountCacheBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]*list.Element{}
	}
	if old := c.entries[key]; old != nil {
		c.remove(old)
	}
	for e := c.lru.Back(); e != nil; {
		previous := e.Prev()
		if !time.Now().Before(e.Value.(accountCacheEntry).expires) {
			c.remove(e)
		}
		e = previous
	}
	for len(c.entries) >= accountCacheEntries || c.bytes+size > accountCacheBytes {
		c.remove(c.lru.Back())
	}
	c.entries[key] = c.lru.PushFront(accountCacheEntry{key, value, time.Now().Add(accountCacheTTL), size})
	c.bytes += size
}

func (c *accountExportCache) compile(ctx context.Context, key string, build func() (accountCompilation, error)) (accountCompilation, error) {
	if err := ctx.Err(); err != nil {
		return accountCompilation{}, err
	}
	if c == nil {
		return build()
	}
	if value, ok := c.get(key); ok {
		return value, nil
	}
	result := c.flights.DoChan(key, func() (any, error) {
		if value, ok := c.get(key); ok {
			return value, nil
		}
		value, err := build()
		if err == nil {
			c.remember(key, value)
		}
		return value, err
	})
	select {
	case <-ctx.Done():
		return accountCompilation{}, ctx.Err()
	case result := <-result:
		return result.Val.(accountCompilation), result.Err
	}
}
