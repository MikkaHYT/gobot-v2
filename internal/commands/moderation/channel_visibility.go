package moderation

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type HideChannelCmd struct{}

func (c *HideChannelCmd) Name() string        { return "hidechannel" }
func (c *HideChannelCmd) Aliases() []string   { return []string{"hidechan"} }
func (c *HideChannelCmd) Category() string    { return "Server Configuration" }
func (c *HideChannelCmd) Description() string { return "Hides a channel from everyone." }
func (c *HideChannelCmd) Usage() string       { return "[#channel | channelID]" }
func (c *HideChannelCmd) Example() string     { return "#general" }
func (c *HideChannelCmd) Permissions() int64  { return discordgo.PermissionManageChannels }

func (c *HideChannelCmd) Execute(ctx *bot.Context) error {
	channel := ctx.Message.ChannelID
	if len(ctx.Args) > 0 {
		arg := strings.Join(ctx.Args, " ")
		ch, err := ctx.ResolveChannel(arg)
		if err != nil || ch == nil || ch.GuildID != ctx.Message.GuildID {
			return ctx.SendError(fmt.Sprintf("Could not resolve channel `%s` in this server.", arg))
		}
		channel = ch.ID
	}

	if err := helpers.HideChannel(ctx.Session, ctx.Message.GuildID, channel); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to hide channel: %v", err))
	}

	return ctx.SendSuccess("Hidden <#%s> from everyone.", channel)
}

type RevealChannelCmd struct{}

func (c *RevealChannelCmd) Name() string      { return "revealchannel" }
func (c *RevealChannelCmd) Aliases() []string { return []string{"revealchan", "unhidechannel"} }
func (c *RevealChannelCmd) Category() string  { return "Server Configuration" }
func (c *RevealChannelCmd) Description() string {
	return "Restores everyone view access to a channel."
}
func (c *RevealChannelCmd) Usage() string      { return "<#channel | channelID>" }
func (c *RevealChannelCmd) Example() string    { return "#general" }
func (c *RevealChannelCmd) Permissions() int64 { return discordgo.PermissionManageChannels }

func (c *RevealChannelCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	arg := strings.Join(ctx.Args, " ")
	ch, err := ctx.ResolveChannel(arg)
	if err != nil || ch == nil || ch.GuildID != ctx.Message.GuildID {
		return ctx.SendError(fmt.Sprintf("Could not resolve channel `%s` in this server.", arg))
	}

	if err := helpers.UnhideChannel(ctx.Session, ctx.Message.GuildID, ch.ID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to reveal channel: %v", err))
	}

	hidden := helpers.IsChannelHidden(ch, ctx.Message.GuildID)
	note := ""
	if hidden {
		note = "\n-# Role overwrites may still hide this channel for some members."
	}

	return ctx.SendSuccess("Revealed <#%s> to everyone.%s", ch.ID, note)
}
