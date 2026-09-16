package expressions

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

func (c *EmojiCmd) handleAdd(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "emoji add <name> <URL|attachment>") {
		return nil
	}

	name := ctx.SubArgs()[0]
	imageURL := ctx.ExtractImageURL()
	if imageURL == "" {
		return fmt.Errorf("please provide an image URL or attach an image")
	}

	base64Data, err := helpers.FetchImageAsBase64(imageURL)
	if err != nil {
		return fmt.Errorf("failed to fetch image: %w", err)
	}

	emoji, err := ctx.Session.GuildEmojiCreate(ctx.Message.GuildID, &discordgo.EmojiParams{
		Name:  name,
		Image: base64Data,
	})
	if err != nil {
		return fmt.Errorf("failed to create emoji: %w", err)
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Added new emoji: %s (`:%s:`)", emoji.MessageFormat(), emoji.Name),
	})
	return err
}

func (c *EmojiCmd) handleRemove(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "emoji remove <emoji>") {
		return nil
	}

	targetEmoji, err := resolveGuildEmoji(ctx, ctx.SubArgs()[0])
	if err != nil {
		return err
	}

	confirmed, err := ctx.PromptConfirmation(fmt.Sprintf("Are you sure you want to remove emoji **:%s:**?", targetEmoji.Name))
	if err != nil || !confirmed {
		return err
	}

	err = ctx.Session.GuildEmojiDelete(ctx.Message.GuildID, targetEmoji.ID)
	if err != nil {
		return fmt.Errorf("failed to delete emoji: %w", err)
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Successfully removed emoji **:%s:**", targetEmoji.Name),
	})
	return err
}

func (c *EmojiCmd) handleRename(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "emoji rename <emoji> <new_name>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	targetEmoji, err := resolveGuildEmoji(ctx, subArgs[0])
	if err != nil {
		return err
	}

	newName := subArgs[1]
	oldName := targetEmoji.Name

	updatedEmoji, err := ctx.Session.GuildEmojiEdit(ctx.Message.GuildID, targetEmoji.ID, &discordgo.EmojiParams{
		Name: newName,
	})
	if err != nil {
		return fmt.Errorf("failed to rename emoji: %w", err)
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Renamed emoji %s from **:%s:** to **:%s:**", updatedEmoji.MessageFormat(), oldName, updatedEmoji.Name),
	})
	return err
}

func (c *EmojiCmd) handleSteal(ctx *bot.Context) error {
	subArgs := ctx.SubArgs()
	refMsg := getReferencedMessage(ctx)

	var emojiArg string
	var newName string

	if len(subArgs) > 0 {
		if _, _, _, _, err := parseEmoji(subArgs[0]); err == nil {
			emojiArg = subArgs[0]
			if len(subArgs) >= 2 {
				newName = subArgs[1]
			}
		} else if refMsg != nil {
			matches := customEmojiRegex.FindAllString(refMsg.Content, -1)
			if len(matches) > 0 {
				emojiArg = matches[0]
				newName = subArgs[0]
			}
		}
	} else if refMsg != nil {
		matches := customEmojiRegex.FindAllString(refMsg.Content, -1)
		if len(matches) > 0 {
			emojiArg = matches[0]
		}
	}

	if emojiArg == "" {
		return fmt.Errorf("usage: `%semoji steal <custom_emoji> [new_name]` or reply to a message with a custom emoji", ctx.Prefix)
	}

	emojiID, defaultName, _, emojiURL, err := parseEmoji(emojiArg)
	if err != nil {
		return fmt.Errorf("invalid custom emoji provided. Make sure it is a custom emoji from another server")
	}

	if newName == "" {
		newName = defaultName
	}

	base64Data, err := helpers.FetchImageAsBase64(emojiURL)
	if err != nil {
		return fmt.Errorf("failed to download emoji image: %w", err)
	}

	newEmoji, err := ctx.Session.GuildEmojiCreate(ctx.Message.GuildID, &discordgo.EmojiParams{
		Name:  newName,
		Image: base64Data,
	})
	if err != nil {
		return fmt.Errorf("failed to add emoji to server: %w", err)
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Stole emoji! Added as %s (`:%s:`) [ID: `%s`]", newEmoji.MessageFormat(), newEmoji.Name, emojiID),
	})
	return err
}

func (c *EmojiCmd) handleEnlarge(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "emoji enlarge <emoji>") {
		return nil
	}

	_, emojiName, isAnimated, emojiURL, err := parseEmoji(ctx.SubArgs()[0])
	if err != nil {
		return fmt.Errorf("could not parse a valid custom emoji")
	}

	format := "PNG"
	if isAnimated {
		format = "GIF"
	}

	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Emoji: :%s:", emojiName),
		Description: fmt.Sprintf("[Download Original %s](%s)", format, emojiURL),
		Image:       &discordgo.MessageEmbedImage{URL: emojiURL},
	})
	return err
}

func (c *EmojiCmd) handleList(ctx *bot.Context) error {
	emojis, err := ctx.Session.GuildEmojis(ctx.Message.GuildID)
	if err != nil {
		return fmt.Errorf("failed to fetch server emojis: %w", err)
	}

	if len(emojis) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: "This server has no custom emojis.",
		})
		return err
	}

	var emojiEntries []string
	for _, e := range emojis {
		emojiEntries = append(emojiEntries, fmt.Sprintf("%s `:%s:` (`%s`)", e.MessageFormat(), e.Name, e.ID))
	}

	const pageSize = 15
	var embeds []*discordgo.MessageEmbed
	total := len(emojiEntries)

	for i := 0; i < total; i += pageSize {
		end := i + pageSize
		if end > total {
			end = total
		}

		embeds = append(embeds, &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Server Emojis (%s total)", helpers.FormatNumber(total)),
			Description: strings.Join(emojiEntries[i:end], "\n"),
		})
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

func (c *EmojiCmd) handleInfo(ctx *bot.Context) error {
	target := ""
	if len(ctx.SubArgs()) > 0 {
		target = ctx.SubArgs()[0]
	} else if len(ctx.Args) > 0 {
		target = ctx.Args[0]
	}
	if target == "" {
		return fmt.Errorf("usage: `%semoji info <emoji>`", ctx.Prefix)
	}

	emojiID, emojiName, isAnimated, emojiURL, err := parseEmoji(target)
	if err != nil {
		targetEmoji, errRes := resolveGuildEmoji(ctx, target)
		if errRes != nil {
			return fmt.Errorf("emoji not found")
		}
		emojiID = targetEmoji.ID
		emojiName = targetEmoji.Name
		isAnimated = targetEmoji.Animated
		emojiURL = fmt.Sprintf("https://cdn.discordapp.com/emojis/%s.png?size=1024", emojiID)
		if isAnimated {
			emojiURL = fmt.Sprintf("https://cdn.discordapp.com/emojis/%s.gif?size=1024", emojiID)
		}
	}

	createdTime, _ := discordgo.SnowflakeTimestamp(emojiID)

	embed := &discordgo.MessageEmbed{
		Title: emojiName,
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: emojiURL,
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Name", Value: fmt.Sprintf("`:%s:`", emojiName), Inline: true},
			{Name: "ID", Value: fmt.Sprintf("`%s`", emojiID), Inline: true},
			{Name: "Animated", Value: fmt.Sprintf("`%t`", isAnimated), Inline: true},
			{Name: "Created At", Value: fmt.Sprintf("<t:%d:F>", createdTime.Unix()), Inline: false},
			{Name: "Direct Link", Value: fmt.Sprintf("[Click Here](%s)", emojiURL), Inline: false},
		},
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}
