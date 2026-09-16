package expressions

import (
	"gobot/internal/bot"
	"gobot/internal/commands"

	"github.com/bwmarrin/discordgo"
)

var Commands = []commands.Command{
	&EmojiCmd{},
	&StickerCmd{},
	&EnlargeCmd{},
	&StealCmd{},
}

type EmojiCmd struct{}

func (c *EmojiCmd) Name() string        { return "emoji" }
func (c *EmojiCmd) Aliases() []string   { return []string{"e", "emote", "emojis"} }
func (c *EmojiCmd) Category() string    { return "Media & Fun" }
func (c *EmojiCmd) Description() string { return "emoji management." }
func (c *EmojiCmd) Usage() string       { return "<subcommand> [args]" }
func (c *EmojiCmd) Example() string     { return "steal :pepe_dance:" }

func (c *EmojiCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "add", Description: "Add an emoji from a URL, attachment, or image", Usage: "<name> <URL|attachment>", Example: "cat https://example.com/cat.png"},
		{Name: "remove", Description: "Remove an emoji from the server", Usage: "<emoji>", Example: ":pepe_dead:"},
		{Name: "rename", Description: "Rename an existing server emoji", Usage: "<emoji> <new_name>", Example: ":pepe_sad: pepe_depressed"},
		{Name: "steal", Description: "Steal an emoji from another server and add it here", Usage: "<emoji> [new_name]", Example: ":sob: sob_my_name"},
		{Name: "enlarge", Description: "Get the image link of an emoji", Usage: "<emoji>", Example: ":pepe_vibe:"},
		{Name: "list", Description: "List all custom emojis in this server", Usage: "", Example: ""},
		{Name: "info", Description: "Display metadata for a custom emoji", Usage: "<emoji>", Example: ":pepe_vibe:"},
	}
}

func (c *EmojiCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	var err error
	switch ctx.Subcommand() {
	case "add", "create", "new":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuildExpressions) {
			return nil
		}
		err = c.handleAdd(ctx)
	case "remove", "rem", "delete", "del":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuildExpressions) {
			return nil
		}
		err = c.handleRemove(ctx)
	case "rename", "name":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuildExpressions) {
			return nil
		}
		err = c.handleRename(ctx)
	case "steal", "clone":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuildExpressions) {
			return nil
		}
		err = c.handleSteal(ctx)
	case "enlarge", "big", "jumbo":
		err = c.handleEnlarge(ctx)
	case "list", "all":
		err = c.handleList(ctx)
	case "info":
		err = c.handleInfo(ctx)
	default:
		if _, _, _, _, parseErr := parseEmoji(ctx.Args[0]); parseErr == nil {
			err = c.handleInfo(ctx)
		} else {
			_, _ = ctx.SendUsage(c)
			return nil
		}
	}

	if err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}

type StickerCmd struct{}

func (c *StickerCmd) Name() string        { return "sticker" }
func (c *StickerCmd) Aliases() []string   { return []string{"stickers", "st"} }
func (c *StickerCmd) Category() string    { return "Media & Fun" }
func (c *StickerCmd) Description() string { return "sticker management." }
func (c *StickerCmd) Usage() string       { return "<subcommand> [args]" }
func (c *StickerCmd) Example() string     { return "add CoolSticker https://example.com/sticker.png cool" }

func (c *StickerCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "add", Description: "Add a sticker to the server", Usage: "<name> <URL|attachment> <tags>", Example: "pepe_vibe https://site.com/s.png vibe"},
		{Name: "remove", Description: "Remove a sticker from the server", Usage: "<name|ID>", Example: "pepe_vibe"},
		{Name: "rename", Description: "Rename a server sticker", Usage: "<name|ID> <new_name>", Example: "pepe_vibe pepe_dance"},
		{Name: "steal", Description: "Steal a sticker attached to a message or reply", Usage: "[name]", Example: "stolen_sticker"},
		{Name: "enlarge", Description: "View the high-resolution image of a sticker", Usage: "<name|ID|reply>", Example: "pepe_vibe"},
		{Name: "list", Description: "List all custom stickers in this server", Usage: "", Example: ""},
	}
}

func (c *StickerCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	var err error
	switch ctx.Subcommand() {
	case "add", "create":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuildExpressions) {
			return nil
		}
		err = c.handleAdd(ctx)
	case "remove", "rem", "delete", "del":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuildExpressions) {
			return nil
		}
		err = c.handleRemove(ctx)
	case "rename", "name":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuildExpressions) {
			return nil
		}
		err = c.handleRename(ctx)
	case "steal":
		if !ctx.RequirePermissions(discordgo.PermissionManageGuildExpressions) {
			return nil
		}
		err = c.handleSteal(ctx)
	case "enlarge", "jumbo":
		err = c.handleEnlarge(ctx)
	case "list":
		err = c.handleList(ctx)
	default:
		_, _ = ctx.SendUsage(c)
		return nil
	}

	if err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}

type EnlargeCmd struct{}

func (c *EnlargeCmd) Name() string      { return "enlarge" }
func (c *EnlargeCmd) Aliases() []string { return []string{"jumbo", "big", "eview"} }
func (c *EnlargeCmd) Category() string  { return "Media & Fun" }
func (c *EnlargeCmd) Description() string {
	return "Converts a custom emoji or sticker to a high-res image link."
}
func (c *EnlargeCmd) Usage() string   { return "<emoji|sticker|reply>" }
func (c *EnlargeCmd) Example() string { return ":pepe_vibe:" }

type StealCmd struct{}

func (c *StealCmd) Name() string      { return "steal" }
func (c *StealCmd) Aliases() []string { return []string{"stealemoji", "stealsticker"} }
func (c *StealCmd) Category() string  { return "Media & Fun" }
func (c *StealCmd) Description() string {
	return "Steals a custom emoji or sticker and adds it to this server."
}
func (c *StealCmd) Usage() string      { return "<emoji|sticker|reply> [new_name]" }
func (c *StealCmd) Example() string    { return ":pepe_dance: custom_pepe" }
func (c *StealCmd) Permissions() int64 { return discordgo.PermissionManageGuildExpressions }
