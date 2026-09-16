package moderation

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/modlog"

	"github.com/bwmarrin/discordgo"
)

type AntiMP3Cmd struct{}

func (c *AntiMP3Cmd) Name() string        { return "antimp3" }
func (c *AntiMP3Cmd) Aliases() []string   { return []string{"antimusic", "noaudio"} }
func (c *AntiMP3Cmd) Category() string    { return "Moderation" }
func (c *AntiMP3Cmd) Description() string { return "Configures antimp3" }
func (c *AntiMP3Cmd) Usage() string       { return "[normal / strict / disable / status]" }
func (c *AntiMP3Cmd) Example() string     { return "strict" }
func (c *AntiMP3Cmd) Permissions() int64  { return discordgo.PermissionManageMessages }

func (c *AntiMP3Cmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "status", Description: "View current Anti-MP3 status"},
		{Name: "normal", Description: "Enable normal Anti-MP3 audio protection"},
		{Name: "strict", Description: "Enable strict Anti-MP3 audio protection (restricts admins)"},
		{Name: "disable", Description: "Disable Anti-MP3 audio protection"},
	}
}

func (c *AntiMP3Cmd) Execute(ctx *bot.Context) error {
	if len(ctx.Args) == 0 {
		return c.handleStatus(ctx)
	}

	var action string
	switch strings.ToLower(ctx.Args[0]) {
	case "clear", "reset", "disable", "off", "no":
		action = "disable"
	case "normal", "norm", "on":
		action = "normal"
	case "strict", "admin":
		action = "strict"
	default:
		return c.handleStatus(ctx)
	}

	return c.handleChange(ctx, action)
}

func (c *AntiMP3Cmd) handleStatus(ctx *bot.Context) error {
	antiMode, err := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingAntiMP3Mode)
	if err != nil {
		return err
	}

	antiStatus := "enabled"
	if antiMode == "disable" || antiMode == "" {
		antiStatus = "disabled"
		antiMode = "disable"
	}

	return ctx.SendSuccess("Anti-MP3 is **%s** (`%s`).", antiStatus, antiMode)
}

func (c *AntiMP3Cmd) handleChange(ctx *bot.Context, mode string) error {
	antiMode, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingAntiMP3Mode)
	if antiMode == mode {
		return ctx.SendSuccess("Anti-MP3 is already set to **%s**.", mode)
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingAntiMP3Mode, mode); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
	}

	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Anti-MP3 Mode",
		OldValue:  antiMode,
		NewValue:  mode,
		Moderator: ctx.Message.Author,
	})

	switch mode {
	case "normal":
		return ctx.SendSuccess("Anti-MP3 enabled (**Normal mode**).")
	case "strict":
		return ctx.SendSuccess("Anti-MP3 enabled (**Strict mode**).")
	default:
		return ctx.SendSuccess("Anti-MP3 has been **disabled**.")
	}
}

type DisableSelfReactCmd struct{}

func (c *DisableSelfReactCmd) Name() string { return "disableselfreact" }
func (c *DisableSelfReactCmd) Aliases() []string {
	return []string{"noselfreact", "antiselfreact", "selfreact"}
}
func (c *DisableSelfReactCmd) Category() string { return "Moderation" }
func (c *DisableSelfReactCmd) Description() string {
	return "Toggles deletion of user reactions on their own messages."
}
func (c *DisableSelfReactCmd) Usage() string      { return "[enable / disable / status]" }
func (c *DisableSelfReactCmd) Example() string    { return "enable" }
func (c *DisableSelfReactCmd) Permissions() int64 { return discordgo.PermissionManageMessages }

func (c *DisableSelfReactCmd) Execute(ctx *bot.Context) error {
	if len(ctx.Args) == 0 {
		return c.handleStatus(ctx)
	}

	if val, ok := ctx.ParseBool(ctx.Args[0]); ok {
		return c.handleChange(ctx, val)
	}
	return c.handleStatus(ctx)
}

func (c *DisableSelfReactCmd) handleStatus(ctx *bot.Context) error {
	isDisabled, err := ctx.DB.GetGuildSettingBool(ctx.Message.GuildID, database.SettingSelfReactDisabled)
	if err != nil {
		return err
	}

	statusText := "disabled"
	if isDisabled {
		statusText = "enabled"
	}
	return ctx.SendSuccess("Self-reaction deletion is currently **%s**.", statusText)
}

func (c *DisableSelfReactCmd) handleChange(ctx *bot.Context, disableSelfReact bool) error {
	current, err := ctx.DB.GetGuildSettingBool(ctx.Message.GuildID, database.SettingSelfReactDisabled)
	if err == nil && current == disableSelfReact {
		statusText := "disabled"
		if current {
			statusText = "enabled"
		}
		return ctx.SendSuccess("Self-reaction deletion is already **%s**.", statusText)
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingSelfReactDisabled, disableSelfReact); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
	}

	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Self-Reaction Deletion",
		OldValue:  strconv.FormatBool(current),
		NewValue:  strconv.FormatBool(disableSelfReact),
		Moderator: ctx.Message.Author,
	})

	if disableSelfReact {
		return ctx.SendSuccess("Self-reaction deletion has been **enabled**.")
	}
	return ctx.SendSuccess("Self-reaction deletion has been **disabled**.")
}

type PrefixCmd struct{}

func (c *PrefixCmd) Name() string      { return "prefix" }
func (c *PrefixCmd) Aliases() []string { return []string{"setprefix"} }
func (c *PrefixCmd) Category() string  { return "Server Configuration" }
func (c *PrefixCmd) Description() string {
	return "Sets or views the command prefix for this server."
}
func (c *PrefixCmd) Usage() string   { return "[new_prefix]" }
func (c *PrefixCmd) Example() string { return "!" }

func (c *PrefixCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if len(ctx.Args) == 0 {
		ctx.ReplyText(fmt.Sprintf("Current server command prefix is **%s**", ctx.Prefix))
		return nil
	}

	if !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
		return nil
	}

	newPrefix := strings.TrimSpace(ctx.Args[0])
	if len(newPrefix) > 5 {
		return ctx.SendError("Command prefix cannot be longer than 5 characters.")
	} else if len(newPrefix) < 1 {
		return ctx.SendError("Command prefix cannot be empty.")
	}

	currentPrefix, err := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingPrefix)
	if err == nil && currentPrefix == newPrefix {
		return ctx.SendSuccess("Server prefix is already set to **%s**", newPrefix)
	}

	confirmed, err := ctx.PromptConfirmation(fmt.Sprintf("Are you sure you want to change the prefix to: `%s`?", newPrefix))
	if err != nil || !confirmed {
		return err
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingPrefix, newPrefix); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
	}

	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Server Command Prefix",
		OldValue:  currentPrefix,
		NewValue:  newPrefix,
		Moderator: ctx.Message.Author,
	})

	return ctx.SendSuccess("Updated server command prefix to `%s`", newPrefix)
}

type EmbedColorCmd struct{}

func (c *EmbedColorCmd) Name() string      { return "embedcolor" }
func (c *EmbedColorCmd) Aliases() []string { return []string{"setembedcolor", "color"} }
func (c *EmbedColorCmd) Category() string  { return "Server Configuration" }
func (c *EmbedColorCmd) Description() string {
	return "Sets or views the default embed color for this server."
}
func (c *EmbedColorCmd) Usage() string      { return "[hex_code | default | clear]" }
func (c *EmbedColorCmd) Example() string    { return "#7C5CFF" }
func (c *EmbedColorCmd) Permissions() int64 { return discordgo.PermissionAdministrator }

func (c *EmbedColorCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if len(ctx.Args) == 0 {
		current, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingEmbedColor)
		if current == "" {
			return ctx.SendSuccess("Current server embed color is set to default (`#%06X`).", helpers.ColorDefault)
		}
		embed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Current server embed color is **#%s**.", strings.ToUpper(current)),
			Color:       helpers.ParseHexColor(current),
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	if !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
		return nil
	}

	arg := strings.ToLower(strings.TrimSpace(ctx.Args[0]))
	if arg == "default" || arg == "clear" || arg == "reset" || arg == "none" || arg == "remove" {
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingEmbedColor, ""); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to reset embed color: %v", err))
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
			GuildID:   ctx.Message.GuildID,
			Setting:   "Default Embed Color",
			NewValue:  fmt.Sprintf("Default (#%06X)", helpers.ColorDefault),
			Moderator: ctx.Message.Author,
		})
		return ctx.SendSuccess("Reset server embed color to default (`#%06X`).", helpers.ColorDefault)
	}

	col := helpers.ParseHexColor(arg)
	if col == 0 {
		return ctx.SendError("Invalid color code. Provide a valid 6-character hex code (e.g. `#7C5CFF`) or `clear`.")
	}

	hexStr := fmt.Sprintf("%06X", col)
	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingEmbedColor, hexStr); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update embed color: %v", err))
	}

	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Default Embed Color",
		NewValue:  "#" + hexStr,
		Moderator: ctx.Message.Author,
	})

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Updated server embed color to **#%s**.", hexStr),
		Color:       col,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

type SetServerAvatarCmd struct{}

func (c *SetServerAvatarCmd) Name() string { return "setbotserveravatar" }
func (c *SetServerAvatarCmd) Aliases() []string {
	return []string{"setguildbotavatar", "setbotavatar", "setserveravatar", "setguildavatar", "botavatar"}
}
func (c *SetServerAvatarCmd) Category() string    { return "Server Configuration" }
func (c *SetServerAvatarCmd) Description() string { return "Sets the bot's server-specific avatar." }
func (c *SetServerAvatarCmd) Usage() string       { return "[image_url | attachment | clear]" }
func (c *SetServerAvatarCmd) Example() string     { return "clear" }
func (c *SetServerAvatarCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *SetServerAvatarCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if len(ctx.Args) > 0 && strings.ToLower(ctx.Args[0]) == "clear" {
		if err := c.setBotGuildAvatar(ctx, ""); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to clear server avatar: %v", err))
		}
		return ctx.SendSuccess("Successfully removed the server avatar (reverted to global avatar).")
	}

	imageURL := ctx.ExtractImageURL()
	if imageURL == "" {
		return ctx.SendError("Attach an image, provide an image URL, or use the `clear` argument.")
	}

	dataURI, err := helpers.FetchImageAsBase64(imageURL)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to process image: %v", err))
	}

	if err := c.setBotGuildAvatar(ctx, dataURI); err != nil {
		return ctx.SendError(fmt.Sprintf("Discord rejected the avatar change: %v", err))
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: "Successfully updated my server avatar!",
		Image:       &discordgo.MessageEmbedImage{URL: imageURL},
	})
	return err
}

func (c *SetServerAvatarCmd) setBotGuildAvatar(ctx *bot.Context, avatarDataURI string) error {
	payload := struct {
		Avatar *string `json:"avatar"`
	}{}
	if avatarDataURI != "" {
		payload.Avatar = &avatarDataURI
	}
	endpoint := discordgo.EndpointGuildMember(ctx.Message.GuildID, "@me")
	_, err := ctx.Session.RequestWithBucketID("PATCH", endpoint, payload, discordgo.EndpointGuildMember(ctx.Message.GuildID, ""))
	return err
}

type SetServerBannerCmd struct{}

func (c *SetServerBannerCmd) Name() string { return "setbotserverbanner" }
func (c *SetServerBannerCmd) Aliases() []string {
	return []string{"setguildbotbanner", "setbotbanner", "setserverbanner", "setguildbanner", "botbanner"}
}
func (c *SetServerBannerCmd) Category() string    { return "Server Configuration" }
func (c *SetServerBannerCmd) Description() string { return "Sets the bot's server-specific banner." }
func (c *SetServerBannerCmd) Usage() string       { return "[image_url | attachment | clear]" }
func (c *SetServerBannerCmd) Example() string     { return "clear" }
func (c *SetServerBannerCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *SetServerBannerCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if len(ctx.Args) > 0 && strings.ToLower(ctx.Args[0]) == "clear" {
		if err := c.setBotGuildBanner(ctx, ""); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to clear server banner: %v", err))
		}
		return ctx.SendSuccess("Successfully removed the server banner (reverted to global banner).")
	}
	imageURL := ctx.ExtractImageURL()
	if imageURL == "" {
		return ctx.SendError("Attach an image, provide an image URL, or use the `clear` argument.")
	}

	dataURI, err := helpers.FetchImageAsBase64(imageURL)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to process image: %v", err))
	}

	if err := c.setBotGuildBanner(ctx, dataURI); err != nil {
		return ctx.SendError(fmt.Sprintf("Discord rejected the banner change: %v", err))
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: "Successfully updated my server banner!",
		Image:       &discordgo.MessageEmbedImage{URL: imageURL},
	})
	return err
}

func (c *SetServerBannerCmd) setBotGuildBanner(ctx *bot.Context, bannerDataURI string) error {
	payload := struct {
		Banner *string `json:"banner"`
	}{}
	if bannerDataURI != "" {
		payload.Banner = &bannerDataURI
	}
	endpoint := discordgo.EndpointGuildMember(ctx.Message.GuildID, "@me")
	_, err := ctx.Session.RequestWithBucketID("PATCH", endpoint, payload, discordgo.EndpointGuildMember(ctx.Message.GuildID, ""))
	return err
}

type ImagemuteroleCmd struct{}

func setRoleSetting(ctx *bot.Context, cmd bot.CommandInfo, settingKey database.GuildSetting, roleDesc string) (*discordgo.Role, error) {
	if !ctx.RequireGuild() {
		return nil, nil
	}
	if !ctx.RequireArgs(cmd, 1) {
		return nil, nil
	}
	role, err := ctx.ResolveRole(ctx.Args[0])
	if err != nil || role == nil {
		return nil, ctx.SendError("Specify a valid role.")
	}
	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, settingKey, role.ID); err != nil {
		return nil, ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
	}
	if err := ctx.SendSuccess("Set %s to **%s** (`%s`).", roleDesc, role.Name, role.ID); err != nil {
		return nil, err
	}
	return role, nil
}

func (c *ImagemuteroleCmd) Name() string        { return "imagemuterole" }
func (c *ImagemuteroleCmd) Aliases() []string   { return []string{"imuterole"} }
func (c *ImagemuteroleCmd) Category() string    { return "Server Configuration" }
func (c *ImagemuteroleCmd) Description() string { return "Configure role used for image mutes." }
func (c *ImagemuteroleCmd) Usage() string       { return "<@role|roleID>" }
func (c *ImagemuteroleCmd) Example() string     { return "@Image Muted" }
func (c *ImagemuteroleCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *ImagemuteroleCmd) Execute(ctx *bot.Context) error {
	role, err := setRoleSetting(ctx, c, database.SettingImageMuteRoleID, "image mute role")
	if err != nil || role == nil {
		return err
	}
	_ = helpers.EnsureRoleOverwrites(ctx.Session, ctx.Message.GuildID, role.ID, helpers.ImageMuteDenyFlags)
	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Image Mute Role",
		NewValue:  fmt.Sprintf("<@&%s> (`%s`)", role.ID, role.ID),
		Moderator: ctx.Message.Author,
	})
	return nil
}

type ReactionmuteroleCmd struct{}

func (c *ReactionmuteroleCmd) Name() string        { return "reactionmuterole" }
func (c *ReactionmuteroleCmd) Aliases() []string   { return []string{"rmuterole"} }
func (c *ReactionmuteroleCmd) Category() string    { return "Server Configuration" }
func (c *ReactionmuteroleCmd) Description() string { return "Configure role used for reaction mutes." }
func (c *ReactionmuteroleCmd) Usage() string       { return "<@role|roleID>" }
func (c *ReactionmuteroleCmd) Example() string     { return "@Reaction Muted" }
func (c *ReactionmuteroleCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *ReactionmuteroleCmd) Execute(ctx *bot.Context) error {
	role, err := setRoleSetting(ctx, c, database.SettingReactionMuteRoleID, "reaction mute role")
	if err != nil || role == nil {
		return err
	}
	_ = helpers.EnsureRoleOverwrites(ctx.Session, ctx.Message.GuildID, role.ID, helpers.ReactionMuteDenyFlags)
	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Reaction Mute Role",
		NewValue:  fmt.Sprintf("<@&%s> (`%s`)", role.ID, role.ID),
		Moderator: ctx.Message.Author,
	})
	return nil
}

type JailroleCmd struct{}

func (c *JailroleCmd) Name() string        { return "jailrole" }
func (c *JailroleCmd) Aliases() []string   { return []string{"jail-role", "setjailrole"} }
func (c *JailroleCmd) Category() string    { return "Server Configuration" }
func (c *JailroleCmd) Description() string { return "Configure role used for jailed members." }
func (c *JailroleCmd) Usage() string       { return "<@role|roleID>" }
func (c *JailroleCmd) Example() string     { return "@Jailed" }
func (c *JailroleCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *JailroleCmd) Execute(ctx *bot.Context) error {
	role, err := setRoleSetting(ctx, c, database.SettingJailRoleID, "jail role")
	if err != nil || role == nil {
		return err
	}
	_ = helpers.EnsureRoleOverwrites(ctx.Session, ctx.Message.GuildID, role.ID, helpers.JailDenyFlags)
	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Jail Role",
		NewValue:  fmt.Sprintf("<@&%s> (`%s`)", role.ID, role.ID),
		Moderator: ctx.Message.Author,
	})
	return nil
}

type MuteroleCmd struct{}

func (c *MuteroleCmd) Name() string        { return "muterole" }
func (c *MuteroleCmd) Aliases() []string   { return []string{"mute-role", "setmuterole", "muteroleid"} }
func (c *MuteroleCmd) Category() string    { return "Server Configuration" }
func (c *MuteroleCmd) Description() string { return "Configure role used for muted members." }
func (c *MuteroleCmd) Usage() string       { return "<@role|roleID|name>" }
func (c *MuteroleCmd) Example() string     { return "@Muted" }
func (c *MuteroleCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *MuteroleCmd) Execute(ctx *bot.Context) error {
	role, err := setRoleSetting(ctx, c, database.SettingMuteRoleID, "mute role")
	if err != nil || role == nil {
		return err
	}
	_ = helpers.EnsureRoleOverwrites(ctx.Session, ctx.Message.GuildID, role.ID, helpers.MuteDenyFlags)
	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Mute Role",
		NewValue:  fmt.Sprintf("<@&%s> (`%s`)", role.ID, role.ID),
		Moderator: ctx.Message.Author,
	})
	return nil
}

type ModlogCmd struct{}

func (c *ModlogCmd) Name() string        { return "modlog" }
func (c *ModlogCmd) Aliases() []string   { return []string{"modlogchannel", "setmodlog"} }
func (c *ModlogCmd) Category() string    { return "Server Configuration" }
func (c *ModlogCmd) Description() string { return "Configure mod audit log channel." }
func (c *ModlogCmd) Usage() string       { return "<#channel|channelID|name|clear>" }
func (c *ModlogCmd) Example() string     { return "#mod-logs" }
func (c *ModlogCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *ModlogCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	arg := strings.Join(ctx.Args, " ")
	lower := strings.ToLower(arg)
	if lower == "clear" || lower == "none" || lower == "off" || lower == "reset" {
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingModLogChannelID, ""); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
			GuildID:   ctx.Message.GuildID,
			Setting:   "Mod Log Channel",
			NewValue:  "Cleared",
			Moderator: ctx.Message.Author,
		})
		return ctx.SendSuccess("Cleared mod audit log channel.")
	}

	ch, err := ctx.ResolveChannel(arg)
	if err != nil || ch == nil {
		return ctx.SendError(fmt.Sprintf("Could not resolve channel `%s`. Specify a valid channel mention, ID, or name.", arg))
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingModLogChannelID, ch.ID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
	}

	_ = helpers.HideChannel(ctx.Session, ctx.Message.GuildID, ch.ID)
	modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
		GuildID:   ctx.Message.GuildID,
		Setting:   "Mod Log Channel",
		NewValue:  fmt.Sprintf("<#%s> (`%s`)", ch.ID, ch.ID),
		Moderator: ctx.Message.Author,
	})
	return ctx.SendSuccess("Set mod audit log channel to <#%s>.", ch.ID)
}

type StarboardCmd struct{}

func (c *StarboardCmd) Name() string        { return "starboard" }
func (c *StarboardCmd) Aliases() []string   { return []string{"star"} }
func (c *StarboardCmd) Category() string    { return "Server Configuration" }
func (c *StarboardCmd) Description() string { return "Configure starboard system settings." }
func (c *StarboardCmd) Usage() string       { return "[channel #channel | threshold <num> | clear | status]" }
func (c *StarboardCmd) Example() string     { return "channel #starboard" }
func (c *StarboardCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *StarboardCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "channel", Description: "Set the channel for starboard posts", Usage: "<#channel|channelID>", Example: "#starboard"},
		{Name: "threshold", Description: "Set the minimum star reaction threshold", Usage: "<num>", Example: "3"},
		{Name: "clear", Description: "Disable the starboard system", Usage: "", Example: ""},
		{Name: "status", Description: "View current starboard settings", Usage: "", Example: ""},
	}
}

func (c *StarboardCmd) Execute(ctx *bot.Context) error {
	if len(ctx.Args) == 0 {
		return c.handleStatus(ctx)
	}

	switch strings.ToLower(ctx.Args[0]) {
	case "channel":
		if !ctx.RequireArgs(c, 2) {
			return nil
		}
		chArg := strings.Join(ctx.Args[1:], " ")
		ch, err := ctx.ResolveChannel(chArg)
		if err != nil || ch == nil {
			return ctx.SendError(fmt.Sprintf("Could not resolve channel `%s`.", chArg))
		}
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingStarboardChannelID, ch.ID); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
			GuildID:   ctx.Message.GuildID,
			Setting:   "Starboard Channel",
			NewValue:  fmt.Sprintf("<#%s>", ch.ID),
			Moderator: ctx.Message.Author,
		})
		_ = helpers.LockStarboardChannel(ctx.Session, ctx.Message.GuildID, ch.ID)
		desc := fmt.Sprintf("Starboard channel set to <#%s>.", ch.ID)
		return ctx.SendSuccess("%s", desc)

	case "threshold":
		if !ctx.RequireArgs(c, 2) {
			return nil
		}
		num, err := strconv.Atoi(ctx.Args[1])
		if err != nil || num < 1 {
			return ctx.SendError("Threshold must be at least 1.")
		}
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingStarboardThreshold, num); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
			GuildID:   ctx.Message.GuildID,
			Setting:   "Starboard Threshold",
			NewValue:  fmt.Sprintf("%d ⭐", num),
			Moderator: ctx.Message.Author,
		})
		return ctx.SendSuccess("Starboard reaction threshold set to **%d** ⭐.", num)

	case "clear", "disable", "off", "reset":
		confirmed, err := ctx.PromptConfirmation("Are you sure you want to disable and clear the starboard system for this server?")
		if err != nil || !confirmed {
			return err
		}

		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingStarboardChannelID, ""); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update setting: %v", err))
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
			GuildID:   ctx.Message.GuildID,
			Setting:   "Starboard System",
			NewValue:  "Disabled",
			Moderator: ctx.Message.Author,
		})
		return ctx.SendSuccess("Disabled starboard system.")

	default:
		return c.handleStatus(ctx)
	}
}

func (c *StarboardCmd) handleStatus(ctx *bot.Context) error {
	starChan, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingStarboardChannelID)
	threshold, _ := ctx.DB.GetGuildSettingInt(ctx.Message.GuildID, database.SettingStarboardThreshold)
	if threshold <= 0 {
		threshold = 3
	}

	desc := "Starboard is currently **disabled**."
	if starChan != "" {
		desc = fmt.Sprintf("Starboard Channel: <#%s>\nReaction Threshold: **%d** ⭐", starChan, threshold)
	}

	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:       "Starboard Configuration",
		Description: desc,
	})
	return err
}

type AutoroleCmd struct{}

func (c *AutoroleCmd) Name() string      { return "autorole" }
func (c *AutoroleCmd) Aliases() []string { return []string{"ar", "autoroles"} }
func (c *AutoroleCmd) Category() string  { return "Moderation" }
func (c *AutoroleCmd) Description() string {
	return "Configures roles automatically assigned to new members on join."
}
func (c *AutoroleCmd) Usage() string      { return "<add @role | remove @role | clear | list>" }
func (c *AutoroleCmd) Example() string    { return "add @Member" }
func (c *AutoroleCmd) Permissions() int64 { return discordgo.PermissionAdministrator }

func (c *AutoroleCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "add", Description: "Add a role to autoroles for new members", Usage: "<role>", Example: "@Member"},
		{Name: "clear", Description: "Clear all configured autoroles", Usage: "", Example: ""},
		{Name: "list", Description: "List all configured autoroles", Usage: "", Example: ""},
		{Name: "remove", Description: "Remove a role from autoroles", Usage: "<role>", Example: "@Member"},
	}
}

func (c *AutoroleCmd) Execute(ctx *bot.Context) error {
	if len(ctx.Args) == 0 {
		return c.handleList(ctx)
	}

	switch strings.ToLower(ctx.Args[0]) {
	case "add", "set":
		if !ctx.RequireArgs(c, 2) {
			return nil
		}
		roleArg := strings.Join(ctx.Args[1:], " ")
		role, err := ctx.ResolveRole(roleArg)
		if err != nil || role == nil {
			return ctx.SendError(fmt.Sprintf("Role not found: `%s`", roleArg))
		}

		if role.Permissions&(discordgo.PermissionAdministrator|discordgo.PermissionManageGuild|discordgo.PermissionManageRoles|discordgo.PermissionBanMembers|discordgo.PermissionKickMembers) != 0 {
			return ctx.SendError("Cannot assign a role with administrative or moderation permissions as an autorole.")
		}

		if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, role); !ok {
			return ctx.SendError(err.Error())
		}

		currentRoles, errSlice := ctx.DB.GetGuildSettingSlice(ctx.Message.GuildID, database.SettingAutoroleIDs)
		if errSlice != nil && !errors.Is(errSlice, database.ErrNotFound) {
			return ctx.SendError("Failed to query existing autoroles from database.")
		}

		if len(currentRoles) >= 25 {
			return ctx.SendError("Cannot configure more than 25 autoroles per server.")
		}

		for _, rID := range currentRoles {
			if rID == role.ID {
				return ctx.SendError(fmt.Sprintf("Role <@&%s> is already an autorole.", role.ID))
			}
		}

		newRoles := append(currentRoles, role.ID)
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingAutoroleIDs, newRoles); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update autoroles: %v", err))
		}

		modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
			GuildID:   ctx.Message.GuildID,
			Setting:   "Autoroles",
			Details:   fmt.Sprintf("Added <@&%s> (`%s`)", role.ID, role.ID),
			Moderator: ctx.Message.Author,
		})

		return ctx.SendSuccess("Added <@&%s> to autoroles.", role.ID)

	case "remove", "rem", "del", "delete":
		if !ctx.RequireArgs(c, 2) {
			return nil
		}
		roleArg := strings.Join(ctx.Args[1:], " ")
		role, err := ctx.ResolveRole(roleArg)
		if err != nil || role == nil {
			return ctx.SendError(fmt.Sprintf("Role not found: `%s`", roleArg))
		}

		currentRoles, _ := ctx.DB.GetGuildSettingSlice(ctx.Message.GuildID, database.SettingAutoroleIDs)
		found := false
		var newRoles []string
		for _, rID := range currentRoles {
			if rID == role.ID {
				found = true
			} else {
				newRoles = append(newRoles, rID)
			}
		}

		if !found {
			return ctx.SendError(fmt.Sprintf("Role <@&%s> is not configured as an autorole.", role.ID))
		}

		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingAutoroleIDs, newRoles); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update autoroles: %v", err))
		}

		modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
			GuildID:   ctx.Message.GuildID,
			Setting:   "Autoroles",
			Details:   fmt.Sprintf("Removed <@&%s> (`%s`)", role.ID, role.ID),
			Moderator: ctx.Message.Author,
		})

		return ctx.SendSuccess("Removed <@&%s> from autoroles.", role.ID)

	case "clear", "reset":
		confirmed, err := ctx.PromptConfirmation("Are you sure you want to clear all configured autoroles for this server?")
		if err != nil || !confirmed {
			return err
		}

		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingAutoroleIDs, []string{}); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to clear autoroles: %v", err))
		}

		modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
			GuildID:   ctx.Message.GuildID,
			Setting:   "Autoroles",
			Details:   "Cleared all autoroles",
			Moderator: ctx.Message.Author,
		})

		return ctx.SendSuccess("Cleared all autoroles for this server.")

	case "list", "show", "status":
		return c.handleList(ctx)

	default:
		roleArg := strings.Join(ctx.Args, " ")
		role, err := ctx.ResolveRole(roleArg)
		if err == nil && role != nil {
			ctx.Args = []string{"add", roleArg}
			return c.Execute(ctx)
		}
		return c.handleList(ctx)
	}
}

func (c *AutoroleCmd) handleList(ctx *bot.Context) error {
	roles, _ := ctx.DB.GetGuildSettingSlice(ctx.Message.GuildID, database.SettingAutoroleIDs)
	if len(roles) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Title:       "Autoroles",
			Description: "No autoroles are currently configured for this server.",
		})
		return err
	}

	var roleMentions []string
	for _, rID := range roles {
		roleMentions = append(roleMentions, fmt.Sprintf("<@&%s> (`%s`)", rID, rID))
	}

	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Server Autoroles (%d)", len(roles)),
		Description: strings.Join(roleMentions, "\n"),
	})
	return err
}
