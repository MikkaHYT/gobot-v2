package giveaway

import "gobot/internal/commands"

var Commands = []commands.Command{
	commands.FromSlash(SlashCommand, "Giveaway"),
}
