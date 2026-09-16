package expressions

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

func (c *StickerCmd) handleAdd(ctx *bot.Context) error {
	subArgs := ctx.SubArgs()
	if len(subArgs) == 0 || (len(subArgs) < 2 && len(ctx.Message.Attachments) == 0) {
		return fmt.Errorf("usage: `%ssticker add <name> <URL|attachment> [tags]`", ctx.Prefix)
	}

	name := subArgs[0]
	var imageURL string
	var tags string

	if len(ctx.Message.Attachments) > 0 {
		imageURL = ctx.Message.Attachments[0].URL
		if len(subArgs) >= 2 {
			tags = strings.Join(subArgs[1:], ",")
		} else {
			tags = name
		}
	} else if len(subArgs) >= 2 {
		imageURL = subArgs[1]
		if len(subArgs) >= 3 {
			tags = strings.Join(subArgs[2:], ",")
		} else {
			tags = name
		}
	}

	imgData, contentType, err := helpers.FetchImageData(imageURL)
	if err != nil {
		return fmt.Errorf("failed to download image: %w", err)
	}

	sticker, err := CreateGuildSticker(ctx.Session, CreateStickerParams{
		GuildID:     ctx.Message.GuildID,
		Name:        name,
		Description: "Added via " + ctx.Session.State.User.Username,
		Tags:        tags,
		FileBytes:   imgData,
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("failed to create sticker: %w", err)
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Created new sticker **%s** (`%s`)", sticker.Name, sticker.ID),
	})
	return err
}

func (c *StickerCmd) handleRemove(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "sticker remove <name|ID>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	sticker, err := resolveGuildSticker(ctx, strings.Join(subArgs, " "))
	if err != nil {
		return err
	}

	confirmed, err := ctx.PromptConfirmation(fmt.Sprintf("Are you sure you want to remove sticker **%s**?", sticker.Name))
	if err != nil || !confirmed {
		return err
	}

	err = DeleteGuildSticker(ctx.Session, ctx.Message.GuildID, sticker.ID)
	if err != nil {
		return fmt.Errorf("failed to delete sticker: %w", err)
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Deleted sticker **%s**.", sticker.Name),
	})
	return err
}

func (c *StickerCmd) handleRename(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "sticker rename <name|ID> <new_name>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	newName := subArgs[len(subArgs)-1]
	stickerArg := strings.Join(subArgs[:len(subArgs)-1], " ")

	sticker, err := resolveGuildSticker(ctx, stickerArg)
	if err != nil {
		return err
	}

	updated, err := EditGuildSticker(ctx.Session, ctx.Message.GuildID, sticker.ID, newName, sticker.Description, sticker.Tags)
	if err != nil {
		return fmt.Errorf("failed to rename sticker: %w", err)
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Renamed sticker **%s** > **%s**.", sticker.Name, updated.Name),
	})
	return err
}

func (c *StickerCmd) handleSteal(ctx *bot.Context) error {
	refMsg := getReferencedMessage(ctx)
	var targetSticker *discordgo.StickerItem

	if len(ctx.Message.StickerItems) > 0 {
		targetSticker = ctx.Message.StickerItems[0]
	} else if refMsg != nil && len(refMsg.StickerItems) > 0 {
		targetSticker = refMsg.StickerItems[0]
	}

	if targetSticker == nil {
		return fmt.Errorf("no sticker found. Reply to a message with a sticker or send a sticker with this command")
	}

	subArgs := ctx.SubArgs()
	stickerName := targetSticker.Name
	if len(subArgs) > 0 {
		stickerName = subArgs[0]
	}

	stickerURL := getStickerURL(targetSticker)
	imgData, contentType, err := helpers.FetchImageData(stickerURL)
	if err != nil {
		return fmt.Errorf("failed to download sticker asset: %w", err)
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
		return fmt.Errorf("failed to save sticker: %w", err)
	}

	_ = ctx.ReactSuccess()
	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Successfully stole sticker! Added as **%s** (`%s`).", newSticker.Name, newSticker.ID),
	})
	return err
}

func (c *StickerCmd) handleEnlarge(ctx *bot.Context) error {
	refMsg := getReferencedMessage(ctx)
	var stickerID string
	var stickerName string

	if len(ctx.Message.StickerItems) > 0 {
		stickerID = ctx.Message.StickerItems[0].ID
		stickerName = ctx.Message.StickerItems[0].Name
	} else if refMsg != nil && len(refMsg.StickerItems) > 0 {
		stickerID = refMsg.StickerItems[0].ID
		stickerName = refMsg.StickerItems[0].Name
	} else if len(ctx.SubArgs()) > 0 {
		st, err := resolveGuildSticker(ctx, strings.Join(ctx.SubArgs(), " "))
		if err == nil {
			stickerID = st.ID
			stickerName = st.Name
		}
	}

	if stickerID == "" {
		return fmt.Errorf("please specify a sticker name/ID or reply to a sticker message")
	}

	stickerURL := fmt.Sprintf("https://cdn.discordapp.com/stickers/%s.png", stickerID)
	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Title:       "Sticker: " + stickerName,
		Description: fmt.Sprintf("[Download Sticker Image](%s)", stickerURL),
		Image:       &discordgo.MessageEmbedImage{URL: stickerURL},
	})
	return err
}

func (c *StickerCmd) handleList(ctx *bot.Context) error {
	stickers, err := GetGuildStickers(ctx.Session, ctx.Message.GuildID)
	if err != nil {
		return fmt.Errorf("failed to fetch stickers: %w", err)
	}

	if len(stickers) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: "This server has no custom stickers.",
		})
		return err
	}

	var stickerEntries []string
	for _, s := range stickers {
		stickerEntries = append(stickerEntries, fmt.Sprintf("• **%s** (`%s`) - Tags: `%s`", s.Name, s.ID, s.Tags))
	}

	const pageSize = 15
	var embeds []*discordgo.MessageEmbed
	total := len(stickerEntries)

	for i := 0; i < total; i += pageSize {
		end := i + pageSize
		if end > total {
			end = total
		}

		embeds = append(embeds, &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Server Stickers (%s total)", helpers.FormatNumber(total)),
			Description: strings.Join(stickerEntries[i:end], "\n"),
		})
	}

	return ctx.SendPaginatedEmbeds(embeds)
}
