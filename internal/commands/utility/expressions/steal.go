package expressions

import (
	"fmt"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

func (c *EnlargeCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	refMsg := getReferencedMessage(ctx)

	if len(ctx.Args) > 0 {
		arg := ctx.Args[0]
		if _, emojiName, isAnimated, emojiURL, err := parseEmoji(arg); err == nil {
			format := "PNG"
			if isAnimated {
				format = "GIF"
			}
			_, errReply := ctx.ReplyEmbed(&discordgo.MessageEmbed{
				Title:       fmt.Sprintf("Emoji: :%s:", emojiName),
				Description: fmt.Sprintf("[Download Original %s](%s)", format, emojiURL),
				Image:       &discordgo.MessageEmbedImage{URL: emojiURL},
			})
			return errReply
		}
	}

	var targetSticker *discordgo.StickerItem
	if len(ctx.Message.StickerItems) > 0 {
		targetSticker = ctx.Message.StickerItems[0]
	} else if refMsg != nil && len(refMsg.StickerItems) > 0 {
		targetSticker = refMsg.StickerItems[0]
	}

	if targetSticker != nil {
		stickerURL := getStickerURL(targetSticker)
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Title:       "Sticker: " + targetSticker.Name,
			Description: fmt.Sprintf("[Download Original Asset](%s)", stickerURL),
			Image:       &discordgo.MessageEmbedImage{URL: stickerURL},
		})
		return err
	}

	if refMsg != nil {
		matches := customEmojiRegex.FindAllString(refMsg.Content, -1)
		if len(matches) > 0 {
			emojiArg := matches[0]
			_, emojiName, isAnimated, emojiURL, err := parseEmoji(emojiArg)
			if err == nil {
				format := "PNG"
				if isAnimated {
					format = "GIF"
				}
				_, errReply := ctx.ReplyEmbed(&discordgo.MessageEmbed{
					Title:       fmt.Sprintf("Emoji: :%s:", emojiName),
					Description: fmt.Sprintf("[Download Original %s](%s)", format, emojiURL),
					Image:       &discordgo.MessageEmbedImage{URL: emojiURL},
				})
				return errReply
			}
		}
	}

	if refMsg != nil && len(refMsg.Attachments) > 0 {
		att := refMsg.Attachments[0]
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Title: "Attachment Image",
			Image: &discordgo.MessageEmbedImage{URL: att.URL},
		})
		return err
	}

	return ctx.SendError("Could not find a custom emoji or sticker to enlarge. Reply to a message or provide an emoji.")
}

func (c *StealCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	refMsg := getReferencedMessage(ctx)

	var targetSticker *discordgo.StickerItem
	if len(ctx.Message.StickerItems) > 0 {
		targetSticker = ctx.Message.StickerItems[0]
	} else if refMsg != nil && len(refMsg.StickerItems) > 0 {
		targetSticker = refMsg.StickerItems[0]
	}

	if targetSticker != nil {
		stickerName := targetSticker.Name
		if len(ctx.Args) > 0 {
			stickerName = ctx.Args[0]
		}
		if stickerName == "" {
			stickerName = "stolen_sticker"
		}

		stickerURL := getStickerURL(targetSticker)
		imgData, contentType, err := helpers.FetchImageData(stickerURL)
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to download sticker asset: %v", err))
		}

		newSticker, err := CreateGuildSticker(ctx.Session, CreateStickerParams{
			GuildID:     ctx.Message.GuildID,
			Name:        stickerName,
			Description: "Stolen sticker",
			Tags:        stickerName,
			FileBytes:   imgData,
			ContentType: contentType,
		})
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to save sticker: %v", err))
		}

		_ = ctx.ReactSuccess()
		_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("Successfully stole sticker! Added as **%s** (`%s`).", newSticker.Name, newSticker.ID),
		})
		return err
	}

	var emojiArg string
	var customName string

	if len(ctx.Args) > 0 {
		if _, _, _, _, err := parseEmoji(ctx.Args[0]); err == nil {
			emojiArg = ctx.Args[0]
			if len(ctx.Args) > 1 {
				customName = ctx.Args[1]
			}
		} else if refMsg != nil {
			matches := customEmojiRegex.FindAllString(refMsg.Content, -1)
			if len(matches) > 0 {
				emojiArg = matches[0]
				customName = ctx.Args[0]
			}
		}
	} else if refMsg != nil {
		matches := customEmojiRegex.FindAllString(refMsg.Content, -1)
		if len(matches) > 0 {
			emojiArg = matches[0]
		}
	}

	if emojiArg != "" {
		emojiID, defaultName, _, emojiURL, err := parseEmoji(emojiArg)
		if err != nil {
			return ctx.SendError("Invalid custom emoji provided.")
		}

		name := defaultName
		if customName != "" {
			name = customName
		}

		base64Data, err := helpers.FetchImageAsBase64(emojiURL)
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to download emoji image: %v", err))
		}

		newEmoji, err := ctx.Session.GuildEmojiCreate(ctx.Message.GuildID, &discordgo.EmojiParams{
			Name:  name,
			Image: base64Data,
		})
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to add emoji to server: %v", err))
		}

		_ = ctx.ReactSuccess()
		_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("Stole emoji! Added as %s (`:%s:`) [ID: `%s`]", newEmoji.MessageFormat(), newEmoji.Name, emojiID),
		})
		return err
	}

	if refMsg != nil && len(refMsg.Attachments) > 0 {
		att := refMsg.Attachments[0]
		name := "stolen_emoji"
		if len(ctx.Args) > 0 {
			name = ctx.Args[0]
		}

		base64Data, err := helpers.FetchImageAsBase64(att.URL)
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to download image attachment: %v", err))
		}

		newEmoji, err := ctx.Session.GuildEmojiCreate(ctx.Message.GuildID, &discordgo.EmojiParams{
			Name:  name,
			Image: base64Data,
		})
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to add emoji from attachment: %v", err))
		}

		_ = ctx.ReactSuccess()
		_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("Stole emoji from attachment! Added as %s (`:%s:`)", newEmoji.MessageFormat(), newEmoji.Name),
		})
		return err
	}

	return ctx.SendError(fmt.Sprintf("No sticker or custom emoji found to steal. Reply to a message with a sticker/emoji or provide an emoji syntax: `%ssteal <emoji> [name]`.", ctx.Prefix))
}
