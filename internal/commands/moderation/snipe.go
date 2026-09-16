package moderation

import (
	"fmt"
	"strconv"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/listeners"
	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

type SnipeCmd struct{}

func (c *SnipeCmd) Name() string        { return "snipe" }
func (c *SnipeCmd) Aliases() []string   { return []string{"s"} }
func (c *SnipeCmd) Category() string    { return "Utility" }
func (c *SnipeCmd) Description() string { return "Retrieves recently deleted messages in the channel." }
func (c *SnipeCmd) Usage() string       { return "[index]" }
func (c *SnipeCmd) Example() string     { return "2" }

func (c *SnipeCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	index := 1
	if len(ctx.Args) > 0 {
		if idx, err := strconv.Atoi(ctx.Args[0]); err == nil && idx > 0 {
			index = idx
		}
	}

	entry, total, ok := listeners.GlobalSnipeCache.GetDeleted(ctx.Message.ChannelID, index)
	if !ok || total == 0 {
		if total > 0 {
			return ctx.SendError(fmt.Sprintf("Invalid snipe index. Only **%d** deleted message(s) available.", total))
		}
		return ctx.SendError("There are no deleted messages to snipe in this channel.")
	}

	timeAgo := helpers.FormatDuration(time.Since(entry.Timestamp))

	embed := &discordgo.MessageEmbed{
		Description: logger.SanitizeLogString(entry.Content),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    entry.AuthorName,
			IconURL: entry.AuthorAvatar,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Deleted %s ago (%s) | %d/%d", timeAgo, entry.Timestamp.Format("3:04 PM"), index, total),
		},
	}

	if len(entry.AttachmentURLs) > 0 {
		embed.Image = &discordgo.MessageEmbedImage{
			URL: entry.AttachmentURLs[0],
		}
	}

	_, err := ctx.ReplyEmbed(embed)
	return err
}

type EditsnipeCmd struct{}

func (c *EditsnipeCmd) Name() string      { return "editsnipe" }
func (c *EditsnipeCmd) Aliases() []string { return []string{"es"} }
func (c *EditsnipeCmd) Category() string  { return "Utility" }
func (c *EditsnipeCmd) Description() string {
	return "Retrieves recently edited messages in the channel."
}
func (c *EditsnipeCmd) Usage() string   { return "[index]" }
func (c *EditsnipeCmd) Example() string { return "1" }

func (c *EditsnipeCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	index := 1
	if len(ctx.Args) > 0 {
		if idx, err := strconv.Atoi(ctx.Args[0]); err == nil && idx > 0 {
			index = idx
		}
	}

	entry, total, ok := listeners.GlobalSnipeCache.GetEdited(ctx.Message.ChannelID, index)
	if !ok || total == 0 {
		if total > 0 {
			return ctx.SendError(fmt.Sprintf("Invalid editsnipe index. Only **%d** edited message(s) available.", total))
		}
		return ctx.SendError("There are no edited messages to snipe in this channel.")
	}

	embed := &discordgo.MessageEmbed{
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Before",
				Value:  logger.SanitizeLogString(entry.OriginalContent),
				Inline: false,
			},
			{
				Name:   "After",
				Value:  logger.SanitizeLogString(entry.EditedContent),
				Inline: false,
			},
		},
		Author: &discordgo.MessageEmbedAuthor{
			Name:    entry.AuthorName,
			IconURL: entry.AuthorAvatar,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("%d/%d • %s", index, total, entry.Timestamp.Format("01/02/2006 at 3:04 PM")),
		},
	}

	_, err := ctx.ReplyEmbed(embed)
	return err
}

type ReactionsnipeCmd struct{}

func (c *ReactionsnipeCmd) Name() string      { return "reactionsnipe" }
func (c *ReactionsnipeCmd) Aliases() []string { return []string{"rs", "rsnipe", "reactionsnip"} }
func (c *ReactionsnipeCmd) Category() string  { return "Utility" }
func (c *ReactionsnipeCmd) Description() string {
	return "Retrieves recently removed reactions in the channel."
}
func (c *ReactionsnipeCmd) Usage() string   { return "[index]" }
func (c *ReactionsnipeCmd) Example() string { return "1" }

func (c *ReactionsnipeCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	index := 1
	if len(ctx.Args) > 0 {
		if idx, err := strconv.Atoi(ctx.Args[0]); err == nil && idx > 0 {
			index = idx
		}
	}

	entry, total, ok := listeners.GlobalSnipeCache.GetReaction(ctx.Message.ChannelID, index)
	if !ok || total == 0 {
		if total > 0 {
			return ctx.SendError(fmt.Sprintf("Invalid reaction snipe index. Only **%d** removed reaction(s) available.", total))
		}
		return ctx.SendError("No recently removed reactions found in this channel.")
	}

	timeAgo := helpers.FormatDuration(time.Since(entry.Timestamp))
	targetChannelID := entry.ChannelID
	if targetChannelID == "" {
		targetChannelID = ctx.Message.ChannelID
	}
	msgURL := helpers.MessageURL(ctx.Message.GuildID, targetChannelID, entry.MessageID)

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Removed reaction %s on [Message](%s)", entry.EmojiName, msgURL),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    entry.AuthorName,
			IconURL: entry.AuthorAvatar,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Removed %s ago • %d/%d", timeAgo, index, total),
		},
	}

	if entry.EmojiURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: entry.EmojiURL,
		}
	}

	_, err := ctx.ReplyEmbed(embed)
	return err
}

type ClearsnipesCmd struct{}

func (c *ClearsnipesCmd) Name() string        { return "clearsnipes" }
func (c *ClearsnipesCmd) Aliases() []string   { return []string{"cs", "clearsnipe", "csnipe"} }
func (c *ClearsnipesCmd) Category() string    { return "Moderation" }
func (c *ClearsnipesCmd) Description() string { return "Clears all stored snipes for this channel." }
func (c *ClearsnipesCmd) Usage() string       { return "" }
func (c *ClearsnipesCmd) Example() string     { return "" }
func (c *ClearsnipesCmd) Permissions() int64  { return discordgo.PermissionManageMessages }

func (c *ClearsnipesCmd) Execute(ctx *bot.Context) error {
	listeners.GlobalSnipeCache.Clear(ctx.Message.ChannelID)
	return ctx.SendSuccess("Successfully cleared all snipes for this channel.")
}

type SnipesettingsCmd struct{}

func (c *SnipesettingsCmd) Name() string        { return "snipesettings" }
func (c *SnipesettingsCmd) Aliases() []string   { return []string{"snipelimit"} }
func (c *SnipesettingsCmd) Category() string    { return "Moderation" }
func (c *SnipesettingsCmd) Description() string { return "Configures server snipe history limits." }
func (c *SnipesettingsCmd) Usage() string       { return "[limit: 1-50]" }
func (c *SnipesettingsCmd) Example() string     { return "25" }
func (c *SnipesettingsCmd) Permissions() int64  { return discordgo.PermissionAdministrator }

func (c *SnipesettingsCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if len(ctx.Args) == 0 {
		lim := listeners.GlobalSnipeCache.GetLimit(ctx.Message.GuildID)
		return ctx.SendSuccess("Current snipe history limit for this server is **%d** messages.", lim)
	}

	maxLimit := 50
	if ctx.Config != nil && ctx.Config.MaxSnipeLimit > 0 {
		maxLimit = ctx.Config.MaxSnipeLimit
	}

	limit, err := strconv.Atoi(ctx.Args[0])
	if err != nil || limit < 1 || limit > maxLimit {
		return ctx.SendError(fmt.Sprintf("Please specify a valid snipe limit between **1** and **%d**.", maxLimit))
	}

	if errDB := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingSnipeLimit, limit); errDB != nil {
		return errDB
	}
	listeners.GlobalSnipeCache.SetLimit(ctx.Message.GuildID, limit)

	return ctx.SendSuccess("Set snipe history limit to **%d** messages.", limit)
}
