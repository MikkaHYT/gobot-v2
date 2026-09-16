package moderation

import (
	"errors"
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/helpers"
	"gobot/internal/modlog"
	"gobot/internal/policy"

	"github.com/bwmarrin/discordgo"
)

type BlacklistCmd struct{}

func (c *BlacklistCmd) Name() string      { return "blacklist" }
func (c *BlacklistCmd) Aliases() []string { return []string{"bl", "ignore"} }
func (c *BlacklistCmd) Category() string  { return "Server Configuration" }
func (c *BlacklistCmd) Description() string {
	return "Manage blacklisted users, roles, channels, servers, or commands from execution."
}
func (c *BlacklistCmd) Usage() string {
	return "<user|role|channel|server|command|list|clear> [add|remove] [target] [--global]"
}
func (c *BlacklistCmd) Example() string { return "user add @BadActor --global" }

func (c *BlacklistCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "user add", Description: "Blacklist a user from using commands", Usage: "<@user|id> [--global] [reason]", Example: "@Cloudyy --global Abuse"},
		{Name: "user remove", Description: "Remove a user from the blacklist", Usage: "<@user|id> [--global]", Example: "@Cloudyy --global"},
		{Name: "server add", Description: "Blacklist a server from bot usage and leave (bot owners)", Usage: "<guild_id> [reason]", Example: "123456789012345678 Abuse"},
		{Name: "server remove", Description: "Remove a server from the blacklist (bot owners)", Usage: "<guild_id>", Example: "123456789012345678"},
		{Name: "server list", Description: "List all blacklisted servers (bot owners)", Usage: "", Example: ""},
		{Name: "role add", Description: "Blacklist a role from using commands", Usage: "<@role|id>", Example: "@Muted"},
		{Name: "role remove", Description: "Remove a role from the server blacklist", Usage: "<@role|id>", Example: "@Muted"},
		{Name: "channel add", Description: "Blacklist a channel from command usage", Usage: "<#channel|id>", Example: "#general"},
		{Name: "channel remove", Description: "Remove a channel from the server blacklist", Usage: "<#channel|id>", Example: "#general"},
		{Name: "command add", Description: "Disable a command server-wide or globally", Usage: "<cmd> [--global]", Example: "cat --global"},
		{Name: "command remove", Description: "Re-enable a command server-wide or globally", Usage: "<cmd> [--global]", Example: "cat --global"},
		{Name: "list", Description: "Display all server blacklists or global blacklist", Usage: "[--global]", Example: "--global"},
		{Name: "clear", Description: "Clear all server blacklists", Usage: "", Example: ""},
	}
}

func (c *BlacklistCmd) Execute(ctx *bot.Context) error {
	if ctx.Policy == nil {
		return ctx.SendError("Policy storage is currently unavailable.")
	}

	isGlobal, filtered := extractGlobalFlag(ctx.Args)
	if len(filtered) == 0 {
		if isGlobal {
			if !ctx.IsOwner() {
				return ctx.SendError("Only bot owners can view the global blacklist.")
			}
			return c.handleGlobalList(ctx)
		}
		if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
			return nil
		}
		return c.handleList(ctx)
	}

	targetType := strings.ToLower(filtered[0])
	switch targetType {
	case "list", "show", "status":
		if isGlobal {
			if !ctx.IsOwner() {
				return ctx.SendError("Only bot owners can view the global blacklist.")
			}
			return c.handleGlobalList(ctx)
		}
		if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
			return nil
		}
		return c.handleList(ctx)

	case "user", "users", "member", "members":
		return c.handleUser(ctx, isGlobal, filtered[1:])

	case "server", "servers", "guild", "guilds":
		return c.handleServer(ctx, filtered[1:])

	case "command", "cmd", "commands", "cmds":
		return c.handleCommand(ctx, isGlobal, filtered[1:])

	case "role", "roles":
		if isGlobal {
			return ctx.SendError("Roles cannot be blacklisted globally.")
		}
		if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
			return nil
		}
		return c.handleTarget(ctx, policy.RestrictionKindRole, "role", filtered[1:])

	case "channel", "channels", "chan":
		if isGlobal {
			return ctx.SendError("Channels cannot be blacklisted globally.")
		}
		if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
			return nil
		}
		return c.handleTarget(ctx, policy.RestrictionKindChannel, "channel", filtered[1:])

	case "clear", "reset":
		if isGlobal {
			return ctx.SendError("Global blacklist cannot be bulk-cleared.")
		}
		if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
			return nil
		}
		return c.handleClear(ctx)

	default:
		if len(filtered) == 1 {
			arg := filtered[0]
			if u, _, err := ctx.ResolveUserAndMember(arg); err == nil && u != nil {
				return c.handleUser(ctx, isGlobal, []string{"add", arg})
			}
			if !isGlobal {
				if r, err := ctx.ResolveRole(arg); err == nil && r != nil {
					return c.handleTarget(ctx, policy.RestrictionKindRole, "role", []string{"add", arg})
				}
				if ch, err := ctx.ResolveChannel(arg); err == nil && ch != nil {
					return c.handleTarget(ctx, policy.RestrictionKindChannel, "channel", []string{"add", arg})
				}
			}
		}
		_, _ = ctx.SendUsage(c)
		return nil
	}
}

func (c *BlacklistCmd) handleUser(ctx *bot.Context, isGlobal bool, args []string) error {
	if len(args) == 0 {
		if isGlobal {
			if !ctx.IsOwner() {
				return ctx.SendError("Only bot owners can view the global blacklist.")
			}
			return c.handleGlobalList(ctx)
		}
		if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
			return nil
		}
		return c.handleSingleList(ctx, policy.RestrictionKindUser, "user")
	}

	action, targetParts := parseActionAndTarget(args)
	if action == "list" {
		if isGlobal {
			if !ctx.IsOwner() {
				return ctx.SendError("Only bot owners can view the global blacklist.")
			}
			return c.handleGlobalList(ctx)
		}
		return c.handleSingleList(ctx, policy.RestrictionKindUser, "user")
	}

	if len(targetParts) == 0 {
		return ctx.SendError("Please specify a user to blacklist.")
	}

	targetArg := targetParts[0]
	reason := "Blacklisted by bot owner"
	if len(targetParts) > 1 {
		reason = strings.Join(targetParts[1:], " ")
	}

	targetID, displayName, err := resolveTarget(ctx, "user", targetArg)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	if isGlobal {
		if !ctx.IsOwner() {
			return ctx.SendError("Only bot owners can manage the global user blacklist.")
		}
		if action == "add" {
			if err := ctx.Policy.AddGlobalRestriction(targetID, "user", reason, ctx.Message.Author.ID); err != nil {
				return ctx.SendError(fmt.Sprintf("Failed to add global blacklist: %v", err))
			}
			_ = ctx.ReactSuccess()
			_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
				Description: fmt.Sprintf("Globally blacklisted %s across all servers.\n**Reason:** %s", displayName, reason),
			})
			return err
		}

		if err := ctx.Policy.RemoveGlobalRestriction(targetID); err != nil {
			if errors.Is(err, policy.ErrNotRestricted) {
				return ctx.SendError(fmt.Sprintf("User %s is not globally blacklisted.", displayName))
			}
			return ctx.SendError(fmt.Sprintf("Failed to remove global blacklist: %v", err))
		}
		_ = ctx.ReactSuccess()
		_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("Removed %s from the global blacklist.", displayName),
		})
		return err
	}

	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
		return nil
	}
	return c.updateGuildBlacklist(ctx, policy.RestrictionKindUser, "user", action, targetID, displayName)
}

func (c *BlacklistCmd) handleServer(ctx *bot.Context, args []string) error {
	if !ctx.IsOwner() {
		return ctx.SendError("Only bot owners can manage the server blacklist.")
	}

	if len(args) == 0 {
		return c.handleServerList(ctx)
	}

	action, targetParts := parseActionAndTarget(args)
	if action == "list" {
		return c.handleServerList(ctx)
	}

	if len(targetParts) == 0 {
		return ctx.SendError("Please specify a server ID to blacklist.")
	}

	targetArg := targetParts[0]
	reason := "Blacklisted by bot owner"
	if len(targetParts) > 1 {
		reason = strings.Join(targetParts[1:], " ")
	}

	guildID, displayName, err := resolveTarget(ctx, "server", targetArg)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	if action == "add" {
		if err := ctx.Policy.AddGlobalRestriction(guildID, "server", reason, ctx.Message.Author.ID); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to add server to blacklist: %v", err))
		}

		_ = ctx.ReactSuccess()
		_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("Globally blacklisted server %s.\n**Reason:** %s", displayName, reason),
		})

		_ = ctx.Session.GuildLeave(guildID)
		return err
	}

	if err := ctx.Policy.RemoveGlobalRestriction(guildID); err != nil {
		if errors.Is(err, policy.ErrNotRestricted) {
			return ctx.SendError(fmt.Sprintf("Server %s is not blacklisted.", displayName))
		}
		return ctx.SendError(fmt.Sprintf("Failed to remove server from blacklist: %v", err))
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Removed server %s from the blacklist.", displayName),
	})
	return err
}

func (c *BlacklistCmd) handleServerList(ctx *bot.Context) error {
	entries, err := ctx.Policy.ListGlobalRestrictions("server")
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to retrieve server blacklists: %v", err))
	}

	if len(entries) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Title:       "Server Blacklist",
			Description: "No servers are currently blacklisted.",
		})
		return err
	}

	var lines []string
	for idx, e := range entries {
		lines = append(lines, fmt.Sprintf("**%d.** Server ID `%s`\n   **Reason:** %s\n   **Added:** %s",
			idx+1, e.TargetID, e.Reason, e.CreatedAt.Format("2006-01-02 15:04:05 UTC")))
	}

	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Blacklisted Servers (%d)", len(entries)),
		Description: strings.Join(lines, "\n\n"),
	})
	return err
}

func (c *BlacklistCmd) handleTarget(ctx *bot.Context, kind policy.GuildRestrictionKind, targetType string, args []string) error {
	if len(args) == 0 {
		return c.handleSingleList(ctx, kind, targetType)
	}

	action, targetParts := parseActionAndTarget(args)
	if action == "list" {
		return c.handleSingleList(ctx, kind, targetType)
	}

	if len(targetParts) == 0 {
		return ctx.SendError(fmt.Sprintf("Please specify a %s to %s.", targetType, action))
	}

	targetArg := strings.Join(targetParts, " ")
	targetID, displayName, err := resolveTarget(ctx, targetType, targetArg)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	return c.updateGuildBlacklist(ctx, kind, targetType, action, targetID, displayName)
}

func (c *BlacklistCmd) updateGuildBlacklist(ctx *bot.Context, kind policy.GuildRestrictionKind, targetType, action, targetID, displayName string) error {
	if action == "add" {
		if err := ctx.Policy.AddGuildRestriction(ctx.Message.GuildID, kind, targetID); err != nil {
			if errors.Is(err, policy.ErrAlreadyRestricted) {
				return ctx.SendError(fmt.Sprintf("%s is already blacklisted in this server.", displayName))
			}
			return ctx.SendError(fmt.Sprintf("Failed to update blacklist: %v", err))
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.BlacklistEvent{
			GuildID:    ctx.Message.GuildID,
			Action:     "Blacklisted",
			TargetType: helpers.TitleCase(targetType),
			TargetName: displayName,
			TargetID:   targetID,
			Moderator:  ctx.Message.Author,
		})
		_ = ctx.ReactSuccess()
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("Blacklisted %s from using commands in this server.", displayName),
		})
		return err
	}

	if err := ctx.Policy.RemoveGuildRestriction(ctx.Message.GuildID, kind, targetID); err != nil {
		if errors.Is(err, policy.ErrNotRestricted) {
			return ctx.SendError(fmt.Sprintf("%s is not in the server blacklist.", displayName))
		}
		return ctx.SendError(fmt.Sprintf("Failed to update blacklist: %v", err))
	}
	modlog.Log(ctx.Session, ctx.DB, &modlog.BlacklistEvent{
		GuildID:    ctx.Message.GuildID,
		Action:     "Removed from Blacklist",
		TargetType: helpers.TitleCase(targetType),
		TargetName: displayName,
		TargetID:   targetID,
		Moderator:  ctx.Message.Author,
	})
	_ = ctx.ReactSuccess()
	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Removed %s from the server blacklist.", displayName),
	})
	return err
}

func (c *BlacklistCmd) handleCommand(ctx *bot.Context, isGlobal bool, args []string) error {
	if len(args) == 0 {
		cmdList := &DisabledcmdsCmd{}
		if isGlobal {
			ctx.Args = []string{"--global"}
		} else {
			ctx.Args = nil
		}
		return cmdList.Execute(ctx)
	}

	action := strings.ToLower(args[0])
	cmdArgs := args[1:]
	if isGlobal {
		cmdArgs = append(cmdArgs, "--global")
	}

	switch action {
	case "add", "disable", "set":
		cmdDisable := &DisablecmdCmd{}
		ctx.Args = cmdArgs
		return cmdDisable.Execute(ctx)
	case "remove", "enable", "del", "delete", "rem":
		cmdEnable := &EnablecmdCmd{}
		ctx.Args = cmdArgs
		return cmdEnable.Execute(ctx)
	case "list", "show":
		cmdList := &DisabledcmdsCmd{}
		if isGlobal {
			ctx.Args = []string{"--global"}
		} else {
			ctx.Args = nil
		}
		return cmdList.Execute(ctx)
	default:
		cmdDisable := &DisablecmdCmd{}
		fullArgs := args
		if isGlobal {
			fullArgs = append(fullArgs, "--global")
		}
		ctx.Args = fullArgs
		return cmdDisable.Execute(ctx)
	}
}

func (c *BlacklistCmd) handleList(ctx *bot.Context) error {
	restr, _ := ctx.Policy.ListGuildRestrictions(ctx.Message.GuildID)
	disabledCmds, _ := ctx.Policy.ListDisabledCommands(policy.Scope{Type: policy.ScopeGuild, GuildID: ctx.Message.GuildID})

	var users, roles, channels []string
	if restr != nil {
		users = restr.UserIDs
		roles = restr.RoleIDs
		channels = restr.ChannelIDs
	}

	if len(users) == 0 && len(roles) == 0 && len(channels) == 0 && len(disabledCmds) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Title:       "Server Blacklists",
			Description: "No users, roles, channels, or commands are blacklisted in this server.",
		})
		return err
	}

	var fields []*discordgo.MessageEmbedField

	if len(users) > 0 {
		var userMentions []string
		for _, uID := range users {
			userMentions = append(userMentions, fmt.Sprintf("<@%s> (`%s`)", uID, uID))
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Blacklisted Users (%d)", len(users)),
			Value:  joinWithinLimit(userMentions, 950),
			Inline: false,
		})
	}

	if len(roles) > 0 {
		var roleMentions []string
		for _, rID := range roles {
			roleMentions = append(roleMentions, fmt.Sprintf("<@&%s> (`%s`)", rID, rID))
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Blacklisted Roles (%d)", len(roles)),
			Value:  joinWithinLimit(roleMentions, 950),
			Inline: false,
		})
	}

	if len(channels) > 0 {
		var channelMentions []string
		for _, chID := range channels {
			channelMentions = append(channelMentions, fmt.Sprintf("<#%s>", chID))
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Blacklisted Channels (%d)", len(channels)),
			Value:  joinWithinLimit(channelMentions, 950),
			Inline: false,
		})
	}

	if len(disabledCmds) > 0 {
		quotedCmds := make([]string, len(disabledCmds))
		for i, cmd := range disabledCmds {
			quotedCmds[i] = "`" + cmd + "`"
		}
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Disabled Commands (%d)", len(disabledCmds)),
			Value:  joinWithinLimit(quotedCmds, 950),
			Inline: false,
		})
	}

	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:  "Server Blacklists",
		Fields: fields,
	})
	return err
}

func joinWithinLimit(lines []string, limit int) string {
	var sb strings.Builder
	omitted := 0
	for _, line := range lines {
		if sb.Len()+len(line)+1 > limit {
			omitted++
			continue
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	if omitted > 0 {
		fmt.Fprintf(&sb, "... and %d more not shown.", omitted)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (c *BlacklistCmd) handleGlobalList(ctx *bot.Context) error {
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
			serverLines = append(serverLines, fmt.Sprintf("- Server ID `%s` - %s *(%s)*",
				e.TargetID, e.Reason, e.CreatedAt.Format("2006-01-02 15:04 UTC")))
		default:
			userLines = append(userLines, fmt.Sprintf("- <@%s> (`%s`) - %s *(%s)*",
				e.TargetID, e.TargetID, e.Reason, e.CreatedAt.Format("2006-01-02 15:04 UTC")))
		}
	}

	var fields []*discordgo.MessageEmbedField
	if len(userLines) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Globally Blacklisted Users (%d)", len(userLines)),
			Value:  joinWithinLimit(userLines, 950),
			Inline: false,
		})
	}
	if len(serverLines) > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Globally Blacklisted Servers (%d)", len(serverLines)),
			Value:  joinWithinLimit(serverLines, 950),
			Inline: false,
		})
	}

	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:  "Global Blacklist",
		Fields: fields,
	})
	return err
}

func (c *BlacklistCmd) handleSingleList(ctx *bot.Context, kind policy.GuildRestrictionKind, targetType string) error {
	restr, err := ctx.Policy.ListGuildRestrictions(ctx.Message.GuildID)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to retrieve the %s blacklist: %v", targetType, err))
	}

	var items []string
	if restr != nil {
		switch kind {
		case policy.RestrictionKindUser:
			items = restr.UserIDs
		case policy.RestrictionKindRole:
			items = restr.RoleIDs
		case policy.RestrictionKindChannel:
			items = restr.ChannelIDs
		}
	}

	if len(items) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("No %ss are blacklisted in this server.", targetType),
		})
		return err
	}

	var mentions []string
	for _, id := range items {
		switch targetType {
		case "user":
			mentions = append(mentions, fmt.Sprintf("<@%s> (`%s`)", id, id))
		case "role":
			mentions = append(mentions, fmt.Sprintf("<@&%s> (`%s`)", id, id))
		case "channel":
			mentions = append(mentions, fmt.Sprintf("<#%s>", id))
		}
	}

	_, errReply := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Blacklisted Items: %s (%d)", helpers.TitleCase(targetType), len(items)),
		Description: joinWithinLimit(mentions, 1900),
	})
	return errReply
}

func (c *BlacklistCmd) handleClear(ctx *bot.Context) error {
	if err := ctx.Policy.ClearGuildRestrictions(ctx.Message.GuildID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to clear server blacklist: %v", err))
	}

	modlog.Log(ctx.Session, ctx.DB, &modlog.BlacklistEvent{
		GuildID:    ctx.Message.GuildID,
		Action:     "Blacklist Cleared",
		TargetType: "Server Blacklist",
		TargetName: "All users, roles, and channels",
		Moderator:  ctx.Message.Author,
	})

	_ = ctx.ReactSuccess()
	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: "Cleared all blacklisted users, roles, and channels for this server.",
	})
	return err
}

type WhitelistCmd struct{}

func (c *WhitelistCmd) Name() string      { return "whitelist" }
func (c *WhitelistCmd) Aliases() []string { return []string{"wl", "unblacklist", "unignore"} }
func (c *WhitelistCmd) Category() string  { return "Server Configuration" }
func (c *WhitelistCmd) Description() string {
	return "Remove a user, role, channel, server, or command from the blacklist."
}
func (c *WhitelistCmd) Usage() string {
	return "<@user|@role|#channel|server <id>|command <cmd>> [--global]"
}
func (c *WhitelistCmd) Example() string { return "@User --global" }

func (c *WhitelistCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	isGlobal, filtered := extractGlobalFlag(ctx.Args)
	if len(filtered) == 0 {
		_, _ = ctx.SendUsage(c)
		return nil
	}

	first := strings.ToLower(filtered[0])
	if first == "command" || first == "cmd" {
		enableCmd := &EnablecmdCmd{}
		cmdArgs := filtered[1:]
		if isGlobal {
			cmdArgs = append(cmdArgs, "--global")
		}
		ctx.Args = cmdArgs
		return enableCmd.Execute(ctx)
	}

	if first == "server" || first == "guild" {
		blCmd := &BlacklistCmd{}
		return blCmd.handleServer(ctx, append([]string{"remove"}, filtered[1:]...))
	}

	if first == "user" || first == "users" {
		blCmd := &BlacklistCmd{}
		return blCmd.handleUser(ctx, isGlobal, append([]string{"remove"}, filtered[1:]...))
	}

	if isGlobal {
		blCmd := &BlacklistCmd{}
		return blCmd.handleUser(ctx, true, append([]string{"remove"}, filtered...))
	}

	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
		return nil
	}

	blCmd := &BlacklistCmd{}
	arg := strings.Join(filtered, " ")

	targetID, displayName, err := resolveTarget(ctx, "user", arg)
	if err == nil {
		return blCmd.updateGuildBlacklist(ctx, policy.RestrictionKindUser, "user", "remove", targetID, displayName)
	}

	targetID, displayName, err = resolveTarget(ctx, "role", arg)
	if err == nil {
		return blCmd.updateGuildBlacklist(ctx, policy.RestrictionKindRole, "role", "remove", targetID, displayName)
	}

	targetID, displayName, err = resolveTarget(ctx, "channel", arg)
	if err == nil {
		return blCmd.updateGuildBlacklist(ctx, policy.RestrictionKindChannel, "channel", "remove", targetID, displayName)
	}

	return ctx.SendError(fmt.Sprintf("Could not resolve `%s` to a user, role, or channel.", arg))
}

type DisablecmdCmd struct{}

func (c *DisablecmdCmd) Name() string { return "disablecmd" }
func (c *DisablecmdCmd) Aliases() []string {
	return []string{"disablecommand", "gdisablecmd", "globaldisablecmd"}
}
func (c *DisablecmdCmd) Category() string { return "Server Configuration" }
func (c *DisablecmdCmd) Description() string {
	return "Disable a command server-wide or globally (bot owners)."
}
func (c *DisablecmdCmd) Usage() string   { return "<command_name> [--global]" }
func (c *DisablecmdCmd) Example() string { return "cat --global" }

func (c *DisablecmdCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	isGlobal, filtered := extractGlobalFlag(ctx.Args)
	if !isGlobal && (ctx.InvokedCommand == "gdisablecmd" || ctx.InvokedCommand == "globaldisablecmd") {
		isGlobal = true
	}
	if len(filtered) < 1 {
		_, _ = ctx.SendUsage(c)
		return nil
	}

	cmdName := strings.ToLower(filtered[0])

	if isGlobal {
		if !ctx.IsOwner() {
			return ctx.SendError("Only bot owners can disable commands globally.")
		}
		if err := ctx.Policy.DisableCommand(policy.Scope{Type: policy.ScopeGlobal}, cmdName, ctx.Message.Author.ID); err != nil {
			if errors.Is(err, policy.ErrProtectedCommand) {
				return ctx.SendError("You cannot disable core commands.")
			}
			return ctx.SendError(fmt.Sprintf("Failed to disable command `%s` globally: %v", cmdName, err))
		}
		_ = ctx.ReactSuccess()
		embed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Disabled command `%s` **globally** across all servers.", cmdName),
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
		return nil
	}

	if err := ctx.Policy.DisableCommand(policy.Scope{Type: policy.ScopeGuild, GuildID: ctx.Message.GuildID}, cmdName, ctx.Message.Author.ID); err != nil {
		if errors.Is(err, policy.ErrProtectedCommand) {
			return ctx.SendError("You cannot disable core commands.")
		}
		return ctx.SendError(fmt.Sprintf("Failed to disable command `%s`: %v", cmdName, err))
	}

	modlog.Log(ctx.Session, ctx.DB, &modlog.BlacklistEvent{
		GuildID:    ctx.Message.GuildID,
		Action:     "Command Disabled",
		TargetType: "Command",
		TargetName: "`" + cmdName + "`",
		Moderator:  ctx.Message.Author,
	})

	_ = ctx.ReactSuccess()
	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Disabled command `%s` server-wide.", cmdName),
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

type EnablecmdCmd struct{}

func (c *EnablecmdCmd) Name() string { return "enablecmd" }
func (c *EnablecmdCmd) Aliases() []string {
	return []string{"enablecommand", "genablecmd", "globalenablecmd"}
}
func (c *EnablecmdCmd) Category() string { return "Server Configuration" }
func (c *EnablecmdCmd) Description() string {
	return "Re-enable a disabled command server-wide or globally."
}
func (c *EnablecmdCmd) Usage() string   { return "<command_name> [--global]" }
func (c *EnablecmdCmd) Example() string { return "cat --global" }

func (c *EnablecmdCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	isGlobal, filtered := extractGlobalFlag(ctx.Args)
	if !isGlobal && (ctx.InvokedCommand == "genablecmd" || ctx.InvokedCommand == "globalenablecmd") {
		isGlobal = true
	}
	if len(filtered) < 1 {
		_, _ = ctx.SendUsage(c)
		return nil
	}

	cmdName := strings.ToLower(filtered[0])

	if isGlobal {
		if !ctx.IsOwner() {
			return ctx.SendError("Only bot owners can re-enable commands globally.")
		}
		if err := ctx.Policy.EnableCommand(policy.Scope{Type: policy.ScopeGlobal}, cmdName); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to enable command `%s` globally: %v", cmdName, err))
		}
		_ = ctx.ReactSuccess()
		embed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Re-enabled command `%s` **globally**.", cmdName),
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	if !ctx.RequireGuild() || !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
		return nil
	}

	if err := ctx.Policy.EnableCommand(policy.Scope{Type: policy.ScopeGuild, GuildID: ctx.Message.GuildID}, cmdName); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to enable command `%s`: %v", cmdName, err))
	}

	modlog.Log(ctx.Session, ctx.DB, &modlog.BlacklistEvent{
		GuildID:    ctx.Message.GuildID,
		Action:     "Command Enabled",
		TargetType: "Command",
		TargetName: "`" + cmdName + "`",
		Moderator:  ctx.Message.Author,
	})

	_ = ctx.ReactSuccess()
	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Re-enabled command `%s`.", cmdName),
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

type DisabledcmdsCmd struct{}

func (c *DisabledcmdsCmd) Name() string      { return "disabledcmds" }
func (c *DisabledcmdsCmd) Aliases() []string { return []string{"disabledcommands", "listdisabled"} }
func (c *DisabledcmdsCmd) Category() string  { return "Server Configuration" }
func (c *DisabledcmdsCmd) Description() string {
	return "List all disabled commands in this server or globally."
}
func (c *DisabledcmdsCmd) Usage() string   { return "[--global]" }
func (c *DisabledcmdsCmd) Example() string { return "--global" }

func (c *DisabledcmdsCmd) Execute(ctx *bot.Context) error {
	isGlobal := false
	if len(ctx.Args) > 0 {
		isGlobal, _ = extractGlobalFlag(ctx.Args)
	}
	if !isGlobal && (ctx.InvokedCommand == "gdisabledcmds" || ctx.InvokedCommand == "globaldisabledcmds") {
		isGlobal = true
	}
	scope := policy.Scope{Type: policy.ScopeGuild, GuildID: ctx.Message.GuildID}
	targetText := "this server"
	titleText := "Disabled Commands"

	if isGlobal {
		if !ctx.IsOwner() {
			return ctx.SendError("Only bot owners can view globally disabled commands.")
		}
		scope = policy.Scope{Type: policy.ScopeGlobal}
		targetText = "globally"
		titleText = "Globally Disabled Commands"
	} else if !ctx.RequireGuild() {
		return nil
	}

	cmds, err := ctx.Policy.ListDisabledCommands(scope)
	if err != nil || len(cmds) == 0 {
		embed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("No commands are currently disabled %s.", targetText),
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s (%d)", titleText, len(cmds)),
		Description: fmt.Sprintf("`%s`", strings.Join(cmds, "`, `")),
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func extractGlobalFlag(args []string) (bool, []string) {
	isGlobal := false
	var filtered []string
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "--global" || lower == "-g" || lower == "-global" {
			isGlobal = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	return isGlobal, filtered
}

func parseActionAndTarget(args []string) (string, []string) {
	if len(args) == 0 {
		return "list", nil
	}
	first := strings.ToLower(args[0])
	switch first {
	case "add", "set":
		return "add", args[1:]
	case "remove", "delete", "del", "rem":
		return "remove", args[1:]
	case "list", "show":
		return "list", args[1:]
	default:
		return "add", args
	}
}

func resolveTarget(ctx *bot.Context, targetType, targetArg string) (string, string, error) {
	targetArg = strings.TrimSpace(targetArg)
	if targetArg == "" {
		return "", "", fmt.Errorf("no %s specified", targetType)
	}

	switch targetType {
	case "user":
		if u, _, err := ctx.ResolveUserAndMember(targetArg); err == nil && u != nil {
			return u.ID, fmt.Sprintf("<@%s> (`%s`)", u.ID, u.Username), nil
		}
		if rawID := ctx.ParseUserID(targetArg); rawID != "" {
			return rawID, fmt.Sprintf("<@%s> (`%s`)", rawID, rawID), nil
		}
		return "", "", fmt.Errorf("could not resolve user `%s`", targetArg)

	case "role":
		if r, err := ctx.ResolveRole(targetArg); err == nil && r != nil {
			return r.ID, fmt.Sprintf("<@&%s> (`%s`)", r.ID, r.Name), nil
		}
		if rawID := ctx.ParseRoleID(targetArg); rawID != "" {
			return rawID, fmt.Sprintf("<@&%s> (`%s`)", rawID, rawID), nil
		}
		return "", "", fmt.Errorf("could not resolve role `%s`", targetArg)

	case "channel":
		if ch, err := ctx.ResolveChannel(targetArg); err == nil && ch != nil {
			return ch.ID, fmt.Sprintf("<#%s>", ch.ID), nil
		}
		if rawID := ctx.ParseChannelID(targetArg); rawID != "" {
			return rawID, fmt.Sprintf("<#%s>", rawID), nil
		}
		return "", "", fmt.Errorf("could not resolve channel `%s`", targetArg)

	case "server", "guild":
		guildID := targetArg
		guildName := targetArg
		if g, err := ctx.Session.Guild(targetArg); err == nil && g != nil {
			guildName = g.Name
		} else if ctx.Session.State != nil {
			if g, err := ctx.Session.State.Guild(targetArg); err == nil && g != nil {
				guildName = g.Name
			}
		}
		if helpers.IsSnowflake(guildID) {
			if guildName != guildID {
				return guildID, fmt.Sprintf("**%s** (`%s`)", guildName, guildID), nil
			}
			return guildID, fmt.Sprintf("`%s`", guildID), nil
		}
		return "", "", fmt.Errorf("could not resolve server ID `%s`", targetArg)

	default:
		return "", "", fmt.Errorf("unknown target type `%s`", targetType)
	}
}
