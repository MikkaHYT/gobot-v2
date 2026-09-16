package utility

import (
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands/moderation/roles"
)

type RoleInfoCMD struct{}

func (c *RoleInfoCMD) Name() string        { return "roleinfo" }
func (c *RoleInfoCMD) Aliases() []string   { return []string{"ri"} }
func (c *RoleInfoCMD) Category() string    { return "Utility" }
func (c *RoleInfoCMD) Description() string { return "Displays detailed information about a role." }
func (c *RoleInfoCMD) Usage() string       { return "[role | roleid | rolename]" }
func (c *RoleInfoCMD) Example() string     { return "Admin" }

func (c *RoleInfoCMD) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	query := strings.TrimSpace(strings.Join(ctx.Args, " "))
	targetRole, err := ctx.ResolveRole(query)
	if err != nil || targetRole == nil {
		if err != nil {
			return ctx.SendError(err.Error())
		}
		return ctx.SendError("Could not find specified role.")
	}

	embed, err := roles.BuildRoleInfoEmbed(ctx, targetRole)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}
