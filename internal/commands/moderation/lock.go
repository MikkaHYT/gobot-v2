package moderation

import (
	"fmt"
	"strings"
	"sync"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/modlog"

	"github.com/bwmarrin/discordgo"
)

type LockCmd struct{}

func (c *LockCmd) Name() string      { return "lock" }
func (c *LockCmd) Aliases() []string { return []string{"l", "lockchannel"} }
func (c *LockCmd) Category() string  { return "Moderation" }
func (c *LockCmd) Description() string {
	return "Locks a channel."
}
func (c *LockCmd) Usage() string      { return "(#channel) [reason]" }
func (c *LockCmd) Example() string    { return "#general Maintenance" }
func (c *LockCmd) Permissions() int64 { return discordgo.PermissionManageChannels }

func (c *LockCmd) Execute(ctx *bot.Context) error {
	return handleLockToggle(ctx, true)
}

type UnlockCmd struct{}

func (c *UnlockCmd) Name() string      { return "unlock" }
func (c *UnlockCmd) Aliases() []string { return []string{"ul", "unlockchannel"} }
func (c *UnlockCmd) Category() string  { return "Moderation" }
func (c *UnlockCmd) Description() string {
	return "Unlocks a channel."
}
func (c *UnlockCmd) Usage() string      { return "(#channel) [reason]" }
func (c *UnlockCmd) Example() string    { return "#general Maintenance completed" }
func (c *UnlockCmd) Permissions() int64 { return discordgo.PermissionManageChannels }

func (c *UnlockCmd) Execute(ctx *bot.Context) error {
	return handleLockToggle(ctx, false)
}

func handleLockToggle(ctx *bot.Context, isLock bool) error {

	var channel *discordgo.Channel
	reason := ""
	if len(ctx.Args) > 0 {
		if ch, err := ctx.ResolveChannel(ctx.Args[0]); err == nil && ch != nil {
			channel = ch
			if len(ctx.Args) > 1 {
				reason = strings.Join(ctx.Args[1:], " ")
			}
		} else {
			reason = strings.Join(ctx.Args, " ")
		}
	}
	if channel == nil {
		ch, err := ctx.ResolveChannel(ctx.Message.ChannelID)
		if err != nil || ch == nil {
			return ctx.SendError("Could not find current channel.")
		}
		channel = ch
	}

	targetChannelID := channel.ID

	isCurrentlyLocked := helpers.IsChannelLocked(channel, ctx.Message.GuildID)

	if isLock {
		if isCurrentlyLocked {
			embed := &discordgo.MessageEmbed{
				Description: fmt.Sprintf("**<#%s>** is already locked.", targetChannelID),
			}
			_, err := ctx.ReplyEmbed(embed)
			return err
		}
		if err := helpers.LockChannel(ctx.Session, ctx.Message.GuildID, targetChannelID); err != nil {
			return fmt.Errorf("failed to lock channel: %w", err)
		}
	} else {
		if !isCurrentlyLocked {
			embed := &discordgo.MessageEmbed{
				Description: fmt.Sprintf("**<#%s>** is already unlocked.", targetChannelID),
			}
			_, err := ctx.ReplyEmbed(embed)
			return err
		}
		if err := helpers.UnlockChannel(ctx.Session, ctx.Message.GuildID, targetChannelID); err != nil {
			return fmt.Errorf("failed to unlock channel: %w", err)
		}
	}

	actionStr := "locked"
	if !isLock {
		actionStr = "unlocked"
	}

	_ = ctx.ReactSuccess()
	eventTitle := "Channel Locked"
	if !isLock {
		eventTitle = "Channel Unlocked"
	}
	modlog.Log(ctx.Session, ctx.DB, &modlog.ChannelEvent{
		GuildID:     ctx.Message.GuildID,
		Action:      eventTitle,
		ChannelName: channel.Name,
		ChannelID:   targetChannelID,
		Moderator:   ctx.Message.Author,
		Reason:      reason,
	})

	desc := fmt.Sprintf("**<#%s>** has been %s.", targetChannelID, actionStr)
	if reason != "" {
		desc += fmt.Sprintf("\n**Reason:** %s", reason)
	}

	embed := &discordgo.MessageEmbed{
		Description: desc,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

type LockdownCmd struct{}

func (c *LockdownCmd) Name() string      { return "lockdown" }
func (c *LockdownCmd) Aliases() []string { return []string{"ld", "serverlock"} }
func (c *LockdownCmd) Category() string  { return "Moderation" }
func (c *LockdownCmd) Description() string {
	return "Locks all public server text channels."
}
func (c *LockdownCmd) Usage() string      { return "[reason]" }
func (c *LockdownCmd) Example() string    { return "Raid in progress" }
func (c *LockdownCmd) Permissions() int64 { return discordgo.PermissionManageChannels }

func (c *LockdownCmd) Execute(ctx *bot.Context) error {
	confirmed, err := ctx.PromptConfirmation("Are you sure you want to initiate a **server lockdown**? All public text channels will be locked.")
	if err != nil || !confirmed {
		return err
	}

	reason := "Server Lockdown"
	if len(ctx.Args) > 0 {
		reason = strings.Join(ctx.Args, " ")
	}

	channels, err := ctx.Session.GuildChannels(ctx.Message.GuildID)
	if err != nil {
		return fmt.Errorf("failed to fetch channels: %w", err)
	}

	var lockedChannels []string
	var mu sync.Mutex
	const maxLockdownWorkers = 8
	sem := make(chan struct{}, maxLockdownWorkers)
	var wg sync.WaitGroup
	for _, ch := range channels {
		if ch.Type == discordgo.ChannelTypeGuildText || ch.Type == discordgo.ChannelTypeGuildNews {
			if !helpers.IsChannelLocked(ch, ctx.Message.GuildID) {
				chID := ch.ID
				sem <- struct{}{}
				wg.Add(1)
				helpers.Spawn(func() {
					defer wg.Done()
					defer func() { <-sem }()
					if err := helpers.LockChannel(ctx.Session, ctx.Message.GuildID, chID); err == nil {
						mu.Lock()
						lockedChannels = append(lockedChannels, chID)
						mu.Unlock()
					}
				})
			}
		}
	}
	wg.Wait()

	if len(lockedChannels) > 0 {
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingLockedChannels, lockedChannels); err != nil {
			bot.Warnf("[LOCKDOWN] Failed to persist locked channels for guild %s: %v", ctx.Message.GuildID, err)
		}
	}

	_ = ctx.ReactSuccess()
	modlog.Log(ctx.Session, ctx.DB, &modlog.ChannelEvent{
		GuildID:     ctx.Message.GuildID,
		Action:      "Server Lockdown Initiated",
		ChannelName: "All Public Channels",
		Moderator:   ctx.Message.Author,
		Reason:      reason,
	})

	var mentions []string
	for _, id := range lockedChannels {
		mentions = append(mentions, fmt.Sprintf("<#%s>", id))
	}

	chanListStr := "None"
	if len(mentions) > 0 {
		if len(mentions) > 20 {
			chanListStr = fmt.Sprintf("%s ... (+%d more)", strings.Join(mentions[:20], ", "), len(mentions)-20)
		} else {
			chanListStr = strings.Join(mentions, ", ")
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Server Lockdown Initiated",
		Description: fmt.Sprintf("Locked **%d** channel(s).\n**Reason:** %s\n\n**Locked Channels:** %s", len(lockedChannels), reason, chanListStr),
		Color:       helpers.ColorError,
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type UnlockdownCmd struct{}

func (c *UnlockdownCmd) Name() string      { return "unlockdown" }
func (c *UnlockdownCmd) Aliases() []string { return []string{"unld", "serverunlock"} }
func (c *UnlockdownCmd) Category() string  { return "Moderation" }
func (c *UnlockdownCmd) Description() string {
	return "Lifts server lockdown and restores general members sending permissions on locked channels."
}
func (c *UnlockdownCmd) Usage() string      { return "[reason]" }
func (c *UnlockdownCmd) Example() string    { return "Lockdown resolved" }
func (c *UnlockdownCmd) Permissions() int64 { return discordgo.PermissionManageChannels }

func (c *UnlockdownCmd) Execute(ctx *bot.Context) error {

	confirmed, err := ctx.PromptConfirmation("Are you sure you want to **lift server lockdown**? Sending permissions will be restored on locked channels.")
	if err != nil || !confirmed {
		return err
	}

	reason := "Lockdown lifted"
	if len(ctx.Args) > 0 {
		reason = strings.Join(ctx.Args, " ")
	}

	var unlockedChannels []string
	var remainingLocked []string

	savedLocked, _ := ctx.DB.GetGuildSettingSlice(ctx.Message.GuildID, database.SettingLockedChannels)

	if len(savedLocked) > 0 {
		var mu sync.Mutex
		const maxUnlockWorkers = 8
		sem := make(chan struct{}, maxUnlockWorkers)
		var wg sync.WaitGroup
		for _, chID := range savedLocked {
			targetChID := chID
			sem <- struct{}{}
			wg.Add(1)
			helpers.Spawn(func() {
				defer wg.Done()
				defer func() { <-sem }()
				if err := helpers.UnlockChannel(ctx.Session, ctx.Message.GuildID, targetChID); err == nil {
					mu.Lock()
					unlockedChannels = append(unlockedChannels, targetChID)
					mu.Unlock()
				} else {
					mu.Lock()
					remainingLocked = append(remainingLocked, targetChID)
					mu.Unlock()
				}
			})
		}
		wg.Wait()
	} else {
		channels, err := ctx.Session.GuildChannels(ctx.Message.GuildID)
		if err != nil {
			return fmt.Errorf("failed to fetch channels: %w", err)
		}
		var mu sync.Mutex
		const maxUnlockWorkers2 = 8
		sem := make(chan struct{}, maxUnlockWorkers2)
		var wg sync.WaitGroup
		for _, ch := range channels {
			if ch.Type == discordgo.ChannelTypeGuildText || ch.Type == discordgo.ChannelTypeGuildNews {
				if helpers.IsChannelLocked(ch, ctx.Message.GuildID) {
					targetChID := ch.ID
					sem <- struct{}{}
					wg.Add(1)
					helpers.Spawn(func() {
						defer wg.Done()
						defer func() { <-sem }()
						if err := helpers.UnlockChannel(ctx.Session, ctx.Message.GuildID, targetChID); err == nil {
							mu.Lock()
							unlockedChannels = append(unlockedChannels, targetChID)
							mu.Unlock()
						}
					})
				}
			}
		}
		wg.Wait()
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingLockedChannels, remainingLocked); err != nil {
		bot.Warnf("[UNLOCKDOWN] Failed to update locked channels for guild %s: %v", ctx.Message.GuildID, err)
	}

	_ = ctx.ReactSuccess()
	modlog.Log(ctx.Session, ctx.DB, &modlog.ChannelEvent{
		GuildID:     ctx.Message.GuildID,
		Action:      "Server Lockdown Lifted",
		ChannelName: "All Locked Channels",
		Moderator:   ctx.Message.Author,
		Reason:      reason,
	})

	var mentions []string
	for _, id := range unlockedChannels {
		mentions = append(mentions, fmt.Sprintf("<#%s>", id))
	}

	chanListStr := "None"
	if len(mentions) > 0 {
		if len(mentions) > 20 {
			chanListStr = fmt.Sprintf("%s ... (+%d more)", strings.Join(mentions[:20], ", "), len(mentions)-20)
		} else {
			chanListStr = strings.Join(mentions, ", ")
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Server Lockdown Lifted",
		Description: fmt.Sprintf("Unlocked **%d** channel(s).\n**Reason:** %s\n\n**Unlocked Channels:** %s", len(unlockedChannels), reason, chanListStr),
		Color:       helpers.ColorSuccess,
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}
