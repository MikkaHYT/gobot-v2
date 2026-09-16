package handler

import (
	"errors"
	"fmt"
	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/commands/moderation"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/policy"
	"runtime/debug"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func (h *Handler) OnMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m == nil || m.Author == nil || s == nil || s.State == nil || s.State.User == nil {
		return
	}
	if m.Author.Bot || m.Author.ID == s.State.User.ID {
		return
	}

	var guildCfg *database.GuildConfig
	if h.bot.DB != nil && m.GuildID != "" {
		if gCfg, err := h.bot.DB.GetGuildConfig(m.GuildID); err == nil && gCfg != nil {
			guildCfg = gCfg
		} else if err != nil && !errors.Is(err, database.ErrNotFound) {
			bot.Warnf("[POLICY] Failed to fetch guild config for %s: %v (req denied)", m.GuildID, err)
			return
		}
	}

	prefix := h.bot.Config.Prefix
	if guildCfg != nil && guildCfg.Prefix != "" {
		prefix = guildCfg.Prefix
	}

	raw, matched := parseCommandContent(m.Content, prefix, s.State.User.ID)
	if !matched {
		return
	}
	if h.bot.Policy == nil {
		return
	}

	if m.GuildID != "" {
		if res := h.bot.Policy.IsGuildBlocked(m.GuildID); res.Restricted() {
			_ = s.GuildLeave(m.GuildID)
			return
		}
	}

	if moderation.ConsumeUnjailForce(s, m, h.bot.DB) {
		return
	}

	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return
	}

	cmdName := strings.ToLower(parts[0])
	args := parts[1:]

	bot.Debugf("[CMD] Received command: '%s' from %s", cmdName, m.Author.String())

	if cmd, found := h.registry.Get(cmdName); found {
		canonicalName := strings.ToLower(cmd.Name())

		var memberRoles []string
		if m.Member != nil {
			memberRoles = m.Member.Roles
		}

		res := h.bot.Policy.CanExecute(policy.ExecutionRequest{
			GuildID:       m.GuildID,
			ChannelID:     m.ChannelID,
			UserID:        m.Author.ID,
			MemberRoleIDs: memberRoles,
			CanonicalName: canonicalName,
			InvokedName:   cmdName,
		})
		if !res.Allowed() {
			return
		}

		if m.Member != nil && m.Member.User == nil && m.Author != nil {
			m.Member.User = m.Author
		}

		ctx := &bot.Context{
			Session:        s,
			Message:        m,
			Args:           args,
			Prefix:         prefix,
			InvokedCommand: cmdName,
			DB:             h.bot.DB,
			Policy:         h.bot.Policy,
			Radio:          h.bot.Radio,
			OwnerIDs:       h.bot.Config.OwnerIDs,
			Config:         h.bot.Config,
			GuildConfig:    guildCfg,
			Ctx:            h.bot.Ctx,
		}

		rateLimiter := h.bot.RateLimiter
		if rateLimiter == nil {
			rateLimiter = bot.GlobalRateLimiter
		}
		cooldowns := h.bot.Cooldowns
		if cooldowns == nil {
			cooldowns = bot.GlobalCooldowns
		}

		if rateLimiter != nil {
			if limited, waitDur, shouldNotify := rateLimiter.CheckLimit(m.GuildID, m.Author.ID); limited {
				bot.Debugf("[RATE LIMIT] User %s (%s) rate-limited for %v in guild %s (notified: %v)", m.Author.Username, m.Author.ID, waitDur, m.GuildID, shouldNotify)
				if shouldNotify {
					_ = ctx.SendError(fmt.Sprintf("You are sending commands too quickly. Please wait **%s**.", helpers.FormatDuration(waitDur)))
				}
				return
			}
		}

		if cooldowns != nil {
			if cooldownDuration := bot.GetAPICooldown(canonicalName); cooldownDuration > 0 {
				if onCooldown, remaining := cooldowns.CheckCoolDown(m.Author.ID, canonicalName, cooldownDuration); onCooldown {
					bot.Debugf("[COOLDOWN] User %s (%s) on cooldown for command '%s' (remaining: %v)", m.Author.Username, m.Author.ID, canonicalName, remaining)
					_ = ctx.SendError(fmt.Sprintf("Please wait **%.1f seconds** before using this command again.", remaining.Seconds()))
					return
				}
			}
		}

		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				bot.Errorf("[CMD PANIC] Command '%s' panicked: %v\n%s", cmdName, r, stack)
				bot.SendConsoleWebhook(h.bot.Config.ConsoleWebhookURL, "Command Panic", fmt.Sprintf("Command `%s%s` panicked: `%v`", prefix, cmdName, r), helpers.ColorError)
				if cooldownDuration := bot.GetAPICooldown(canonicalName); cooldownDuration > 0 {
					cooldowns.Reset(m.Author.ID, canonicalName)
				}
				_ = ctx.SendError("An internal error occurred while executing this command.")
			}
		}()

		if permChecker, ok := cmd.(commands.PermissionChecker); ok {
			if reqPerm := permChecker.Permissions(); reqPerm != 0 {
				if !ctx.RequirePermissions(reqPerm) {
					return
				}
			}
		}
		bot.Infof("[CMD] Executing '%s' for %s (Guild: %s)", cmdName, m.Author.String(), m.GuildID)
		if err := cmd.Execute(ctx); err != nil {
			if cooldownDuration := bot.GetAPICooldown(canonicalName); cooldownDuration > 0 {
				cooldowns.Reset(m.Author.ID, canonicalName)
			}
			bot.Errorf("[CMD] Command '%s' returned error: %v", cmdName, err)
			bot.SendConsoleWebhook(h.bot.Config.ConsoleWebhookURL, "Command Error", fmt.Sprintf("Command `%s%s` returned error: `%v`", prefix, cmdName, err), helpers.ColorError)
			_ = ctx.SendError(err.Error())
		}
	} else {
		bot.Debugf("[CMD] Command '%s' not registered", cmdName)
	}
}
