package utility

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
)

type AfkCmd struct{}

func (c *AfkCmd) Name() string        { return "afk" }
func (c *AfkCmd) Aliases() []string   { return []string{"away"} }
func (c *AfkCmd) Category() string    { return "Utility" }
func (c *AfkCmd) Description() string { return "Set AFK status." }
func (c *AfkCmd) Usage() string       { return "[reason]" }
func (c *AfkCmd) Example() string     { return "sleeping" }

func (c *AfkCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	reason := "AFK"
	if len(ctx.Args) > 0 {
		reason = strings.Join(ctx.Args, " ")
	}

	if err := ctx.DB.SetUserAFK(ctx.Message.GuildID, ctx.Message.Author.ID, reason); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to set AFK status: %v", err))
	}

	return ctx.SendSuccess("Set your AFK status: **%s**", reason)
}
