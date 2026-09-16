package bot

import (
	"context"
	"fmt"
	"gobot/internal/helpers"
	"sync"
	"time"

	"gobot/config"
	"gobot/internal/database"
	"gobot/internal/listeners"
	"gobot/internal/logger"
	"gobot/internal/policy"
	"gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	Session     *discordgo.Session
	Config      *config.Config
	DB          *database.DB
	Policy      *policy.Module
	Radio       *radio.Module
	RateLimiter *RateLimiter
	Cooldowns   *CooldownManager
	Ctx         context.Context
	cancelFunc  context.CancelFunc
	stopOnce    sync.Once
	wg          sync.WaitGroup
}

const (
	retentionDays = 60
)

type periodicWorkerDef struct {
	name     string
	interval time.Duration
	task     func(ctx context.Context)
}

var (
	periodicWorkerRegistryMu sync.Mutex
	periodicWorkerRegistry   []periodicWorkerDef
)

func RegisterPeriodicWorker(name string, interval time.Duration, task func(ctx context.Context)) {
	if interval <= 0 || task == nil {
		return
	}
	periodicWorkerRegistryMu.Lock()
	defer periodicWorkerRegistryMu.Unlock()
	periodicWorkerRegistry = append(periodicWorkerRegistry, periodicWorkerDef{
		name:     name,
		interval: interval,
		task:     task,
	})
}

func NewBot(cfg *config.Config) (*Bot, error) {
	s, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("error creating discord session: %w", err)
	}

	db, err := database.InitDBWithOptions(cfg.DatabasePath, cfg.DBMaxOpenConns, cfg.DBBusyTimeout)
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("error initializing database: %w", err)
	}

	s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions

	ctx, cancel := context.WithCancel(context.Background())

	rateLimiter := NewRateLimiter()
	cooldowns := NewCooldownManager()

	rateLimiter.Start(ctx)
	cooldowns.Start(ctx, 10*time.Minute)

	GlobalRateLimiter = rateLimiter
	GlobalCooldowns = cooldowns

	pol := policy.New(db, policy.Options{
		ConfiguredGlobalUsers: cfg.GlobalBlacklistedUsers,
	})

	b := &Bot{
		Session:     s,
		Config:      cfg,
		DB:          db,
		Policy:      pol,
		RateLimiter: rateLimiter,
		Cooldowns:   cooldowns,
		Ctx:         ctx,
		cancelFunc:  cancel,
	}

	if cfg.SnipeExpirationHours > 0 {
		listeners.GlobalSnipeCache.SetExpirationHours(cfg.SnipeExpirationHours)
	}
	if cfg.MaxSnipeLimit > 0 {
		listeners.GlobalSnipeCache.SetMaxLimit(cfg.MaxSnipeLimit)
	}
	listeners.GlobalSnipeCache.Start(ctx)
	listeners.GlobalEmbedSessions.Start(ctx)
	listeners.GlobalPaginationStore.Start(ctx)

	b.StartPeriodicWorker("spam_tracker", 10*time.Minute, func(ctx context.Context) {
		listeners.CleanupSpamTracker()
	})

	periodicWorkerRegistryMu.Lock()
	for _, pw := range periodicWorkerRegistry {
		b.StartPeriodicWorker(pw.name, pw.interval, pw.task)
	}
	periodicWorkerRegistryMu.Unlock()
	b.Session.AddHandler(listeners.OnGuildMemberAdd(b.DB))
	b.Session.AddHandler(listeners.OnGuildMemberRemove(b.DB))
	b.Session.AddHandler(listeners.OnMessageReactionAdd(b.DB))
	listeners.DB = b.DB

	b.StartPeriodicWorker("saved_roles_pruner", 24*time.Hour, func(ctx context.Context) {
		if pruned, err := b.DB.PruneOldSavedUserRoles(retentionDays); err == nil && pruned > 0 {
			logger.Infof("[ROLES] Cleared %d expired saved user roles (older than %d days)", pruned, retentionDays)
		}
	})

	b.Session.AddHandler(listeners.OnMessageCreateForSnipe)
	b.Session.AddHandler(listeners.OnMessageDelete)
	b.Session.AddHandler(listeners.OnMessageDeleteBulk)
	b.Session.AddHandler(listeners.OnMessageUpdate)
	b.Session.AddHandler(listeners.OnReactionRoleAdd(b.Policy, b.DB))
	b.Session.AddHandler(listeners.OnReactionRoleRemove(b.DB))

	return b, nil
}

func (b *Bot) AttachRadio(radioModule *radio.Module) {
	b.Radio = radioModule
}

func (b *Bot) WG() *sync.WaitGroup {
	return &b.wg
}

func (b *Bot) StartPeriodicWorker(name string, interval time.Duration, task func(ctx context.Context)) {
	if interval <= 0 || task == nil {
		return
	}
	b.wg.Add(1)
	helpers.Spawn(func() {
		defer b.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		task(b.Ctx)
		for {
			select {
			case <-b.Ctx.Done():
				return
			case <-ticker.C:
				task(b.Ctx)
			}
		}
	})
}

func (b *Bot) Start() error {
	return b.Session.Open()
}

func (b *Bot) Stop() {
	b.stopOnce.Do(func() {
		if b.Radio != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := b.Radio.Shutdown(shutdownCtx); err != nil {
				logger.Warnf("[RADIO] Error shutting down radio module: %v", err)
			}
			shutdownCancel()
		}

		if b.cancelFunc != nil {
			b.cancelFunc()
		}

		if b.Session != nil {
			if err := b.Session.Close(); err != nil {
				logger.Warnf("[SESSION] Error closing Discord gateway session: %v", err)
			}
		}

		if b.RateLimiter != nil {
			b.RateLimiter.Stop()
		}
		if b.Cooldowns != nil {
			b.Cooldowns.Stop()
		}
		listeners.GlobalSnipeCache.Stop()
		listeners.GlobalEmbedSessions.Stop()
		listeners.GlobalPaginationStore.Stop()

		b.wg.Wait()

		if b.DB != nil {
			if err := b.DB.Close(); err != nil {
				logger.Warnf("[DATABASE] Error closing database handle: %v", err)
			}
		}
	})
}
