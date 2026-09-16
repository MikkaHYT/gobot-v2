package utility

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/helpers"
	"gobot/internal/listeners"

	"github.com/bwmarrin/discordgo"
)

func generateSecureEmbedSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

type EmbedCmd struct{}

func (c *EmbedCmd) Name() string      { return "embed" }
func (c *EmbedCmd) Aliases() []string { return []string{"ce", "createembed", "embedbuilder", "embeds"} }
func (c *EmbedCmd) Category() string  { return "Utility" }
func (c *EmbedCmd) Description() string {
	return "embed builder with JSON importer/exporter, and server templates."
}
func (c *EmbedCmd) Usage() string {
	return "[json / export / import / template] (json_data | template_name | msg_id)"
}
func (c *EmbedCmd) Example() string {
	return `json {"title": "Hello", "description": "World"}`
}
func (c *EmbedCmd) Permissions() int64 { return discordgo.PermissionManageMessages }

func (c *EmbedCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "build", Description: "Launch interactive embed builder", Usage: "", Example: ""},
		{Name: "json", Description: "Create embed from raw JSON data", Usage: "<json_payload>", Example: `{"title": "Hello", "description": "World"}`},
		{Name: "export", Description: "Export an existing message embed to raw JSON", Usage: "<message_id>", Example: "123456789012345678"},
		{Name: "template", Description: "Load or save a custom embed template", Usage: "<name>", Example: "rules"},
	}
}

func (c *EmbedCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	var (
		initialEmbed *discordgo.MessageEmbed
		initialBtns  []listeners.CustomButton
		err          error
	)

	if len(ctx.Args) > 0 {
		switch strings.ToLower(ctx.Args[0]) {
		case "template", "templates", "tpl":
			return c.handleTemplate(ctx)
		case "export":
			return c.handleExport(ctx)
		case "json":
			return c.handleJSON(ctx)
		case "import":
			initialEmbed, initialBtns, err = c.handleImport(ctx)
			if err != nil {
				return err
			}
		}
	}

	if initialEmbed == nil {
		initialEmbed = defaultEmbed(ctx)
	}

	return launchEmbedSession(ctx, initialEmbed, initialBtns, "", "")
}

func (c *EmbedCmd) handleExport(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "embed export <message_link|message_id>") {
		return nil
	}

	msgInput := ctx.Args[1]
	msg, err := ctx.FetchMessageFromInput(msgInput)
	if err != nil || msg == nil {
		return fmt.Errorf("could not find message from link or ID `%s`: %v", msgInput, err)
	}
	if len(msg.Embeds) == 0 {
		return fmt.Errorf("specified message contains no embeds to export")
	}

	buttons := extractButtons(msg.Components)
	jsonStr, err := listeners.ExportEmbedToJSON(msg.Embeds[0], buttons)
	if err != nil {
		return err
	}

	_, err = ctx.ReplyText(fmt.Sprintf("```json\n%s\n```", jsonStr))
	return err
}

func (c *EmbedCmd) handleJSON(ctx *bot.Context) error {
	jsonInput := strings.TrimSpace(strings.Join(ctx.Args[1:], " "))
	if jsonInput == "" {
		return ctx.SendError("Please provide a valid JSON string or Discohook payload to import.")
	}

	targetEmbed, buttonsList, err := listeners.LoadEmbedFromJSON(jsonInput)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to parse JSON embed: %v", err))
	}
	if targetEmbed == nil {
		return ctx.SendError("Parsed embed is empty.")
	}
	if errVal := listeners.ValidateEmbedSize(targetEmbed); errVal != nil {
		return ctx.SendError(errVal.Error())
	}

	msgSend := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{targetEmbed},
	}
	if len(buttonsList) > 0 {
		msgSend.Components = listeners.CreateCustomButtonsView(buttonsList)
	}

	_, err = ctx.Session.ChannelMessageSendComplex(ctx.Message.ChannelID, msgSend)
	return err
}

func (c *EmbedCmd) handleImport(ctx *bot.Context) (*discordgo.MessageEmbed, []listeners.CustomButton, error) {
	jsonInput := strings.TrimSpace(strings.Join(ctx.Args[1:], " "))
	if jsonInput == "" {
		return nil, nil, ctx.SendError("Please provide a valid JSON string or Discohook payload to import.")
	}

	emb, btns, err := listeners.LoadEmbedFromJSON(jsonInput)
	if err != nil {
		return nil, nil, ctx.SendError(fmt.Sprintf("Failed to parse JSON embed: %v", err))
	}

	return emb, btns, nil
}

func (c *EmbedCmd) handleTemplate(ctx *bot.Context) error {
	subArgs := ctx.SubArgs()
	if len(subArgs) == 0 {
		return c.handleTemplateList(ctx)
	}

	action := strings.ToLower(subArgs[0])
	switch action {
	case "save", "add", "create":
		if !ctx.RequirePermissions(discordgo.PermissionManageMessages) {
			return nil
		}
		if !ctx.RequireSubArgs(3, "embed template save <name> <json_payload>") {
			return nil
		}
		tplName := strings.ToLower(subArgs[1])
		jsonInput := strings.Join(subArgs[2:], " ")

		embed, btns, err := listeners.LoadEmbedFromJSON(jsonInput)
		if err != nil {
			return err
		}

		jsonStr, err := listeners.ExportEmbedToJSON(embed, btns)
		if err != nil {
			return err
		}

		if ctx.DB == nil {
			return fmt.Errorf("database connection unavailable")
		}

		if err := ctx.DB.SaveEmbedTemplate(ctx.Message.GuildID, tplName, jsonStr, ctx.Message.Author.ID); err != nil {
			return err
		}

		confirmEmbed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Successfully saved embed template **%s**.", tplName),
		}
		_, err = ctx.ReplyEmbed(confirmEmbed)
		return err

	case "load", "get", "use":
		if !ctx.RequireSubArgs(2, "embed template load <name>") {
			return nil
		}
		tplName := strings.ToLower(subArgs[1])

		if ctx.DB == nil {
			return fmt.Errorf("database connection unavailable")
		}

		payload, err := ctx.DB.GetEmbedTemplate(ctx.Message.GuildID, tplName)
		if err != nil {
			return fmt.Errorf("failed to load template **%s**: %w", tplName, err)
		}
		if payload == "" {
			return fmt.Errorf("template **%s** not found in this server", tplName)
		}

		embed, btns, err := listeners.LoadEmbedFromJSON(payload)
		if err != nil {
			return fmt.Errorf("failed to parse template embed: %w", err)
		}

		return launchEmbedSession(ctx, embed, btns, "", "")

	case "delete", "remove", "rm":
		if !ctx.RequirePermissions(discordgo.PermissionManageMessages) {
			return nil
		}
		if !ctx.RequireSubArgs(2, "embed template delete <name>") {
			return nil
		}
		tplName := strings.ToLower(subArgs[1])

		if ctx.DB == nil {
			return fmt.Errorf("database connection unavailable")
		}

		confirmed, errPrompt := ctx.PromptConfirmation(fmt.Sprintf("Are you sure you want to delete the embed template **%s**?", tplName))
		if errPrompt != nil || !confirmed {
			return nil
		}

		if err := ctx.DB.DeleteEmbedTemplate(ctx.Message.GuildID, tplName); err != nil {
			return fmt.Errorf("template **%s** does not exist in this server", tplName)
		}

		confirmEmbed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Successfully deleted template **%s**.", tplName),
		}
		_, errReply := ctx.ReplyEmbed(confirmEmbed)
		return errReply

	case "list", "show":
		return c.handleTemplateList(ctx)

	default:
		return c.handleTemplateList(ctx)
	}
}

func (c *EmbedCmd) handleTemplateList(ctx *bot.Context) error {
	if ctx.DB == nil {
		return fmt.Errorf("database connection unavailable")
	}

	names, err := ctx.DB.ListEmbedTemplates(ctx.Message.GuildID)
	if err != nil {
		return fmt.Errorf("failed to list embed templates: %w", err)
	}
	if len(names) == 0 {
		return fmt.Errorf("no embed templates saved for this server. Use `%sembed template save <name> <json>` to create one", ctx.Prefix)
	}

	var lines []string
	for i, name := range names {
		lines = append(lines, fmt.Sprintf("`%d.` **%s**", i+1, name))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Saved Server Embed Templates (%s total)", helpers.FormatNumber(len(names))),
		Description: strings.Join(lines, "\n"),
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type EditembedCmd struct{}

func (c *EditembedCmd) Name() string      { return "editembed" }
func (c *EditembedCmd) Aliases() []string { return []string{"ee", "edite"} }
func (c *EditembedCmd) Category() string  { return "Utility" }
func (c *EditembedCmd) Description() string {
	return "Edits an existing bot embed message."
}
func (c *EditembedCmd) Usage() string      { return "<message_id> [template <name> | json_payload]" }
func (c *EditembedCmd) Example() string    { return `123456789012345678 {"title": "Updated Title"}` }
func (c *EditembedCmd) Permissions() int64 { return discordgo.PermissionManageMessages }

func (c *EditembedCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() || !ctx.RequireArgs(c, 1) {
		return nil
	}

	msg, err := ctx.FetchMessageFromInput(ctx.Args[0])
	if err != nil || msg == nil {
		return fmt.Errorf("could not find message from link or ID `%s`: %v", ctx.Args[0], err)
	}

	if msg.Author == nil || msg.Author.ID != ctx.Session.State.User.ID {
		return fmt.Errorf("cannot edit message `%s`: the message was not sent by this bot", msg.ID)
	}

	if !ctx.IsGuildOwner() && !ctx.IsOwner() {
		perms, errP := ctx.Session.UserChannelPermissions(ctx.Message.Author.ID, msg.ChannelID)
		if errP != nil || (perms&discordgo.PermissionAdministrator == 0 && perms&discordgo.PermissionManageMessages == 0) {
			return fmt.Errorf("you do not have permission to manage messages in <#%s>", msg.ChannelID)
		}
	}

	initialEmbed := &discordgo.MessageEmbed{
		Title:       "Title",
		Description: "Description",
	}
	if len(msg.Embeds) > 0 {
		initialEmbed = msg.Embeds[0]
	}

	buttons := extractButtons(msg.Components)

	if len(ctx.Args) > 1 {
		jsonInput := strings.Join(ctx.Args[1:], " ")
		if strings.EqualFold(ctx.Args[1], "template") || strings.EqualFold(ctx.Args[1], "tpl") {
			if !ctx.RequireSubArgs(3, "editembed <message_id> template <name>") {
				return nil
			}
			tplName := strings.ToLower(ctx.Args[2])
			if ctx.DB == nil {
				return fmt.Errorf("database connection unavailable")
			}
			payload, errDB := ctx.DB.GetEmbedTemplate(ctx.Message.GuildID, tplName)
			if errDB != nil || payload == "" {
				return fmt.Errorf("template **%s** not found in this server", tplName)
			}
			jsonInput = payload
		}

		emb, btns, errP := listeners.LoadEmbedFromJSON(jsonInput)
		if errP != nil {
			return fmt.Errorf("failed to parse JSON embed: %w", errP)
		}
		if errVal := listeners.ValidateEmbedSize(emb); errVal != nil {
			return errVal
		}

		edit := &discordgo.MessageEdit{
			ID:         msg.ID,
			Channel:    msg.ChannelID,
			Embeds:     &[]*discordgo.MessageEmbed{emb},
			Components: &[]discordgo.MessageComponent{},
		}
		if len(btns) > 0 {
			compList := listeners.CreateCustomButtonsView(btns)
			edit.Components = &compList
		}

		if _, errEdit := ctx.Session.ChannelMessageEditComplex(edit); errEdit != nil {
			return fmt.Errorf("failed to edit message: %w", errEdit)
		}

		confirmEmbed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Successfully updated embed in <#%s>! [Jump to Message](%s)", msg.ChannelID, helpers.MessageURL(ctx.Message.GuildID, msg.ChannelID, msg.ID)),
		}
		_, errReply := ctx.ReplyEmbed(confirmEmbed)
		return errReply
	}

	return launchEmbedSession(ctx, initialEmbed, buttons, msg.ID, msg.ChannelID)
}

func defaultEmbed(ctx *bot.Context) *discordgo.MessageEmbed {
	guildName := ""
	guildIcon := ""
	if guild, err := ctx.Guild(); err == nil && guild != nil {
		guildName = guild.Name
		guildIcon = guild.IconURL("")
	}

	embed := &discordgo.MessageEmbed{
		Title:       "title",
		Description: "Desc",
		Color:       helpers.ColorDefault,
	}
	if guildName != "" || guildIcon != "" {
		embed.Footer = &discordgo.MessageEmbedFooter{
			Text:    guildName,
			IconURL: guildIcon,
		}
	}
	return embed
}

func extractButtons(components []discordgo.MessageComponent) []listeners.CustomButton {
	var buttons []listeners.CustomButton
	for _, row := range components {
		actionRow, ok := row.(*discordgo.ActionsRow)
		if !ok {
			continue
		}
		for _, comp := range actionRow.Components {
			btn, ok := comp.(*discordgo.Button)
			if !ok {
				continue
			}
			var emoji string
			if btn.Emoji != nil {
				emoji = btn.Emoji.Name
			}
			buttons = append(buttons, listeners.CustomButton{
				Label: btn.Label,
				URL:   btn.URL,
				Emoji: emoji,
			})
		}
	}
	return buttons
}

func launchEmbedSession(ctx *bot.Context, embed *discordgo.MessageEmbed, btns []listeners.CustomButton, targetMsgID, targetChannelID string) error {
	sessionID := generateSecureEmbedSessionID()
	session := &listeners.EmbedSession{
		SessionID:       sessionID,
		AuthorID:        ctx.Message.Author.ID,
		GuildID:         ctx.Message.GuildID,
		ChannelID:       ctx.Message.ChannelID,
		TargetChannelID: targetChannelID,
		TargetMessageID: targetMsgID,
		Embed:           embed,
		Buttons:         btns,
		Created:         time.Now(),
	}
	listeners.GlobalEmbedSessions.Set(session)

	msgSend := &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{session.Embed},
		Components: listeners.BuildEmbedBuilderComponents(sessionID),
	}
	_, err := ctx.Session.ChannelMessageSendComplex(ctx.Message.ChannelID, msgSend)
	return err
}
