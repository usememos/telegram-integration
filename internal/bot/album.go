package bot

import (
	"context"
	"sync"
	"time"

	"github.com/usememos/memogram/internal/memosapi"
)

// albumTTL bounds how long an album's memo is remembered. Telegram delivers
// the parts of an album together, so this only needs to outlast one batch of
// updates; expiry keeps memory bounded on long-running instances.
const albumTTL = time.Hour

// mediaGroups maps each album to the memo created for its first part, so that
// later parts attach to the same memo. Parts are handled concurrently.
type mediaGroups struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]*mediaGroup
}

type mediaGroup struct {
	done    chan struct{}  // closed when creation finishes
	memo    *memosapi.Memo // set before done is closed; nil if creation failed
	expires time.Time
}

func newMediaGroups(ttl time.Duration) *mediaGroups {
	return &mediaGroups{ttl: ttl, now: time.Now, entries: map[string]*mediaGroup{}}
}

// memo returns the album's memo, calling create for the first part. Parts that
// arrive meanwhile wait for that call instead of creating duplicates, without
// blocking other albums. If creation fails, the next part tries again.
func (g *mediaGroups) memo(ctx context.Context, key string, create func() (*memosapi.Memo, error)) (*memosapi.Memo, error) {
	for {
		g.mu.Lock()
		g.evictExpired()
		entry, ok := g.entries[key]
		if !ok {
			entry = &mediaGroup{done: make(chan struct{})}
			g.entries[key] = entry
			g.mu.Unlock()
			return g.create(key, entry, create)
		}
		g.mu.Unlock()

		select {
		case <-entry.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if entry.memo != nil {
			return entry.memo, nil
		}
	}
}

func (g *mediaGroups) create(key string, entry *mediaGroup, create func() (*memosapi.Memo, error)) (memo *memosapi.Memo, err error) {
	defer func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if memo == nil {
			delete(g.entries, key)
		} else {
			entry.memo = memo
			entry.expires = g.now().Add(g.ttl)
		}
		close(entry.done)
	}()
	return create()
}

// evictExpired forgets finished albums past their TTL. The caller must hold
// g.mu.
func (g *mediaGroups) evictExpired() {
	now := g.now()
	for key, entry := range g.entries {
		if entry.memo != nil && now.After(entry.expires) {
			delete(g.entries, key)
		}
	}
}
