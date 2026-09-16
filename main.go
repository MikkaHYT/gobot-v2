package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gobot/config"
	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/commands/filter"
	"gobot/internal/commands/giveaway"
	cmdjw "gobot/internal/commands/juicewrld"
	"gobot/internal/commands/lastfm"
	"gobot/internal/commands/leveling"
	"gobot/internal/commands/moderation"
	"gobot/internal/commands/radio"
	"gobot/internal/commands/ripper"
	"gobot/internal/commands/utility"
	"gobot/internal/commands/voice"
	"gobot/internal/graphics"
	"gobot/internal/handler"
	"gobot/internal/helpers"
	"gobot/internal/juicewrld"
	coordinator "gobot/internal/radio"

	"gobot/internal/listeners"
	"gobot/internal/logger"
	"gobot/internal/policy"

	"github.com/bwmarrin/discordgo"
)

//go:generate go run ./cmd/gendocs

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		if errors.Is(err, config.ErrDiscordTokenMissing) {
			fmt.Fprintln(os.Stderr, "[STARTUP ERROR] No Discord bot token was found.")
			fmt.Fprintln(os.Stderr, "Set DISCORD_TOKEN in your environment or add DISCORD_TOKEN=your_token to .env.")
			fmt.Fprintln(os.Stderr, "The bot will shut down in 30 seconds. Press Ctrl+C to exit now.")
			time.Sleep(30 * time.Second)
			return
		}
		bot.Fatalf("Failed to load configuration: %v", err)
	}

	bot.InitLogger(cfg.LogLevel, cfg.ConsoleWebhookURL)
	bot.Infof("Initializing Bot (Log Level: %s)...", cfg.LogLevel)
	warnUnavailableBinaries()

	b, err := bot.NewBot(cfg)

	if err != nil {
		bot.Fatalf("Failed to initialize bot: %v", err)
	}

	radioModule, err := buildRadioModule(b, cfg)
	if err != nil {
		b.Stop()
		bot.Fatalf("Failed to initialize radio: %v", err)
	}
	b.AttachRadio(radioModule)

	radio.SetDefaultBotPrefix(cfg.Prefix)
	radio.SetPrefixResolver(func(guildID string) string {
		if b != nil && b.DB != nil && guildID != "" {
			if gCfg, err := b.DB.GetGuildConfig(guildID); err == nil && gCfg != nil && gCfg.Prefix != "" {
				return gCfg.Prefix
			}
		}
		return cfg.Prefix
	})
	initRuntime(cfg, b)
	coordinator.CleanupStaleRadioBuffers()
	registry := initCommandRegistry()
	eventHandler := handler.New(b, registry)
	registerListeners(b)
	b.Session.AddHandler(eventHandler.OnMessageCreate)
	b.Session.AddHandler(eventHandler.OnInteractionCreate)
	initApplication(b, cfg)

	if err := b.Start(); err != nil {
		bot.Fatalf("Failed to start bot session: %v", err)
	}

	startBackgroundWorkers(b)
	waitForShutdown(b, cfg)
}

func warnUnavailableBinaries() {
	for _, check := range helpers.CheckRequiredBinaries() {
		if check.ConfiguredPath != "" && check.ConfiguredError != nil {
			bot.Warnf("[STARTUP] %s=%q is unusable: %v.", check.EnvVar, check.ConfiguredPath, check.ConfiguredError)
		}
		if check.Err == nil {
			bot.Infof("[STARTUP] %s ready at %q (%s).", check.Name, check.Path, check.Source)
			continue
		}
		bot.Warnf("[STARTUP] %s is unavailable at %q (%s): %v. Media features that need it will be unavailable; install it, add it to PATH, or set %s to its executable path.", check.Name, check.Path, check.Source, check.Err, check.EnvVar)
	}
}

func buildRadioModule(b *bot.Bot, cfg *config.Config) (*coordinator.Module, error) {
	songCache := coordinator.NewLRUSongCache(
		cfg.RadioCacheDir,
		coordinator.DefaultMaxCacheBytes,
		coordinator.DefaultMaxCachedSongs,
	)
	if cfg.MaxMediaDownloadSizeMB > 0 {
		songCache.SetMaxDownloadSizeMB(cfg.MaxMediaDownloadSizeMB)
	}
	juiceProvider := radio.NewJuiceTrackProvider(songCache)
	controllerPort := radio.NewDiscordControllerPort(b.Session)

	var radioModule *coordinator.Module
	voicePort := coordinator.NewDAVEPort(b.Session, func(connection coordinator.VoiceConnection, guildID, channelID string) {
		if radioModule != nil {
			radioModule.HandleVoiceConnectionLoss(b.Ctx, guildID, channelID, connection)
		}
	})
	playbackEngine := coordinator.NewEngine(
		songCache,
		func(guildID string) coordinator.VoiceConnection {
			if radioModule == nil {
				return nil
			}
			return radioModule.VoiceConnection(guildID)
		},
		coordinator.DefaultPlaybackConfig(),
		func(err error) {
			logger.Warnf("[RADIO PLAYBACK] %v", err)
		},
	)

	recentTracker := coordinator.NewRecentPlaysTracker(coordinator.DefaultRecentPlayCap, coordinator.DefaultRecentMaxAge)
	inactivityManager := coordinator.NewInactivityManager(coordinator.DefaultInactivityConfig())

	radioModule, err := coordinator.New(coordinator.Options{
		Context:           b.Ctx,
		Settings:          coordinator.NewDatabaseSettingsStore(b.DB),
		Voice:             voicePort,
		Playback:          playbackEngine,
		AutoPlay:          juiceProvider,
		TrackResolver:     radio.FetchJuiceWRLDSong,
		RecentPath:        filepath.Join(cfg.DataDir, "radio", "recent_plays.json"),
		RecentPlays:       recentTracker,
		Inactivity:        inactivityManager,
		IdleSweepInterval: 5 * time.Minute,
		IdleSessionAge:    15 * time.Minute,
		Controller:        controllerPort,
		Prebuffer:         songCache,
		Cache:             songCache,
		Warn: func(err error) {
			logger.Warnf("[RADIO] %v", err)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing radio module: %w", err)
	}
	return radioModule, nil
}

func initRuntime(cfg *config.Config, b *bot.Bot) {
	juicewrld.ConfigureAPI(cfg.JuiceWRLDAPIURL, cfg.JuiceWRLDAPITimeout)
	graphics.ConfigureGraphics(cfg.CustomFontPath, cfg.DefaultRankCardTheme)
	coordinator.ConfigureRadioStorage(cfg.RadioCacheDir, cfg.TempDownloadDir, cfg.YTDLPCookiesPath)
	if b.Policy != nil && len(cfg.GlobalDisabledCommands) > 0 {
		for _, cmdToDisable := range cfg.GlobalDisabledCommands {
			if err := b.Policy.DisableCommand(policy.Scope{Type: policy.ScopeGlobal}, cmdToDisable, "SYSTEM_CONFIG"); err != nil {
				bot.Warnf("[POLICY] Failed to disable global command '%s': %v", cmdToDisable, err)
			}
		}
	}

	if b.DB != nil && cfg.AutoBackupEnabled {
		if backupFile, created, err := b.DB.AutoBackupIfDue(cfg.DatabasePath, "", cfg.AutoBackupInterval, cfg.BackupRetentionCount); err == nil {
			if created {
				bot.Infof("[DATABASE] Created automated backup: %s (Max retained: %d)", backupFile, cfg.BackupRetentionCount)
			} else {
				bot.Debugf("[DATABASE] Automated backup skipped - most recent backup is less than %s old", cfg.AutoBackupInterval)
			}
		} else {
			bot.Warnf("[DATABASE] Failed to perform automated backup check: %v", err)
		}
	}
}

func initCommandRegistry() *commands.Registry {
	registry := commands.NewRegistry()
	if err := registry.RegisterAll(
		utility.Commands,
		moderation.Commands,
		filter.Commands,
		cmdjw.Commands,
		lastfm.Commands,
		ripper.Commands,
		radio.Commands,
		voice.Commands,
		leveling.Commands,
		giveaway.Commands,
	); err != nil {
		bot.Fatalf("Command registry initialization failed: %v", err)
	}
	utility.HelpCmdInstance.Registry = registry
	return registry
}

func registerListeners(b *bot.Bot) {
	b.Session.AddHandler(listeners.OnAFKMessageCreate(b.DB, b.Config.Prefix))
	b.Session.AddHandler(listeners.OnLevelingMessageCreate(b.DB))
	b.Session.AddHandler(listeners.OnGuildChannelCreate(b.DB))
	b.Session.AddHandler(radio.OnChannelDeleteHandler(b.Radio))
	b.Session.AddHandler(radio.OnMessageCreateForPlayerChannel(b))
	b.Session.AddHandler(listeners.OnMessageReactionRemove)
	b.Session.AddHandler(listeners.OnStarboardReactionAdd(b.DB))
	b.Session.AddHandler(listeners.OnStarboardReactionRemove(b.DB))
	b.Session.AddHandler(listeners.OnAutoModerationActionExecution(b.DB, b.Policy))
	b.Session.AddHandler(radio.OnVoiceStateUpdateHandler(b.Radio))
	b.Session.AddHandler(voice.OnVoiceMasterUpdate(b.DB))

	voice.GlobalDB = b.DB
	b.Session.AddHandler(listeners.OnGuildMemberUpdateNickname(b.DB))
	b.Session.AddHandler(listeners.OnMessageCreateForAntiMP3(b.DB))

	b.Session.AddHandler(func(s *discordgo.Session, g *discordgo.GuildCreate) {
		if g == nil || g.Guild == nil || g.ID == "" || b.Policy == nil {
			return
		}
		res := b.Policy.IsGuildBlocked(g.ID)
		if res.Unavailable() {
			bot.Errorf("[SECURITY] Global blacklist lookup failed for guild %s (%s); skipping auto-leave", g.ID, g.Name)
			return
		}
		if res.Restricted() {
			bot.Infof("[SECURITY] Auto-leaving blacklisted guild: %s (%s)", g.Name, g.ID)
			_ = s.GuildLeave(g.ID)
		}
	})
	b.Session.AddHandler(func(s *discordgo.Session, g *discordgo.GuildDelete) {
		if g == nil || g.Guild == nil || g.ID == "" || g.Unavailable {
			return
		}
		if b.DB != nil {
			if err := b.DB.DeleteGuildConfig(g.ID); err != nil {
				bot.Warnf("[GUILD] Failed to purge guild config on leave %s: %v", g.ID, err)
			}
		}
	})

	b.Session.AddHandler(listeners.OnGuildMemberUpdateForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildMemberAddForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildMemberRemoveForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildBanAddForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildBanRemoveForLog(b.DB))
	b.Session.AddHandler(listeners.OnVoiceStateUpdateForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildChannelCreateForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildChannelDeleteForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildChannelUpdateForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildRoleCreateForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildRoleUpdateForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildRoleDeleteForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildInviteCreateForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildInviteDeleteForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildEmojisUpdateForLog(b.DB))
	b.Session.AddHandler(listeners.OnGuildUpdateForLog(b.DB))
}

func initApplication(b *bot.Bot, cfg *config.Config) {
	app, err := b.Session.Application("@me")
	if err != nil || app == nil {
		bot.Warnf("[SESSION] Failed to fetch application info: %v (radio emojis will degrade to Unicode fallbacks)", err)
		return
	}

	if app.Owner != nil && app.Owner.ID != "" {
		cfg.OwnerIDs = append(cfg.OwnerIDs, app.Owner.ID)
	}
	if app.Team != nil {
		for _, member := range app.Team.Members {
			if member.User != nil && member.User.ID != "" {
				cfg.OwnerIDs = append(cfg.OwnerIDs, member.User.ID)
			}
		}
	}

	if errEmoji := radio.EnsureRadioApplicationEmojis(b.Session, app.ID); errEmoji != nil {
		bot.Warnf("[RADIO EMOJI] Some application emojis could not be provisioned: %v (degraded to Unicode fallbacks)", errEmoji)
	}

	registerSlashCommands(b, cfg, app.ID)
}

func registerSlashCommands(b *bot.Bot, cfg *config.Config, appID string) {
	if cfg.PrimaryGuildID != "" {
		bot.Infof("[SLASH] PRIMARY_GUILD_ID is set; registering slash commands to guild %s", cfg.PrimaryGuildID)
	}
	if errSlash := giveaway.RegisterGiveawaySlashCommands(b.Session, appID, cfg.PrimaryGuildID); errSlash != nil {
		bot.Warnf("[GIVEAWAY] Failed to register slash commands: %v", errSlash)
	}
}

func startBackgroundWorkers(b *bot.Bot) {
	if err := ripper.StartQueueManager(b.Ctx, b.Config.TempDownloadDir, b.Config.MaxMediaDownloadSizeMB, b.Config.YTDLPCookiesPath); err != nil {
		bot.Errorf("[RIPPER] Failed to start media ripper: %v", err)
	}
	giveaway.InitGiveawayScheduler(b.Session, b.DB)
	b.StartPeriodicWorker("temp_roles", 30*time.Second, func(ctx context.Context) {
		listeners.ProcessExpiredTempRoles(ctx, b.Session, b.DB)
	})
	b.StartPeriodicWorker("radio_buffer_cleanup", 1*time.Hour, func(ctx context.Context) {
		coordinator.CleanupStaleRadioBuffers()
	})
	if b.DB != nil && b.Config.AutoBackupEnabled {
		checkInterval := b.Config.AutoBackupInterval / 2
		if checkInterval < 15*time.Minute {
			checkInterval = 15 * time.Minute
		}
		b.StartPeriodicWorker("auto_backup", checkInterval, func(ctx context.Context) {
			if backupFile, created, err := b.DB.AutoBackupIfDue(b.Config.DatabasePath, "", b.Config.AutoBackupInterval, b.Config.BackupRetentionCount); err == nil {
				if created {
					bot.Infof("[DATABASE] Created automated backup: %s (Max retained: %d)", backupFile, b.Config.BackupRetentionCount)
				}
			} else {
				bot.Warnf("[DATABASE] Periodic automated backup check failed: %v", err)
			}
		})
	}
	b.WG().Add(1)
	helpers.Spawn(func() {
		defer b.WG().Done()
		voice.InitVoiceMaster(b.Ctx, b.Session, b.DB)
	})
}

func waitForShutdown(b *bot.Bot, cfg *config.Config) {
	bot.SendConsoleWebhook(cfg.ConsoleWebhookURL, "Bot Started", fmt.Sprintf("GoBot is online and running (Prefix: `%s`).", cfg.Prefix), helpers.ColorSuccess)
	bot.Infof("GoBot is online and running (Prefix: '%s'). Press CTRL-C to shut down.", cfg.Prefix)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdown(b, cfg)
}

func shutdown(b *bot.Bot, cfg *config.Config) {
	bot.Infof("Shutting down bot session...")
	bot.SendConsoleWebhook(cfg.ConsoleWebhookURL, "Bot Shutting Down", "GoBot session has stopped.", helpers.ColorError)
	ripper.StopQueueManager()
	giveaway.StopGiveawayScheduler()
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer flushCancel()
	if err := filter.DefaultSyncer.Flush(flushCtx); err != nil {
		bot.Warnf("[AUTOMOD] Pending rule sync flush timed out or failed: %v", err)
	}
	b.Stop()
	logger.Close()
}
