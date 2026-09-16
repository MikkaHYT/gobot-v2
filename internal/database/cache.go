package database

import (
	"sync"
	"time"
)

const defaultCacheTTL = 5 * time.Minute

type cachedGuildConfig struct {
	config    *GuildConfig
	fetchedAt time.Time
}

type ConfigCache struct {
	mu      sync.RWMutex
	entries map[string]*cachedGuildConfig
	ttl     time.Duration
}

func NewConfigCache(ttl time.Duration) *ConfigCache {
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	return &ConfigCache{
		entries: make(map[string]*cachedGuildConfig),
		ttl:     ttl,
	}
}

func (c *ConfigCache) Get(guildID string) (*GuildConfig, bool) {
	if guildID == "" {
		return nil, false
	}

	c.mu.RLock()
	entry, exists := c.entries[guildID]
	c.mu.RUnlock()

	if !exists || entry == nil || entry.config == nil {
		return nil, false
	}

	if time.Since(entry.fetchedAt) > c.ttl {
		c.Invalidate(guildID)
		return nil, false
	}

	return entry.config.Clone(), true
}

func (c *ConfigCache) Set(guildID string, cfg *GuildConfig) {
	if guildID == "" || cfg == nil {
		return
	}

	c.mu.Lock()
	c.entries[guildID] = &cachedGuildConfig{
		config:    cfg.Clone(),
		fetchedAt: time.Now(),
	}
	c.mu.Unlock()
}

func (c *ConfigCache) Invalidate(guildID string) {
	if guildID == "" {
		return
	}

	c.mu.Lock()
	delete(c.entries, guildID)
	c.mu.Unlock()
}

func (c *ConfigCache) Clear() {
	c.mu.Lock()
	c.entries = make(map[string]*cachedGuildConfig)
	c.mu.Unlock()
}
