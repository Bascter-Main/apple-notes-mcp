package reader

import (
	"context"
	"fmt"
	"sync"
	"time"

	"example.com/icloud-notes-mcp/internal/model"
)

// Cache is in-process only. No copies of Notes are written to disk.
type Cache struct {
	src        model.Source
	ttl        time.Duration
	mu         sync.Mutex
	snap       model.Snapshot
	validUntil time.Time
}

func NewCache(src model.Source, ttl time.Duration) *Cache { return &Cache{src: src, ttl: ttl} }

func (c *Cache) Read(ctx context.Context) (model.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.validUntil.IsZero() && time.Now().Before(c.validUntil) {
		return c.snap, nil
	}
	snap, err := c.src.Fetch(ctx)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("iCloud read failed: %w", err)
	}
	if snap.FetchedAt.IsZero() {
		snap.FetchedAt = time.Now().UTC()
	}
	c.snap = snap
	c.validUntil = time.Now().Add(c.ttl)
	return snap, nil
}
