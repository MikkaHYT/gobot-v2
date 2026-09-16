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

type GoodbyeCmd struct{}

func (c *GoodbyeCmd) Name() string        { return "goodbye" }
func (c *GoodbyeCmd) Aliases() []string   { return []string{"leave", "farewell"} }
func (c *GoodbyeCmd) Category() string    { return "Moderation" }
func (c *GoodbyeCmd) Description() string { return "Configures member leave announcements." }
func (c *GoodbyeCmd) Usage() string {
	return "<channel|message|embed|mode|show|test|status> [args]"
}
func (c *GoodbyeCmd) Example() string    { return "channel #goodbye" }
func (c *GoodbyeCmd) Permissions() int64 { return discordgo.PermissionManageGuild }

func (c *GoodbyeCmd) HelpFields() []*discordgo.MessageEmbedField {
	return []*discordgo.MessageEmbedField{
		helpers.AnnouncementVariablesEmbedField(),
	}
}

func (c *GoodbyeCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "channel", Description: "Set or clear the goodbye announcement channel", Usage: "<#channel|clear>", Example: "#goodbye"},
		{Name: "message", Description: "Set plain text leave message", Usage: "<text>", Example: "{user} has left {server}."},
		{Name: "embed", Description: "Set custom goodbye embed from JSON", Usage: "<json_payload>", Example: `{"title": "Member Left", "description": "{user.name} left"}`},
		{Name: "mode", Description: "Toggle between embed and plain text mode", Usage: "<embed|text>", Example: "embed"},
		{Name: "variables", Description: "Display all placeholder variables and their definitions", Usage: "", Example: ""},
		{Name: "show", Description: "Show current goodbye message or embed template", Usage: "", Example: ""},
		{Name: "test", Description: "Preview rendered goodbye announcement in channel", Usage: "", Example: ""},
		{Name: "status", Description: "Display current goodbye configuration", Usage: "", Example: ""},
	}
}

func (c *GoodbyeCmd) Execute(ctx *bot.Context) error {
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

func (c *GoodbyeCmd) handleChannel(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "goodbye channel <#channel|clear>") {
		return nil
	}

	target := ctx.Args[1]
	if strings.EqualFold(target, "clear") || strings.EqualFold(target, "disable") || strings.EqualFold(target, "none") {
		if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingGoodbyeChannelID, ""); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to clear goodbye channel: %v", err))
		}
		return ctx.SendSuccess("Goodbye channel cleared.")
	}

	ch, err := ctx.ResolveChannel(target)
	if err != nil || ch == nil || ch.GuildID != ctx.Message.GuildID {
		return ctx.SendError("Invalid text channel specified.")
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingGoodbyeChannelID, ch.ID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update goodbye channel: %v", err))
	}

	return ctx.SendSuccess("Goodbye channel set to <#%s>.", ch.ID)
}

func (c *GoodbyeCmd) handleMessage(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "goodbye message <text>") {
		return nil
	}

	msg := strings.TrimSpace(strings.Join(ctx.Args[1:], " "))
	if len(msg) > 2000 {
		return ctx.SendError("Goodbye message exceeds Discord's maximum limit of 2000 characters.")
	}

	updates := map[database.GuildSetting]any{
		database.SettingGoodbyeMessage: msg,
		database.SettingGoodbyeIsEmbed: false,
	}

	if err := ctx.DB.UpdateGuildSettings(ctx.Message.GuildID, updates); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to save goodbye message: %v", err))
	}

	return ctx.SendSuccess("Goodbye message updated.")
}

func (c *GoodbyeCmd) handleEmbed(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "goodbye embed <json_payload>") {
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
		database.SettingGoodbyeEmbedJSON: rawJSON,
		database.SettingGoodbyeIsEmbed:   true,
	}

	if err := ctx.DB.UpdateGuildSettings(ctx.Message.GuildID, updates); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to save goodbye embed: %v", err))
	}

	return ctx.SendSuccess("Goodbye embed updated and enabled.")
}

func (c *GoodbyeCmd) handleMode(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "goodbye mode <embed|text>") {
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

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingGoodbyeIsEmbed, isEmbed); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update goodbye mode: %v", err))
	}

	if isEmbed {
		return ctx.SendSuccess("Goodbye delivery mode set to **embed**.")
	}
	return ctx.SendSuccess("Goodbye delivery mode set to **plain text**.")
}

func (c *GoodbyeCmd) handleShow(ctx *bot.Context) error {
	cfg, err := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if err != nil || cfg == nil {
		return ctx.SendError("Failed to fetch server configuration.")
	}

	if cfg.GoodbyeIsEmbed {
		if cfg.GoodbyeEmbedJSON == "" {
			return ctx.SendError("No goodbye embed configured.")
		}
		return ctx.SendSuccess("Raw goodbye embed JSON:\n```json\n%s\n```", cfg.GoodbyeEmbedJSON)
	}

	if cfg.GoodbyeMessage == "" {
		return ctx.SendError("No goodbye message configured.")
	}
	return ctx.SendSuccess("Raw goodbye message:\n```\n%s\n```", cfg.GoodbyeMessage)
}

func (c *GoodbyeCmd) handleTest(ctx *bot.Context) error {
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

	if cfg.GoodbyeIsEmbed {
		if cfg.GoodbyeEmbedJSON == "" {
			return ctx.SendError("No goodbye embed configured.")
		}
		parsed, _, err := listeners.LoadEmbedFromJSON(cfg.GoodbyeEmbedJSON)
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to render embed template: %v", err))
		}
		rendered := helpers.FormatAnnouncementEmbed(parsed, member, guild)
		_, errSend := ctx.ReplyEmbed(rendered)
		return errSend
	}

	msg := cfg.GoodbyeMessage
	if msg == "" {
		msg = "{user} has left {server}."
	}
	rendered := helpers.FormatAnnouncementVariables(msg, member, guild)
	_, errReply := ctx.ReplyText(rendered)
	return errReply
}

func (c *GoodbyeCmd) handleStatus(ctx *bot.Context) error {
	cfg, err := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if err != nil || cfg == nil {
		return ctx.SendError("Failed to fetch server configuration.")
	}

	channelStatus := "Disabled"
	if cfg.GoodbyeChannelID != "" {
		channelStatus = fmt.Sprintf("<#%s>", cfg.GoodbyeChannelID)
	}

	modeStatus := "Plain Text"
	if cfg.GoodbyeIsEmbed {
		modeStatus = "Embed"
	}

	desc := fmt.Sprintf(
		"**Channel:** %s\n**Delivery Mode:** %s\n\n**Available Variables:**\n%s",
		channelStatus, modeStatus, helpers.AnnouncementVariablesHelp(),
	)

	embed := &discordgo.MessageEmbed{
		Title:       "Goodbye Configuration",
		Description: desc,
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Use %sgoodbye test to preview", ctx.Prefix),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *GoodbyeCmd) handleVariables(ctx *bot.Context) error {
	embed := &discordgo.MessageEmbed{
		Title:       "Goodbye Placeholder Variables",
		Description: "The following variables can be used in plain text messages and JSON embed fields:\n\n" + helpers.AnnouncementVariablesHelp(),
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Use %sgoodbye test to preview rendering", ctx.Prefix),
		},
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func (c *GoodbyeCmd) handleHelp(ctx *bot.Context) error {
	embed := &discordgo.MessageEmbed{
		Title:       "Goodbye Command Usage",
		Description: fmt.Sprintf("**Usage:** %s\n**Example:** %s\n\nUse `%sgoodbye <subcommand>` to configure leave announcements.", ctx.FormatUsage(c), ctx.FormatExample(c), ctx.Prefix),
		Fields: []*discordgo.MessageEmbedField{
			helpers.AnnouncementVariablesEmbedField(),
		},
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}
