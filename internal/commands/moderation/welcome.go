package moderation

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/listeners"

	"github.com/bwmarrin/discordgo"
)

type WelcomeCmd struct{}

func (c *WelcomeCmd) Name() string        { return "welcome" }
func (c *WelcomeCmd) Aliases() []string   { return []string{"wlc", "greet"} }
func (c *WelcomeCmd) Category() string    { return "Moderation" }
func (c *WelcomeCmd) Description() string { return "Configures member welcome announcements." }
func (c *WelcomeCmd) Usage() string {
	return "<channel|message|embed|mode|dm|show|test|status> [args]"
}
func (c *WelcomeCmd) Example() string    { return "channel #welcome" }
func (c *WelcomeCmd) Permissions() int64 { return discordgo.PermissionManageGuild }

func (c *WelcomeCmd) HelpFields() []*discordgo.MessageEmbedField {
	return []*discordgo.MessageEmbedField{
		helpers.AnnouncementVariablesEmbedField(),
	}
}

func (c *WelcomeCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "channel", Description: "Set or clear the welcome announcement channel", Usage: "<#channel|clear>", Example: "#welcome"},
		{Name: "message", Description: "Set plain text welcome message", Usage: "<text>", Example: "Welcome {mention} to {server}!"},
		{Name: "embed", Description: "Set custom welcome embed from JSON", Usage: "<json_payload>", Example: `{"title": "Welcome {user.name}"}`},
		{Name: "mode", Description: "Toggle between embed and plain text mode", Usage: "<embed|text>", Example: "embed"},
		{Name: "dm", Description: "Configure private onboarding direct messages", Usage: "<enable|disable|message <text>|embed <json>>", Example: "enable"},
		{Name: "variables", Description: "Display all placeholder variables and their definitions", Usage: "", Example: ""},
		{Name: "show", Description: "Show current welcome message or embed template", Usage: "", Example: ""},
		{Name: "test", Description: "Preview rendered welcome announcement in channel", Usage: "", Example: ""},
		{Name: "status", Description: "Display current welcome configuration", Usage: "", Example: ""},
	}
}

func (c *WelcomeCmd) Execute(ctx *bot.Context) error {
	if len(ctx.Args) == 0 {
		return c.handleStatus(ctx)
	}

	sub := strings.ToLower(ctx.Args[0])
	switch sub {
	case "channel", "setchannel":
		return c.handleChannel(ctx)
	case "message", "msg", "text":
		return c.handleMessage(ctx)
	case "embed":
		return c.handleEmbed(ctx)
	case "mode":
		return c.handleMode(ctx)
	case "dm":
		return c.handleDM(ctx)
	case "variables", "vars", "placeholders":
		return c.handleVariables(ctx)
	case "show", "view":
		return c.handleShow(ctx)
	case "test", "preview":
		return c.handleTest(ctx)
	case "status", "info":
		return c.handleStatus(ctx)
	case "help":
		return c.handleHelp(ctx)
	default:
		return c.handleHelp(ctx)
	}
}

func (c *WelcomeCmd) handleChannel(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "welcome channel <#channel|clear>") {
		return nil
	}

	target := ctx.Args[1]
	if strings.EqualFold(target, "clear") || strings.EqualFold(target, "disable") || strings.EqualFold(target, "none") {
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingWelcomeChannelID, ""); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to clear welcome channel: %v", err))
		}
		return ctx.SendSuccess("Welcome channel cleared.")
	}

	ch, err := ctx.ResolveChannel(target)
	if err != nil || ch == nil || ch.GuildID != ctx.Message.GuildID {
		return ctx.SendError("Invalid text channel specified.")
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingWelcomeChannelID, ch.ID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update welcome channel: %v", err))
	}

	return ctx.SendSuccess("Welcome channel set to <#%s>.", ch.ID)
}

func (c *WelcomeCmd) handleMessage(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "welcome message <text>") {
		return nil
	}

	msg := strings.TrimSpace(strings.Join(ctx.Args[1:], " "))
	if len(msg) > 2000 {
		return ctx.SendError("Welcome message exceeds Discord's maximum limit of 2000 characters.")
	}

	updates := map[database.GuildSetting]any{
		database.SettingWelcomeMessage: msg,
		database.SettingWelcomeIsEmbed: false,
	}

	if err := ctx.DB.UpdateGuildSettings(ctx.Message.GuildID, updates); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to save welcome message: %v", err))
	}

	return ctx.SendSuccess("Welcome message updated.")
}

func (c *WelcomeCmd) handleEmbed(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "welcome embed <json_payload>") {
		return nil
	}

	rawJSON := strings.TrimSpace(strings.Join(ctx.Args[1:], " "))
	targetEmbed, _, err := listeners.LoadEmbedFromJSON(rawJSON)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Invalid JSON embed: %v", err))
	}
	if targetEmbed == nil {
		return ctx.SendError("Parsed embed is empty.")
	}
	if errVal := listeners.ValidateEmbedSize(targetEmbed); errVal != nil {
		return ctx.SendError(errVal.Error())
	}

	updates := map[database.GuildSetting]any{
		database.SettingWelcomeEmbedJSON: rawJSON,
		database.SettingWelcomeIsEmbed:   true,
	}

	if err := ctx.DB.UpdateGuildSettings(ctx.Message.GuildID, updates); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to save welcome embed: %v", err))
	}

	return ctx.SendSuccess("Welcome embed updated and enabled.")
}

func (c *WelcomeCmd) handleMode(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "welcome mode <embed|text>") {
		return nil
	}

	mode := strings.ToLower(ctx.Args[1])
	isEmbed := false
	switch mode {
	case "embed":
		isEmbed = true
	case "text", "plain":
		isEmbed = false
	default:
		return ctx.SendError("Mode must be either `embed` or `text`.")
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingWelcomeIsEmbed, isEmbed); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update welcome mode: %v", err))
	}

	if isEmbed {
		return ctx.SendSuccess("Welcome delivery mode set to **embed**.")
	}
	return ctx.SendSuccess("Welcome delivery mode set to **plain text**.")
}

func (c *WelcomeCmd) handleDM(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "welcome dm <enable|disable|message <text>|embed <json>|mode <embed|text>|show|test>") {
		return nil
	}

	action := strings.ToLower(ctx.Args[1])
	switch action {
	case "enable", "on", "true":
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingWelcomeDMEnabled, true); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to enable welcome DM: %v", err))
		}
		return ctx.SendSuccess("Welcome direct messages enabled.")
	case "disable", "off", "false":
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingWelcomeDMEnabled, false); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to disable welcome DM: %v", err))
		}
		return ctx.SendSuccess("Welcome direct messages disabled.")
	case "message", "msg", "text":
		if !ctx.RequireSubArgs(3, "welcome dm message <text>") {
			return nil
		}
		msg := strings.TrimSpace(strings.Join(ctx.Args[2:], " "))
		if len(msg) > 2000 {
			return ctx.SendError("Welcome DM message exceeds Discord's maximum limit of 2000 characters.")
		}
		updates := map[database.GuildSetting]any{
			database.SettingWelcomeDMMessage: msg,
			database.SettingWelcomeDMIsEmbed: false,
		}
		if err := ctx.DB.UpdateGuildSettings(ctx.Message.GuildID, updates); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update welcome DM message: %v", err))
		}
		return ctx.SendSuccess("Welcome direct message text updated.")
	case "embed":
		if !ctx.RequireSubArgs(3, "welcome dm embed <json>") {
			return nil
		}
		rawJSON := strings.TrimSpace(strings.Join(ctx.Args[2:], " "))
		targetEmbed, _, err := listeners.LoadEmbedFromJSON(rawJSON)
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Invalid JSON embed: %v", err))
		}
		if targetEmbed == nil {
			return ctx.SendError("Parsed embed is empty.")
		}
		if errVal := listeners.ValidateEmbedSize(targetEmbed); errVal != nil {
			return ctx.SendError(errVal.Error())
		}
		updates := map[database.GuildSetting]any{
			database.SettingWelcomeDMEmbedJSON: rawJSON,
			database.SettingWelcomeDMIsEmbed:   true,
		}
		if err := ctx.DB.UpdateGuildSettings(ctx.Message.GuildID, updates); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update welcome DM embed: %v", err))
		}
		return ctx.SendSuccess("Welcome direct message embed updated.")
	case "mode":
		if !ctx.RequireSubArgs(3, "welcome dm mode <embed|text>") {
			return nil
		}
		mode := strings.ToLower(ctx.Args[2])
		isEmbed := false
		switch mode {
		case "embed":
			isEmbed = true
		case "text", "plain":
			isEmbed = false
		default:
			return ctx.SendError("Mode must be either `embed` or `text`.")
		}
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingWelcomeDMIsEmbed, isEmbed); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to update welcome DM mode: %v", err))
		}
		if isEmbed {
			return ctx.SendSuccess("Welcome DM delivery mode set to **embed**.")
		}
		return ctx.SendSuccess("Welcome DM delivery mode set to **plain text**.")
	case "show", "view":
		return c.handleShowDM(ctx)
	case "test", "preview":
		return c.handleTestDM(ctx)
	default:
		return ctx.SendError(fmt.Sprintf("Usage: `%swelcome dm <enable|disable|message <text>|embed <json>|mode <embed|text>|show|test>`", ctx.Prefix))
	}
}

func (c *WelcomeCmd) handleShowDM(ctx *bot.Context) error {
	cfg, err := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if err != nil || cfg == nil {
		return ctx.SendError("Failed to fetch server configuration.")
	}

	if cfg.WelcomeDMIsEmbed {
		if cfg.WelcomeDMEmbedJSON == "" {
			return ctx.SendError("No welcome DM embed configured.")
		}
		return ctx.SendSuccess("Raw welcome DM embed JSON:\n```json\n%s\n```", cfg.WelcomeDMEmbedJSON)
	}

	if cfg.WelcomeDMMessage == "" {
		return ctx.SendError("No welcome DM message configured.")
	}
	return ctx.SendSuccess("Raw welcome DM message:\n```\n%s\n```", cfg.WelcomeDMMessage)
}

func (c *WelcomeCmd) handleShow(ctx *bot.Context) error {
	if len(ctx.Args) > 1 && strings.EqualFold(ctx.Args[1], "dm") {
		return c.handleShowDM(ctx)
	}

	cfg, err := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if err != nil || cfg == nil {
		return ctx.SendError("Failed to fetch server configuration.")
	}

	if cfg.WelcomeIsEmbed {
		if cfg.WelcomeEmbedJSON == "" {
			return ctx.SendError("No welcome embed configured.")
		}
		return ctx.SendSuccess("Raw welcome embed JSON:\n```json\n%s\n```", cfg.WelcomeEmbedJSON)
	}

	if cfg.WelcomeMessage == "" {
		return ctx.SendError("No welcome message configured.")
	}
	return ctx.SendSuccess("Raw welcome message:\n```\n%s\n```", cfg.WelcomeMessage)
}

func (c *WelcomeCmd) handleTestDM(ctx *bot.Context) error {
	cfg, err := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if err != nil || cfg == nil {
		return ctx.SendError("Failed to fetch server configuration.")
	}

	guild, err := ctx.Guild()
	if err != nil {
		return ctx.SendError("Failed to fetch server details.")
	}

	member := ctx.Message.Member
	if member == nil {
		member = &discordgo.Member{
			User: ctx.Message.Author,
		}
	}

	dmChannel, err := ctx.Session.UserChannelCreate(ctx.Message.Author.ID)
	if err != nil {
		return ctx.SendError("Could not open direct messages. Please check your privacy settings.")
	}

	if cfg.WelcomeDMIsEmbed {
		rawJSON := cfg.WelcomeDMEmbedJSON
		if rawJSON == "" {
			rawJSON = cfg.WelcomeEmbedJSON
		}
		if rawJSON == "" {
			return ctx.SendError("No welcome DM embed configured.")
		}
		parsed, _, err := listeners.LoadEmbedFromJSON(rawJSON)
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to render embed template: %v", err))
		}
		rendered := helpers.FormatAnnouncementEmbed(parsed, member, guild)
		if _, errSend := ctx.Session.ChannelMessageSendEmbed(dmChannel.ID, rendered); errSend != nil {
			return ctx.SendError("Failed to deliver test DM. Make sure your server DMs are open.")
		}
		return ctx.SendSuccess("Sent test welcome embed to your direct messages.")
	}

	msg := cfg.WelcomeDMMessage
	if msg == "" {
		msg = cfg.WelcomeMessage
	}
	if msg == "" {
		msg = "Welcome to {server}, {user.name}!"
	}
	rendered := helpers.FormatAnnouncementVariables(msg, member, guild)
	if _, errSend := ctx.Session.ChannelMessageSend(dmChannel.ID, rendered); errSend != nil {
		return ctx.SendError("Failed to deliver test DM. Make sure your server DMs are open.")
	}
	return ctx.SendSuccess("Sent test welcome message to your direct messages.")
}

func (c *WelcomeCmd) handleTest(ctx *bot.Context) error {
	if len(ctx.Args) > 1 && strings.EqualFold(ctx.Args[1], "dm") {
		return c.handleTestDM(ctx)
	}

	cfg, err := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if err != nil || cfg == nil {
		return ctx.SendError("Failed to fetch server configuration.")
	}

	guild, err := ctx.Guild()
	if err != nil {
		return ctx.SendError("Failed to fetch server details.")
	}

	member := ctx.Message.Member
	if member == nil {
		member = &discordgo.Member{
			User: ctx.Message.Author,
		}
	}

	if cfg.WelcomeIsEmbed {
		if cfg.WelcomeEmbedJSON == "" {
			return ctx.SendError("No welcome embed configured.")
		}
		parsed, _, err := listeners.LoadEmbedFromJSON(cfg.WelcomeEmbedJSON)
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to render embed template: %v", err))
		}
		rendered := helpers.FormatAnnouncementEmbed(parsed, member, guild)
		_, errSend := ctx.ReplyEmbed(rendered)
		return errSend
	}

	msg := cfg.WelcomeMessage
	if msg == "" {
		msg = "Welcome {mention} to {server}!"
	}
	rendered := helpers.FormatAnnouncementVariables(msg, member, guild)
	_, errReply := ctx.ReplyText(rendered)
	return errReply
}

func (c *WelcomeCmd) handleStatus(ctx *bot.Context) error {
	cfg, err := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if err != nil || cfg == nil {
		return ctx.SendError("Failed to fetch server configuration.")
	}

	channelStatus := "Disabled"
	if cfg.WelcomeChannelID != "" {
		channelStatus = fmt.Sprintf("<#%s>", cfg.WelcomeChannelID)
	}

	modeStatus := "Plain Text"
	if cfg.WelcomeIsEmbed {
		modeStatus = "Embed"
	}

	dmStatus := "Disabled"
	if cfg.WelcomeDMEnabled {
		if cfg.WelcomeDMIsEmbed {
			dmStatus = "Enabled (Embed)"
		} else {
			dmStatus = "Enabled (Plain Text)"
		}
	}

	desc := fmt.Sprintf(
		"**Channel:** %s\n**Delivery Mode:** %s\n**Direct Messages:** %s\n\n**Available Variables:**\n%s",
		channelStatus, modeStatus, dmStatus, helpers.AnnouncementVariablesHelp(),
	)

	embed := &discordgo.MessageEmbed{
		Title:       "Welcome Configuration",
		Description: desc,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Use %swelcome test to preview channel | %swelcome dm test to preview DM", ctx.Prefix, ctx.Prefix),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *WelcomeCmd) handleVariables(ctx *bot.Context) error {
	embed := &discordgo.MessageEmbed{
		Title:       "Welcome Placeholder Variables",
		Description: "The following variables can be used in plain text messages and JSON embed fields:\n\n" + helpers.AnnouncementVariablesHelp(),
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Use %swelcome test to preview rendering", ctx.Prefix),
		},
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func (c *WelcomeCmd) handleHelp(ctx *bot.Context) error {
	embed := &discordgo.MessageEmbed{
		Title:       "Welcome Command Usage",
		Description: fmt.Sprintf("**Usage:** %s\n**Example:** %s\n\nUse `%swelcome <subcommand>` to configure welcome announcements.", ctx.FormatUsage(c), ctx.FormatExample(c), ctx.Prefix),
		Fields: []*discordgo.MessageEmbedField{
			helpers.AnnouncementVariablesEmbedField(),
		},
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}
