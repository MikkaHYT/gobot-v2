package bot

import (
	"context"
	"fmt"
	"strings"

	"gobot/config"
	"gobot/internal/database"
	"gobot/internal/policy"
	"gobot/internal/policy/auth"
	"gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

type Context struct {
	Session        *discordgo.Session
	Message        *discordgo.MessageCreate
	Args           []string
	Prefix         string
	InvokedCommand string
	DB             *database.DB
	Policy         *policy.Module
	Radio          *radio.Module
	OwnerIDs       []string
	Config         *config.Config
	GuildConfig    *database.GuildConfig
	Ctx            context.Context
}
func (c *Context) Context() context.Context {
	if c.Ctx != nil {
		return c.Ctx
	}
	if c.Radio != nil {
		return c.Radio.LifecycleContext()
	}
	return context.Background()
}

func (c *Context) Subcommand() string {
	if len(c.Args) == 0 {
		return ""
	}
	return strings.ToLower(c.Args[0])
}

func (c *Context) SubArgs() []string {
	if len(c.Args) <= 1 {
		return []string{}
	}
	return c.Args[1:]
}

func (c *Context) RequireArgs(cmd CommandInfo, minArgs int) bool {
	if len(c.Args) < minArgs {
		_, _ = c.SendUsage(cmd)
		return false
	}
	return true
}

func (c *Context) RequireSubArgs(minArgs int, subUsage string) bool {
	if len(c.Args) < minArgs {
		_ = c.SendError(fmt.Sprintf("Usage: `%s%s`", c.Prefix, subUsage))
		return false
	}
	return true
}

func (c *Context) RequireGuild() bool {
	if c.Message.GuildID == "" {
		_ = c.SendError("This command can only be used in a server.")
		return false
	}
	return true
}

func (c *Context) Authorizer() *auth.Authorizer {
	return auth.NewAuthorizer(auth.NewDiscordStateAdapter(c.Session), c.OwnerIDs)
}

func (c *Context) RequirePermissions(permission int64) bool {
	hasPerm, err := c.HasPermission(permission)
	if err != nil || !hasPerm {
		_ = c.SendError("You do not have permission to execute this command.")
		return false
	}
	return true
}

func (c *Context) HasPermission(perm int64) (bool, error) {
	if c.Message == nil || c.Message.Author == nil {
		return false, nil
	}
	if c.Message.GuildID == "" {
		if perm == 0 {
			return true, nil
		}
		return false, nil
	}

	d := c.Authorizer().Evaluate(c.Context(), auth.Request{
		GuildID:      c.Message.GuildID,
		ChannelID:    c.Message.ChannelID,
		ActorID:      c.Message.Author.ID,
		RequiredPerm: perm,
	})
	if d.Err != nil {
		return false, d.Err
	}
	return d.Allowed, nil
}

func (c *Context) IsOwner() bool {
	if c.Message == nil || c.Message.Author == nil {
		return false
	}
	return c.IsOwnerID(c.Message.Author.ID)
}

func (c *Context) IsOwnerID(userID string) bool {
	if userID == "" {
		return false
	}
	for _, id := range c.OwnerIDs {
		if id == userID {
			return true
		}
	}
	return false
}
