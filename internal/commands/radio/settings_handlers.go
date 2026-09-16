package radio

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

func NormalizeRadioMode(input string) (string, bool) {
	return coordinator.NormalizeRadioMode(input)
}

func (c *RadioGroupCmd) handleMode(ctx *bot.Context, mode string) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	if mode == "" {
		curr := "all"
		if ctx.Radio != nil {
			if snap, ok := ctx.Radio.Snapshot(ctx.Message.GuildID); ok && snap.Mode != "" {
				curr = snap.Mode
			}
		}

		embed := &discordgo.MessageEmbed{
			Title:       "Radio Mode Filter",
			Description: fmt.Sprintf("Current radio mode: **%s**\n\n**Available Modes:**\n• **Eras**: `gbgr`, `wod`, `drfl`, `jw3`, `post`, `pre-gbgr`\n• **Categories**: `unreleased`, `released`, `sessions`\n• **Reset**: `all`", curr),
			Color:       helpers.ColorDefault,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	canonicalMode, ok := NormalizeRadioMode(mode)
	if !ok {
		return ctx.SendError("Invalid radio mode. Valid modes: `all`, `unreleased`, `released`, `sessions`, `gbgr`, `wod`, `drfl`, `jw3`, `post`, `pre-gbgr`.")
	}

	if ctx.Radio != nil {
		if _, err := ctx.Radio.UpdateSettings(ctx.Context(), coordinator.SettingsUpdate{
			GuildID: ctx.Message.GuildID,
			Mode:    canonicalMode,
			ModeSet: true,
		}); err != nil {
			return ctx.SendError("Could not save the radio mode.")
		}
	}

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Set radio mode filter to **%s**.", canonicalMode),
		Color:       helpers.ColorDefault,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func (c *RadioGroupCmd) handleRestriction(ctx *bot.Context, mode string) error {
	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionManageGuild) {
		return nil
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		curr := "vc"
		if ctx.Radio != nil {
			if snap, ok := ctx.Radio.Snapshot(ctx.Message.GuildID); ok && snap.RestrictionMode != "" {
				curr = snap.RestrictionMode
			}
		}

		embed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Current restriction mode: **%s** (vc, all, mods)", curr),
			Color:       helpers.ColorDefault,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	if mode != "vc" && mode != "all" && mode != "mods" {
		return ctx.SendError("Restriction mode must be `vc`, `all`, or `mods`.")
	}

	if ctx.Radio != nil {
		if _, err := ctx.Radio.UpdateSettings(ctx.Context(), coordinator.SettingsUpdate{
			GuildID:            ctx.Message.GuildID,
			RestrictionMode:    mode,
			RestrictionModeSet: true,
		}); err != nil {
			return ctx.SendError("Could not save the radio restriction mode.")
		}
	}

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Updated radio restriction mode to **%s**.", strings.ToUpper(mode)),
		Color:       helpers.ColorDefault,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func (c *RadioGroupCmd) handleSetChannel(ctx *bot.Context, target string) error {
	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionManageGuild) {
		return nil
	}

	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		curr := ""
		if ctx.Radio != nil {
			if snap, ok := ctx.Radio.Snapshot(ctx.Message.GuildID); ok {
				curr = snap.AutojoinChannelID
			}
		}

		desc := "No autojoin voice channel currently set."
		if curr != "" {
			desc = fmt.Sprintf("Current autojoin voice channel: <#%s>", curr)
		}
		embed := &discordgo.MessageEmbed{
			Description: desc,
			Color:       helpers.ColorDefault,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	if target == "clear" || target == "off" || target == "disable" || target == "none" {
		if ctx.Radio != nil {
			if _, err := ctx.Radio.UpdateSettings(ctx.Context(), coordinator.SettingsUpdate{
				GuildID:              ctx.Message.GuildID,
				AutojoinChannelIDSet: true,
			}); err != nil {
				return ctx.SendError("Could not save the autojoin channel.")
			}
		}

		embed := &discordgo.MessageEmbed{
			Description: "Disabled autojoin voice channel.",
			Color:       helpers.ColorError,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	channelID := ctx.ParseChannelID(target)
	resolved, err := ctx.Session.Channel(channelID)
	if err != nil || resolved == nil || resolved.GuildID != ctx.Message.GuildID {
		return ctx.SendError("That channel does not exist in this server.")
	}
	if resolved.Type != discordgo.ChannelTypeGuildVoice && resolved.Type != discordgo.ChannelTypeGuildStageVoice {
		return ctx.SendError("Autojoin target must be a voice or stage channel.")
	}
	if ctx.Radio != nil {
		if _, err := ctx.Radio.UpdateSettings(ctx.Context(), coordinator.SettingsUpdate{
			GuildID:              ctx.Message.GuildID,
			AutojoinChannelID:    channelID,
			AutojoinChannelIDSet: true,
		}); err != nil {
			return ctx.SendError("Could not save the autojoin channel.")
		}
	}

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Set autojoin voice channel to <#%s>.", channelID),
		Color:       helpers.ColorSuccess,
	}
	_, errReply := ctx.ReplyEmbed(embed)
	return errReply
}

func (c *RadioGroupCmd) handleSetPlayerChannel(ctx *bot.Context, target string) error {
	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionManageGuild) {
		return nil
	}

	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		curr := ""
		if ctx.Radio != nil {
			if snap, ok := ctx.Radio.Snapshot(ctx.Message.GuildID); ok {
				curr = snap.PlayerChannelID
			}
		}

		desc := "No dedicated radio player channel currently set."
		if curr != "" {
			desc = fmt.Sprintf("Current dedicated radio player channel: <#%s>", curr)
		}
		embed := &discordgo.MessageEmbed{
			Description: desc,
			Color:       helpers.ColorDefault,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	if target == "clear" || target == "off" || target == "disable" || target == "none" {
		if ctx.Radio != nil {
			if _, err := ctx.Radio.UpdateSettings(ctx.Context(), coordinator.SettingsUpdate{
				GuildID:            ctx.Message.GuildID,
				PlayerChannelID:    "",
				PlayerChannelIDSet: true,
			}); err != nil {
				return ctx.SendError("Could not clear dedicated player channel.")
			}
		}

		embed := &discordgo.MessageEmbed{
			Description: "Disabled dedicated radio player channel.",
			Color:       helpers.ColorError,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	channelID := ctx.ParseChannelID(target)
	resolved, err := ctx.Session.Channel(channelID)
	if err != nil || resolved == nil || resolved.GuildID != ctx.Message.GuildID {
		return ctx.SendError("That channel does not exist in this server.")
	}
	if resolved.Type != discordgo.ChannelTypeGuildText {
		return ctx.SendError("Dedicated player channel must be a text channel.")
	}
	if ctx.Radio != nil {
		if _, err := ctx.Radio.UpdateSettings(ctx.Context(), coordinator.SettingsUpdate{
			GuildID:            ctx.Message.GuildID,
			PlayerChannelID:    channelID,
			PlayerChannelIDSet: true,
		}); err != nil {
			return ctx.SendError("Could not save dedicated player channel.")
		}
	}

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Dedicated radio player channel set to <#%s>.\nMessages and uploads sent there will automatically queue tracks.", channelID),
		Color:       helpers.ColorSuccess,
	}
	_, errReply := ctx.ReplyEmbed(embed)
	return errReply
}

func (c *RadioGroupCmd) handle247(ctx *bot.Context) error {
	if !ctx.RequirePermissions(discordgo.PermissionManageGuild) {
		return nil
	}

	current247 := false
	isConnected := false
	if ctx.Radio != nil {
		if snap, ok := ctx.Radio.Snapshot(ctx.Message.GuildID); ok {
			current247 = snap.Is247
			isConnected = snap.VoiceChannelID != ""
		}
	}

	newState := !current247
	if ctx.Radio != nil {
		if _, err := ctx.Radio.UpdateSettings(ctx.Context(), coordinator.SettingsUpdate{
			GuildID:  ctx.Message.GuildID,
			Is247:    newState,
			Is247Set: true,
		}); err != nil {
			return ctx.SendError("Could not update 24/7 mode.")
		}
	}

	status := "Disabled"
	color := helpers.ColorError
	desc := "24/7 Mode disabled."
	if newState {
		status = "Enabled"
		color = helpers.ColorSuccess
		if isConnected {
			desc = "24/7 Mode enabled. The bot will remain in the voice channel indefinitely."
		} else {
			desc = fmt.Sprintf("24/7 Mode enabled. Connect the bot to voice using `%sr join` or set an autojoin channel using `%sr autojoin <channel>`.", ctx.Prefix, ctx.Prefix)
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("24/7 Mode %s", status),
		Description: desc,
		Color:       color,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func (c *RadioGroupCmd) handleForceLeave(ctx *bot.Context) error {
	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
		return nil
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	if _, err = radio.Control(ctx.Context(), coordinator.ControlRequest{
		GuildID: ctx.Message.GuildID,
		Action:  coordinator.ControlLeave,
	}); err != nil {
		return ctx.SendError("Radio is not currently connected to a voice channel.")
	}

	embed := &discordgo.MessageEmbed{
		Description: "Forcibly disconnected radio and purged voice session.",
		Color:       helpers.ColorError,
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}
