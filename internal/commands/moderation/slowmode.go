package moderation

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/modlog"

	"github.com/bwmarrin/discordgo"
)

type SlowmodeCmd struct{}

func (c *SlowmodeCmd) Name() string        { return "slowmode" }
func (c *SlowmodeCmd) Aliases() []string   { return []string{"sm"} }
func (c *SlowmodeCmd) Category() string    { return "Moderation" }
func (c *SlowmodeCmd) Description() string { return "Sets or disables channel slowmode rate limit." }
func (c *SlowmodeCmd) Usage() string       { return "<duration|off> [#channel]" }
func (c *SlowmodeCmd) Example() string     { return "5s" }
func (c *SlowmodeCmd) Permissions() int64  { return discordgo.PermissionManageChannels }

func (c *SlowmodeCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	targetChannelID := ctx.Message.ChannelID
	durArg := ""

	if len(ctx.Args) == 1 {
		durArg = ctx.Args[0]
	} else {
		if ch, err := ctx.ResolveChannel(ctx.Args[0]); err == nil && ch != nil {
			targetChannelID = ch.ID
			durArg = ctx.Args[1]
		} else if ch, err := ctx.ResolveChannel(ctx.Args[1]); err == nil && ch != nil {
			durArg = ctx.Args[0]
			targetChannelID = ch.ID
		} else {
			durArg = ctx.Args[0]
		}
	}

	channel, err := helpers.GetChannel(ctx.Session, targetChannelID)
	if err != nil || channel == nil {
		return ctx.SendError("Channel not found.")
	}
	if channel.GuildID != ctx.Message.GuildID {
		return ctx.SendError("Channel does not belong to this server.")
	}

	seconds := 0
	lowerDur := strings.ToLower(durArg)
	if lowerDur != "off" && lowerDur != "0" && lowerDur != "0s" && lowerDur != "none" && lowerDur != "disable" && lowerDur != "disabled" {
		d, parseErr := helpers.ParseDuration(durArg)
		if parseErr != nil {
			return ctx.SendError(fmt.Sprintf("Invalid duration `%s`. Example: `%ssm 5s` or `%ssm off`", durArg, ctx.Prefix, ctx.Prefix))
		}
		seconds = int(d.Seconds())
		if seconds < 0 || seconds > 21600 {
			return ctx.SendError("Slowmode duration must be between 0 seconds and 6 hours.")
		}
	}

	_, err = ctx.Session.ChannelEdit(targetChannelID, &discordgo.ChannelEdit{
		RateLimitPerUser: &seconds,
	})
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update slowmode: %v", err))
	}

	_ = ctx.ReactSuccess()

	actionStr := fmt.Sprintf("Slowmode set to %s", helpers.FormatDuration(time.Duration(seconds)*time.Second))
	if seconds == 0 {
		actionStr = "Slowmode disabled"
	}
	modlog.Log(ctx.Session, ctx.DB, &modlog.ChannelEvent{
		GuildID:     ctx.Message.GuildID,
		Action:      actionStr,
		ChannelName: channel.Name,
		ChannelID:   targetChannelID,
		Moderator:   ctx.Message.Author,
		Reason:      fmt.Sprintf("Rate limit updated to %d seconds", seconds),
	})

	var desc string
	if seconds > 0 {
		desc = fmt.Sprintf("Set slowmode for <#%s> to **%s**.", targetChannelID, helpers.FormatDuration(time.Duration(seconds)*time.Second))
	} else {
		desc = fmt.Sprintf("Disabled slowmode for <#%s>.", targetChannelID)
	}

	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: desc,
	})
	return err
}
