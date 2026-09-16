package moderation

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type ReactionRoleCmd struct{}

func (c *ReactionRoleCmd) Name() string      { return "reactionrole" }
func (c *ReactionRoleCmd) Aliases() []string { return []string{"rr", "reactionroles", "addrr"} }
func (c *ReactionRoleCmd) Category() string  { return "Moderation" }
func (c *ReactionRoleCmd) Description() string {
	return "Sets up or manages reaction roles for messages."
}
func (c *ReactionRoleCmd) Usage() string {
	return "[add / remove / clear / list] [message_id] [emoji] [role]"
}
func (c *ReactionRoleCmd) Example() string { return "add 123456789012345678 👍 @Member" }

func (c *ReactionRoleCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "add", Description: "Bind a reaction role to a message", Usage: "<message_id> <emoji> <@role>", Example: "123456789012345678 👍 @Member"},
		{Name: "remove", Description: "Remove a reaction role binding", Usage: "<message_id> <emoji>", Example: "123456789012345678 👍"},
		{Name: "clear", Description: "Clear all reaction roles from a message", Usage: "<message_id>", Example: "123456789012345678"},
		{Name: "list", Description: "List all active reaction roles in the server", Usage: "", Example: ""},
	}
}

func (c *ReactionRoleCmd) Permissions() int64 { return discordgo.PermissionManageRoles }

func (c *ReactionRoleCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if ctx.DB == nil {
		return ctx.SendError("Database connection is currently unavailable.")
	}

	if len(ctx.Args) == 0 {
		return c.handleList(ctx)
	}

	subCmd := strings.ToLower(ctx.Args[0])
	switch subCmd {
	case "add", "bind", "create":
		return c.handleAdd(ctx, ctx.SubArgs())
	case "remove", "delete", "rm", "unbind":
		return c.handleRemove(ctx)
	case "clear", "purge":
		return c.handleClear(ctx)
	case "list", "show":
		return c.handleList(ctx)
	default:
		return c.handleAdd(ctx, ctx.Args)
	}
}

func (c *ReactionRoleCmd) handleAdd(ctx *bot.Context, args []string) error {
	if len(args) < 3 {
		_, _ = ctx.SendUsage(c)
		return nil
	}

	messageID := args[0]
	emoji := args[1]
	roleArg := strings.Join(args[2:], " ")

	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return ctx.SendError(fmt.Sprintf("Role not found: `%s`", roleArg))
	}

	if ok, err := helpers.CanMemberManageRole(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetRole); !ok {
		return ctx.SendError(err.Error())
	}
	if ok, err := helpers.CanBotManageRole(ctx.Session, ctx.Message.GuildID, targetRole); !ok {
		return ctx.SendError(err.Error())
	}

	msg, err := ctx.Session.ChannelMessage(ctx.Message.ChannelID, messageID)
	if err != nil || msg == nil {
		return ctx.SendError(fmt.Sprintf("Could not find a message with ID `%s` in this channel.", messageID))
	}

	if err := ctx.Session.MessageReactionAdd(ctx.Message.ChannelID, messageID, emoji); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to add reaction %s to message `%s`: %v", emoji, messageID, err))
	}

	if err := ctx.DB.SaveReactionRoleBinding(messageID, ctx.Message.GuildID, ctx.Message.ChannelID, emoji, targetRole.ID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to save reaction role binding: %v", err))
	}

	_ = ctx.ReactSuccess()
	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Successfully bound %s to role <@&%s> on message `%s`.", emoji, targetRole.ID, messageID),
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *ReactionRoleCmd) handleRemove(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "reactionrole remove <message_id> <emoji>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	messageID := subArgs[0]
	emoji := subArgs[1]

	if err := ctx.DB.DeleteReactionRoleBinding(ctx.Message.GuildID, messageID, emoji); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to delete reaction role binding: %v", err))
	}

	_ = ctx.Session.MessageReactionRemove(ctx.Message.ChannelID, messageID, emoji, "@me")

	_ = ctx.ReactSuccess()
	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Successfully removed reaction role binding %s from message `%s`.", emoji, messageID),
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func (c *ReactionRoleCmd) handleClear(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "reactionrole clear <message_id>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	messageID := subArgs[0]

	prompt := fmt.Sprintf("Are you sure you want to clear all reaction role bindings on message `%s`?", messageID)
	confirmed, err := ctx.PromptConfirmation(prompt)
	if err != nil || !confirmed {
		return err
	}

	bindings, err := ctx.DB.GetReactionRolesForMessage(ctx.Message.GuildID, messageID)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to retrieve existing reaction roles: %v", err))
	}
	for _, b := range bindings {
		_ = ctx.Session.MessageReactionRemove(ctx.Message.ChannelID, messageID, b.Emoji, "@me")
	}

	if err := ctx.DB.DeleteAllReactionRolesForMessage(ctx.Message.GuildID, messageID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to clear reaction roles: %v", err))
	}

	_ = ctx.ReactSuccess()
	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Successfully cleared all reaction roles for message `%s`.", messageID),
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *ReactionRoleCmd) handleList(ctx *bot.Context) error {
	bindings, err := ctx.DB.GetGuildReactionRoles(ctx.Message.GuildID)
	if err != nil || len(bindings) == 0 {
		return ctx.SendError("No reaction role bindings set for this server.")
	}

	var lines []string
	for i, b := range bindings {
		if i >= 15 {
			lines = append(lines, fmt.Sprintf("-# *(+%d more)*", len(bindings)-15))
			break
		}
		roleMention := fmt.Sprintf("<@&%s>", b.RoleID)
		lines = append(lines, fmt.Sprintf("`%d.` %s > %s (Msg: `%s`)", i+1, b.Emoji, roleMention, b.MessageID))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Reaction Roles (%d total)", len(bindings)),
		Description: strings.Join(lines, "\n"),
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}
