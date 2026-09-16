package moderation

import (
	"errors"
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/helpers"
	"gobot/internal/policy"

	"github.com/bwmarrin/discordgo"
)

type GlobalBlacklistCmd struct{}

func (c *GlobalBlacklistCmd) Name() string      { return "globalblacklist" }
func (c *GlobalBlacklistCmd) Aliases() []string { return []string{"gblacklist", "gbl"} }
func (c *GlobalBlacklistCmd) Category() string  { return "Moderation" }
func (c *GlobalBlacklistCmd) Description() string {
	return "Manage globally blacklisted users and servers across all guilds (bot owners only)."
}
func (c *GlobalBlacklistCmd) Usage() string {
	return "<add|remove|list> <@user|server <id>|id> [reason]"
}
func (c *GlobalBlacklistCmd) Example() string { return "add @MaliciousUser Repeated API abuse" }

func (c *GlobalBlacklistCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "add", Description: "Globally blacklist a user or server from bot usage", Usage: "<@user|server <id>|id> [reason]", Example: "@User Abuse"},
		{Name: "remove", Description: "Remove a user or server from the global blacklist", Usage: "<@user|server <id>|id>", Example: "@User"},
		{Name: "list", Description: "List all globally blacklisted users and servers", Usage: "", Example: ""},
	}
}

func (c *GlobalBlacklistCmd) Execute(ctx *bot.Context) error {
	if !ctx.IsOwner() {
		return ctx.SendError("Only bot owners can manage the global blacklist.")
	}
	if ctx.Policy == nil {
		return ctx.SendError("Policy storage is currently unavailable.")
	}

	if len(ctx.Args) == 0 {
		return c.handleList(ctx)
	}

	action, targetParts := parseActionAndTarget(ctx.Args)
	if action == "list" {
		return c.handleList(ctx)
	}

	if len(targetParts) == 0 {
		return ctx.SendError("Please specify a user or server ID.")
	}

	targetType := "user"
	if strings.ToLower(targetParts[0]) == "server" || strings.ToLower(targetParts[0]) == "guild" {
		targetType = "server"
		targetParts = targetParts[1:]
	}

	if len(targetParts) == 0 {
		return ctx.SendError(fmt.Sprintf("Please specify a %s to %s.", targetType, action))
	}

	targetArg := targetParts[0]
	reason := "Blacklisted by bot owner"
	if len(targetParts) > 1 {
		reason = strings.Join(targetParts[1:], " ")
	}

	targetID, displayName, err := resolveTarget(ctx, targetType, targetArg)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	if action == "add" {
		if targetType == "user" {
			if ctx.IsOwnerID(targetID) {
				return ctx.SendError("Cannot globally blacklist a bot owner.")
			}
			if ctx.Session.State != nil && ctx.Session.State.User != nil && targetID == ctx.Session.State.User.ID {
				return ctx.SendError("Cannot globally blacklist the bot itself.")
			}
		}

		if err := ctx.Policy.AddGlobalRestriction(targetID, targetType, reason, ctx.Message.Author.ID); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to add global blacklist: %v", err))
		}

		_ = ctx.ReactSuccess()
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("Globally blacklisted %s %s.\n**Reason:** %s", targetType, displayName, reason),
		})

		if targetType == "server" {
			_ = ctx.Session.GuildLeave(targetID)
		}
		return err
	}

	_, errGet := ctx.Policy.GetGlobalRestriction(targetID)
	if errGet != nil {
		if !errors.Is(errGet, policy.ErrNotRestricted) {
			return ctx.SendError(fmt.Sprintf("Failed to query global blacklist: %v", errGet))
		}
		return ctx.SendError(fmt.Sprintf("%s %s is not globally blacklisted.", helpers.TitleCase(targetType), displayName))
	}
	if err := ctx.Policy.RemoveGlobalRestriction(targetID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to remove global blacklist: %v", err))
	}
	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Removed %s %s from the global blacklist.", targetType, displayName),
	})
	return err
}

func (c *GlobalBlacklistCmd) handleList(ctx *bot.Context) error {
	entries, err := ctx.Policy.ListGlobalRestrictions("")
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to retrieve global blacklists: %v", err))
	}

	if len(entries) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Title:       "Global Blacklist",
			Description: "No users or servers are currently globally blacklisted.",
		})
		return err
	}

	var userLines []string
	var serverLines []string

	for _, e := range entries {
		switch e.TargetKind {
		case "server", "guild":
			serverLines = append(serverLines, fmt.Sprintf("• Server ID `%s` - %s *(%s)*",
				e.TargetID, e.Reason, e.CreatedAt.Format("2006-01-02 15:04 UTC")))
		default:
			userLines = append(userLines, fmt.Sprintf("• <@%s> (`%s`) - %s *(%s)*",
				e.TargetID, e.TargetID, e.Reason, e.CreatedAt.Format("2006-01-02 15:04 UTC")))
		}
	}

	var fields []*discordgo.MessageEmbedField
	if len(userLines) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Globally Blacklisted Users (%d)", len(userLines)),
			Value:  strings.Join(userLines, "\n"),
			Inline: false,
		})
	}
	if len(serverLines) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Globally Blacklisted Servers (%d)", len(serverLines)),
			Value:  strings.Join(serverLines, "\n"),
			Inline: false,
		})
	}

	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:  "Global Blacklist",
		Fields: fields,
	})
	return err
}

type GlobalWhitelistCmd struct{}

func (c *GlobalWhitelistCmd) Name() string      { return "globalwhitelist" }
func (c *GlobalWhitelistCmd) Aliases() []string { return []string{"gwhitelist", "gwl", "gunblacklist"} }
func (c *GlobalWhitelistCmd) Category() string  { return "Moderation" }
func (c *GlobalWhitelistCmd) Description() string {
	return "Remove a user or server from the global blacklist (bot owners only)."
}
func (c *GlobalWhitelistCmd) Usage() string   { return "<@user|server <id>|id>" }
func (c *GlobalWhitelistCmd) Example() string { return "@User" }

func (c *GlobalWhitelistCmd) Execute(ctx *bot.Context) error {
	if !ctx.IsOwner() {
		return ctx.SendError("Only bot owners can manage the global blacklist.")
	}
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	gbl := &GlobalBlacklistCmd{}
	ctx.Args = append([]string{"remove"}, ctx.Args...)
	return gbl.Execute(ctx)
}
