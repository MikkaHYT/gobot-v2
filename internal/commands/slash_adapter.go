package commands

import (
	"fmt"
	"strings"

	"gobot/internal/bot"

	"github.com/bwmarrin/discordgo"
)

type SlashOnly interface {
	IsSlashOnly() bool
}

type slashAdapter struct {
	cmd     *discordgo.ApplicationCommand
	cat     string
	aliases []string
}

func FromSlash(cmd *discordgo.ApplicationCommand, category string, aliases ...string) Command {
	return &slashAdapter{cmd: cmd, cat: category, aliases: aliases}
}

func (a *slashAdapter) Name() string        { return a.cmd.Name }
func (a *slashAdapter) Aliases() []string   { return a.aliases }
func (a *slashAdapter) Category() string    { return a.cat }
func (a *slashAdapter) Description() string { return a.cmd.Description }
func (a *slashAdapter) IsSlashOnly() bool   { return true }

func (a *slashAdapter) Usage() string {
	if subs := a.Subcommands(); len(subs) > 0 {
		names := make([]string, len(subs))
		for i, s := range subs {
			names[i] = s.Name
		}
		return strings.Join(names, "|")
	}
	return buildOptionUsage(a.cmd.Options)
}

func (a *slashAdapter) Example() string {
	if subs := a.Subcommands(); len(subs) > 0 {
		return subs[0].Name
	}
	return ""
}

func (a *slashAdapter) Subcommands() []Subcommand {
	var subs []Subcommand
	for _, opt := range a.cmd.Options {
		if opt.Type == discordgo.ApplicationCommandOptionSubCommand {
			subs = append(subs, Subcommand{
				Name:        opt.Name,
				Description: opt.Description,
				Usage:       buildOptionUsage(opt.Options),
			})
		}
	}
	return subs
}

func (a *slashAdapter) Execute(ctx *bot.Context) error {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Use the slash command `/%s` to interact with this feature.\n\n", a.cmd.Name))

	for _, sub := range a.Subcommands() {
		desc := sub.Description
		if desc == "" {
			desc = "No description available"
		}
		sb.WriteString(fmt.Sprintf("> `/%s %s` - %s\n", a.cmd.Name, sub.Name, desc))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("/%s (Slash Command)", a.cmd.Name),
		Description: sb.String(),
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func buildOptionUsage(opts []*discordgo.ApplicationCommandOption) string {
	var parts []string
	for _, o := range opts {
		if o.Type == discordgo.ApplicationCommandOptionSubCommand {
			continue
		}
		if o.Required {
			parts = append(parts, fmt.Sprintf("<%s>", o.Name))
		} else {
			parts = append(parts, fmt.Sprintf("[%s]", o.Name))
		}
	}
	return strings.Join(parts, " ")
}
