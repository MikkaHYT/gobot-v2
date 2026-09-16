package moderation

import (
	"fmt"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/modlog"

	"github.com/bwmarrin/discordgo"
)

type NukeCmd struct{}

func (c *NukeCmd) Name() string      { return "nuke" }
func (c *NukeCmd) Aliases() []string { return []string{"nukechannel", "channelnuke"} }
func (c *NukeCmd) Category() string  { return "Moderation" }
func (c *NukeCmd) Description() string {
	return "Clones current channel and deletes old channel after confirmation."
}
func (c *NukeCmd) Usage() string      { return "(#channel)" }
func (c *NukeCmd) Example() string    { return "#general" }
func (c *NukeCmd) Permissions() int64 { return discordgo.PermissionManageChannels }

func (c *NukeCmd) Execute(ctx *bot.Context) error {
	return executeNuke(ctx, false)
}

type SoftnukeCmd struct{}

func (c *SoftnukeCmd) Name() string      { return "softnuke" }
func (c *SoftnukeCmd) Aliases() []string { return []string{"snuke"} }
func (c *SoftnukeCmd) Category() string  { return "Moderation" }
func (c *SoftnukeCmd) Description() string {
	return "Makes old channel private and clones a fresh channel after confirmation."
}
func (c *SoftnukeCmd) Usage() string      { return "(#channel)" }
func (c *SoftnukeCmd) Example() string    { return "#general" }
func (c *SoftnukeCmd) Permissions() int64 { return discordgo.PermissionManageChannels }

func (c *SoftnukeCmd) Execute(ctx *bot.Context) error {
	return executeNuke(ctx, true)
}

func executeNuke(ctx *bot.Context, isSoft bool) error {

	channelArg := ""
	if len(ctx.Args) > 0 {
		channelArg = ctx.Args[0]
	}

	targetChannelID := ctx.ParseChannelID(channelArg)
	if targetChannelID == "" {
		return ctx.SendError("Could not find that channel.")
	}

	oldChannel, err := ctx.Session.Channel(targetChannelID)
	if err != nil {
		return ctx.SendError("Could not find that channel.")
	}

	if oldChannel.GuildID == "" || oldChannel.GuildID != ctx.Message.GuildID {
		return ctx.SendError("The specified channel does not belong to this server.")
	}

	promptText := "Delete all messages in this channel?"
	confirmed, err := ctx.PromptConfirmation(promptText)
	if err != nil || !confirmed {
		return err
	}

	newChannel, err := ctx.Session.GuildChannelCreateComplex(oldChannel.GuildID, discordgo.GuildChannelCreateData{
		Name:                 oldChannel.Name,
		Type:                 oldChannel.Type,
		Topic:                oldChannel.Topic,
		Bitrate:              oldChannel.Bitrate,
		UserLimit:            oldChannel.UserLimit,
		RateLimitPerUser:     oldChannel.RateLimitPerUser,
		Position:             oldChannel.Position,
		PermissionOverwrites: oldChannel.PermissionOverwrites,
		ParentID:             oldChannel.ParentID,
		NSFW:                 oldChannel.NSFW,
	})
	if err != nil {
		return fmt.Errorf("failed to clone channel: %w", err)
	}

	_ = ctx.ReactSuccess()
	nukeType := "Channel Nuked"
	if isSoft {
		nukeType = "Channel Soft-Nuked"
	}
	modlog.Log(ctx.Session, ctx.DB, &modlog.ChannelEvent{
		GuildID:     ctx.Message.GuildID,
		Action:      nukeType,
		ChannelName: oldChannel.Name,
		ChannelID:   newChannel.ID,
		Moderator:   ctx.Message.Author,
		Reason:      "Channel recreated",
	})

	cfg, _ := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if cfg != nil {
		if cfg.ModLogChannelID == oldChannel.ID {
			_ = ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingModLogChannelID, newChannel.ID)
		}
		if cfg.StarboardChannelID == oldChannel.ID {
			_ = ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingStarboardChannelID, newChannel.ID)
		}
		if cfg.JailChannelID == oldChannel.ID {
			_ = ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingJailChannelID, newChannel.ID)
		}
		if cfg.VoiceMasterTriggerChannelID == oldChannel.ID {
			_ = ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingVoiceMasterTriggerChannelID, newChannel.ID)
		}
	}

	if isSoft {
		_ = helpers.HideChannel(ctx.Session, oldChannel.GuildID, oldChannel.ID)

		softEmbed := &discordgo.MessageEmbed{
			Title:       "First",
			Description: fmt.Sprintf("Channel has been soft-nuked by **<@%s>**.", ctx.Message.Author.ID),
			Color:       helpers.ColorDefault,
		}
		_, err = ctx.Session.ChannelMessageSendEmbed(newChannel.ID, softEmbed)
		return err
	}

	_, err = ctx.Session.ChannelDelete(oldChannel.ID)
	if err != nil {
		return fmt.Errorf("failed to delete old channel: %w", err)
	}

	nukeEmbed := &discordgo.MessageEmbed{
		Title:       "First",
		Description: fmt.Sprintf("Channel has been nuked by **<@%s>**.", ctx.Message.Author.ID),
		Color:       helpers.ColorDefault,
	}
	_, err = ctx.Session.ChannelMessageSendEmbed(newChannel.ID, nukeEmbed)
	return err
}
