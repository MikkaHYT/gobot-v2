package bot

import (
	"context"
	"sync"
	"time"

	"gobot/internal/helpers"
)

const shardCount = 32

func shardIndex(key string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return h % shardCount
}

type userRateLimitState struct {
	timestamps    []time.Time
	violations    int
	cooldownUntil time.Time
	lastViolation time.Time
	lastActivity  time.Time
	warnedUntil   time.Time
	spamStrikes   int
}

type rateLimitShard struct {
	mu    sync.Mutex
	users map[string]*userRateLimitState
}

type RateLimiter struct {
	shards    [shardCount]*rateLimitShard
	stopChan  chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

var GlobalRateLimiter = NewRateLimiter()

func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		stopChan: make(chan struct{}),
	}
	for i := 0; i < shardCount; i++ {
		rl.shards[i] = &rateLimitShard{
			users: make(map[string]*userRateLimitState),
		}
	}
	return rl
}

func (rl *RateLimiter) Start(ctx context.Context) {
	rl.startOnce.Do(func() {
		rl.wg.Add(1)
		helpers.Spawn(func() {
			rl.runCleanup(ctx, 10*time.Minute)
		})
	})
}

func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.stopChan)
		rl.wg.Wait()
	})
}

func (rl *RateLimiter) runCleanup(ctx context.Context, interval time.Duration) {
	defer rl.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			for _, shard := range rl.shards {
				shard.mu.Lock()
				for id, state := range shard.users {
					if now.Sub(state.lastActivity) > 5*time.Minute {
						delete(shard.users, id)
					}
				}
				shard.mu.Unlock()
			}
		}
	}
}

func (rl *RateLimiter) CheckLimit(guildID, userID string) (bool, time.Duration, bool) {
	key := userID
	if guildID != "" {
		key = guildID + ":" + userID
	}

	shard := rl.shards[shardIndex(key)]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	state, exists := shard.users[key]
	if !exists {
		shard.users[key] = &userRateLimitState{
			timestamps:   []time.Time{now},
			lastActivity: now,
		}
		return false, 0, false
	}

	state.lastActivity = now

	if now.Before(state.cooldownUntil) {
		state.spamStrikes++
		state.cooldownUntil = state.cooldownUntil.Add(time.Duration(state.spamStrikes) * time.Second)
		maxEnd := now.Add(30 * time.Second)
		if state.cooldownUntil.After(maxEnd) {
			state.cooldownUntil = maxEnd
		}

		remaining := state.cooldownUntil.Sub(now)
		shouldNotify := false
		if now.After(state.warnedUntil) {
			shouldNotify = true
			state.warnedUntil = state.cooldownUntil
		}
		return true, remaining, shouldNotify
	}

	if state.violations > 0 && now.Sub(state.lastViolation) > 15*time.Second {
		state.violations = 0
		state.spamStrikes = 0
	}

	cutoff := now.Add(-3 * time.Second)
	valid := state.timestamps[:0]
	for _, ts := range state.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	state.timestamps = append(valid, now)

	if len(state.timestamps) > 3 {
		state.violations++
		state.lastViolation = now
		state.spamStrikes = 0
		penalty := rl.cooldownDuration(state.violations)
		state.cooldownUntil = now.Add(penalty)
		state.warnedUntil = state.cooldownUntil
		state.timestamps = nil

		return true, penalty, true
	}

	return false, 0, false
}

func (rl *RateLimiter) cooldownDuration(violations int) time.Duration {
	switch {
	case violations >= 5:
		return 30 * time.Second
	case violations >= 4:
		return 15 * time.Second
	case violations >= 3:
		return 8 * time.Second
	case violations >= 2:
		return 4 * time.Second
	default:
		return 2 * time.Second
	}
}

type cooldownShard struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

type CooldownManager struct {
	shards    [shardCount]*cooldownShard
	stopChan  chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

var GlobalCooldowns = NewCooldownManager()

func NewCooldownManager() *CooldownManager {
	cm := &CooldownManager{
		stopChan: make(chan struct{}),
	}
	for i := 0; i < shardCount; i++ {
		cm.shards[i] = &cooldownShard{
			entries: make(map[string]time.Time),
		}
	}
	return cm
}

func (cm *CooldownManager) Start(ctx context.Context, interval time.Duration) {
	cm.startOnce.Do(func() {
		cm.wg.Add(1)
		helpers.Spawn(func() {
			cm.runCleanup(ctx, interval)
		})
	})
}

func (cm *CooldownManager) Stop() {
	cm.stopOnce.Do(func() {
		close(cm.stopChan)
		cm.wg.Wait()
	})
}

func (cm *CooldownManager) runCleanup(ctx context.Context, interval time.Duration) {
	defer cm.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-cm.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			for _, shard := range cm.shards {
				shard.mu.Lock()
				for key, expiry := range shard.entries {
					if now.After(expiry) {
						delete(shard.entries, key)
					}
				}
				shard.mu.Unlock()
			}
		}
	}
}

func (cm *CooldownManager) CheckCoolDown(userID, cmdName string, duration time.Duration) (bool, time.Duration) {
	if duration <= 0 {
		return false, 0
	}

	key := userID + ":" + cmdName
	shard := cm.shards[shardIndex(key)]
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := time.Now()
	if expiry, exists := shard.entries[key]; exists {
		if remaining := expiry.Sub(now); remaining > 0 {
			return true, remaining
		}
	}

	shard.entries[key] = now.Add(duration)
	return false, 0
}

func (cm *CooldownManager) Reset(userID, cmdName string) {
	key := userID + ":" + cmdName
	shard := cm.shards[shardIndex(key)]
	shard.mu.Lock()
	delete(shard.entries, key)
	shard.mu.Unlock()
}

var apiCooldownCommands = map[string]time.Duration{
	"leak":        10 * time.Second,
	"songinfo":    10 * time.Second,
	"snip":        10 * time.Second,
	"session":     10 * time.Second,
	"sessioninfo": 10 * time.Second,
	"cover":       10 * time.Second,

	"lf":            2 * time.Second,
	"fm":            2 * time.Second,
	"np":            2 * time.Second,
	"recents":       2 * time.Second,
	"whoknows":      5 * time.Second,
	"whoknowsalbum": 5 * time.Second,
	"whoknowstrack": 5 * time.Second,
	"chart":         3 * time.Second,
	"taste":         3 * time.Second,
	"crowns":        3 * time.Second,

	"urban":     3 * time.Second,
	"image":     3 * time.Second,
	"define":    3 * time.Second,
	"translate": 3 * time.Second,
	"ocr":       5 * time.Second,
	"rip":       5 * time.Second,
	"quote":     3 * time.Second,

	"cat":   2 * time.Second,
	"dog":   2 * time.Second,
	"bunny": 2 * time.Second,

	"steal":   3 * time.Second,
	"enlarge": 2 * time.Second,
	"emoji":   3 * time.Second,
	"sticker": 3 * time.Second,

	"avatar":             2 * time.Second,
	"serveravatar":       2 * time.Second,
	"banner":             2 * time.Second,
	"serverbanner":       2 * time.Second,
	"setbotserveravatar": 15 * time.Second,
	"setbotserverbanner": 15 * time.Second,
	"detailedserverinfo": 3 * time.Second,
	"detaileduserinfo":   3 * time.Second,

	"leaderboard": 3 * time.Second,
	"synclevels":  1 * time.Hour,

	"purge":      5 * time.Second,
	"botclear":   3 * time.Second,
	"nuke":       10 * time.Second,
	"softnuke":   10 * time.Second,
	"lockdown":   15 * time.Second,
	"unlockdown": 15 * time.Second,
}

func GetAPICooldown(cmdName string) time.Duration {
	return apiCooldownCommands[cmdName]
}
