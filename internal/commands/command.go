package commands

import (
	"fmt"
	"sort"
	"strings"

	"gobot/internal/bot"

	"github.com/bwmarrin/discordgo"
)

type Command interface {
	Name() string
	Aliases() []string
	Category() string
	Description() string
	Usage() string
	Example() string
	Execute(ctx *bot.Context) error
}

type Registry struct {
	commands map[string]Command
	aliases  map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		commands: make(map[string]Command),
		aliases:  make(map[string]string),
	}
}

func (r *Registry) Register(cmd Command) error {
	if cmd == nil {
		return fmt.Errorf("cannot register nil command")
	}

	cmdName := strings.ToLower(strings.TrimSpace(cmd.Name()))
	if cmdName == "" {
		return fmt.Errorf("command name cannot be empty")
	}

	if existing, exists := r.commands[cmdName]; exists {
		return fmt.Errorf("duplicate command name %q (conflicts with %T)", cmdName, existing)
	}
	if existingTarget, exists := r.aliases[cmdName]; exists {
		return fmt.Errorf("command name %q conflicts with existing alias for %q", cmdName, existingTarget)
	}

	for _, alias := range cmd.Aliases() {
		cleanAlias := strings.ToLower(strings.TrimSpace(alias))
		if cleanAlias == "" {
			continue
		}
		if cleanAlias == cmdName {
			continue
		}
		if existing, exists := r.commands[cleanAlias]; exists {
			return fmt.Errorf("command alias %q for %q conflicts with primary command %q (%T)", cleanAlias, cmdName, existing.Name(), existing)
		}
		if existingTarget, exists := r.aliases[cleanAlias]; exists {
			return fmt.Errorf("command alias %q for %q conflicts with existing alias for %q", cleanAlias, cmdName, existingTarget)
		}
	}

	r.commands[cmdName] = cmd
	for _, alias := range cmd.Aliases() {
		cleanAlias := strings.ToLower(strings.TrimSpace(alias))
		if cleanAlias != "" && cleanAlias != cmdName {
			r.aliases[cleanAlias] = cmdName
		}
	}

	return nil
}

func (r *Registry) RegisterAll(groups ...[]Command) error {
	for _, group := range groups {
		for _, cmd := range group {
			if err := r.Register(cmd); err != nil {
				return err
			}
		}
	}
	return nil
}

type Subcommand struct {
	Name        string
	Description string
	Usage       string
	Example     string
}

type Subcommandable interface {
	Subcommands() []Subcommand
}

type PermissionChecker interface {
	Permissions() int64
}

type HelpFieldsProvider interface {
	HelpFields() []*discordgo.MessageEmbedField
}

func (r *Registry) AllCommands() []Command {
	cmds := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		cmds = append(cmds, cmd)
	}

	sort.Slice(cmds, func(i, j int) bool {
		return strings.ToLower(cmds[i].Name()) < strings.ToLower(cmds[j].Name())
	})

	return cmds
}

func (r *Registry) Get(name string) (Command, bool) {
	cleanName := strings.ToLower(strings.TrimSpace(name))
	if cmd, ok := r.commands[cleanName]; ok {
		return cmd, true
	}
	if targetName, ok := r.aliases[cleanName]; ok {
		if cmd, ok := r.commands[targetName]; ok {
			return cmd, true
		}
	}
	return nil, false
}
