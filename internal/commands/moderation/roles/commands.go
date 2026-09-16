package roles

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

var Commands = []commands.Command{
	&RoleCmd{},
	&GiveRoleCmd{},
	&RemoveRoleCmd{},
	&TempRoleCmd{},
}

type RoleCmd struct{}

func (c *RoleCmd) Name() string        { return "role" }
func (c *RoleCmd) Aliases() []string   { return []string{"roles"} }
func (c *RoleCmd) Category() string    { return "Moderation" }
func (c *RoleCmd) Description() string { return "server role management." }
func (c *RoleCmd) Usage() string       { return "<user> <role...> | <subcommand> [args]" }
func (c *RoleCmd) Example() string     { return "@Cloudyy VIP" }

func (c *RoleCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "add", Description: "Add a role to a member", Usage: "<user> <role...>", Example: "@Cloudyy VIP"},
		{Name: "remove", Description: "Remove a role from a member", Usage: "<user> <role...>", Example: "@Cloudyy VIP"},
		{Name: "temp", Description: "Temporarily assign a role for a duration", Usage: "<user> <role> <duration>", Example: "@Cloudyy VIP 1h"},
		{Name: "list", Description: "List all members assigned to a role, or all server roles", Usage: "[role]", Example: "VIP"},
		{Name: "create", Description: "Create a new role", Usage: "<name> [hexColor]", Example: "Moderator #3498DB"},
		{Name: "delete", Description: "Delete a role", Usage: "<role>", Example: "Muted"},
		{Name: "rename", Description: "Rename an existing role", Usage: "<role> <new_name>", Example: "VIP Member"},
		{Name: "color", Description: "Set role color", Usage: "<role> <hex>", Example: "VIP #FF5733"},
		{Name: "icon", Description: "Set role icon (emoji, custom emoji, URL, or none)", Usage: "<role> <emoji|URL|none>", Example: "VIP 👑"},
		{Name: "hoist", Description: "Toggle role display in member list", Usage: "<role>", Example: "Admin"},
		{Name: "mentionable", Description: "Toggle role mention permissions", Usage: "<role>", Example: "Announcements"},
		{Name: "info", Description: "Display detailed role information", Usage: "<role>", Example: "Moderator"},
		{Name: "copy", Description: "Duplicate a role's color and permissions", Usage: "<role> <new_name>", Example: "VIP VIP-2"},
	}
}

func (c *RoleCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if len(ctx.Args) == 0 {
		return c.handleList(ctx)
	}

	sub := ctx.Subcommand()

	var err error
	switch sub {
	case "list", "members":
		err = c.handleList(ctx)
	case "info":
		err = c.handleInfo(ctx)
	default:
		if !ctx.RequirePermissions(discordgo.PermissionManageRoles) {
			return nil
		}

		switch sub {
		case "add":
			err = c.handleExplicitAdd(ctx)
		case "remove", "rem", "delmember":
			err = c.handleExplicitRemove(ctx)
		case "temp", "temprole":
			err = c.handleTemp(ctx)
		case "create", "make", "new":
			err = c.handleCreate(ctx)
		case "delete", "del", "destroy":
			err = c.handleDelete(ctx)
		case "rename", "name":
			err = c.handleRename(ctx)
		case "color", "colour":
			err = c.handleColor(ctx)
		case "icon":
			err = c.handleIcon(ctx)
		case "hoist":
			err = c.handleHoist(ctx)
		case "mentionable", "mention":
			err = c.handleMentionable(ctx)
		case "copy", "clone", "dup":
			err = c.handleCopy(ctx)
		default:
			err = c.handleToggle(ctx)
		}
	}

	if err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}

type GiveRoleCmd struct{}

func (c *GiveRoleCmd) Name() string        { return "giverole" }
func (c *GiveRoleCmd) Aliases() []string   { return []string{"gr", "addrole"} }
func (c *GiveRoleCmd) Category() string    { return "Moderation" }
func (c *GiveRoleCmd) Description() string { return "Gives a role to a server member." }
func (c *GiveRoleCmd) Usage() string       { return "<user> <role...>" }
func (c *GiveRoleCmd) Example() string     { return "@Cloudyy VIP" }
func (c *GiveRoleCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *GiveRoleCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	targetUser, targetMember, usedArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || targetMember == nil {
		return ctx.SendError("Could not find a valid user or member.")
	}

	var roleTokens []string
	if usedArg {
		roleTokens = ctx.Args[1:]
	} else {
		roleTokens = ctx.Args
	}

	targetRoles, err := resolveRoles(ctx, roleTokens)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	if err := GiveRoles(ctx, targetUser, targetMember, targetRoles); err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}

type RemoveRoleCmd struct{}

func (c *RemoveRoleCmd) Name() string        { return "removerole" }
func (c *RemoveRoleCmd) Aliases() []string   { return []string{"delrole", "takerole"} }
func (c *RemoveRoleCmd) Category() string    { return "Moderation" }
func (c *RemoveRoleCmd) Description() string { return "Removes a role from a server member." }
func (c *RemoveRoleCmd) Usage() string       { return "<user> <role...>" }
func (c *RemoveRoleCmd) Example() string     { return "@Cloudyy VIP" }
func (c *RemoveRoleCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *RemoveRoleCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	targetUser, targetMember, usedArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || targetMember == nil {
		return ctx.SendError("Could not find a valid user or member.")
	}

	var roleTokens []string
	if usedArg {
		roleTokens = ctx.Args[1:]
	} else {
		roleTokens = ctx.Args
	}

	targetRoles, err := resolveRoles(ctx, roleTokens)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	if err := RemoveRoles(ctx, targetUser, targetMember, targetRoles); err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}

type TempRoleCmd struct{}

func (c *TempRoleCmd) Name() string        { return "temprole" }
func (c *TempRoleCmd) Aliases() []string   { return []string{"tpr", "trole"} }
func (c *TempRoleCmd) Category() string    { return "Moderation" }
func (c *TempRoleCmd) Description() string { return "Temporarily assign a role for a duration." }
func (c *TempRoleCmd) Usage() string       { return "<user> <role> <duration>" }
func (c *TempRoleCmd) Example() string     { return "@Cloudyy VIP 1h" }
func (c *TempRoleCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *TempRoleCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if !ctx.RequireArgs(c, 3) {
		return nil
	}

	targetUser, targetMember, _, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || targetMember == nil {
		return ctx.SendError("Could not find a valid user or member.")
	}

	durStr := ctx.Args[len(ctx.Args)-1]
	duration, err := helpers.ParseDuration(durStr)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Invalid duration: `%s`. Examples: `30m`, `2h`, `7d`", durStr))
	}

	roleArg := strings.Join(ctx.Args[1:len(ctx.Args)-1], " ")
	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return ctx.SendError(fmt.Sprintf("Role not found: `%s`", roleArg))
	}

	if err := TempRole(ctx, targetUser, targetMember, targetRole, duration, durStr); err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}
