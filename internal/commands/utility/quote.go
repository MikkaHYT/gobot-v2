package utility

import (
	"bytes"
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/graphics"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type QuoteCmd struct{}

func (c *QuoteCmd) Name() string      { return "quote" }
func (c *QuoteCmd) Aliases() []string { return []string{"q", "quotemsg"} }
func (c *QuoteCmd) Category() string  { return "Media & Fun" }
func (c *QuoteCmd) Description() string {
	return "Creates an image quote card of a message."
}
func (c *QuoteCmd) Usage() string   { return "[reply | msg_id | msg_link | text] [color] [image_url]" }
func (c *QuoteCmd) Example() string { return "123456789012345678" }

func (c *QuoteCmd) Execute(ctx *bot.Context) error {
	var targetMsg *discordgo.Message
	var quoteText string
	var author *discordgo.User
	var customImgURL string
	var customColor color.Color

	if len(ctx.Message.Attachments) > 0 {
		for _, att := range ctx.Message.Attachments {
			if att.Width > 0 || strings.HasPrefix(att.ContentType, "image/") {
				customImgURL = att.URL
				break
			}
		}
	}

	var cleanArgs []string
	for _, arg := range ctx.Args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}

		if col, ok := parseColorArg(trimmed); ok {
			customColor = col
			continue
		}

		if (strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://")) && !strings.Contains(trimmed, "discord.com/channels/") {
			customImgURL = trimmed
			continue
		}

		cleanArgs = append(cleanArgs, trimmed)
	}

	if ctx.Message.ReferencedMessage != nil {
		targetMsg = ctx.Message.ReferencedMessage
	}

	if targetMsg == nil && len(cleanArgs) > 0 {
		argStr := strings.Join(cleanArgs, " ")

		if msg, err := ctx.FetchMessageFromInput(argStr); err == nil && msg != nil {
			targetMsg = msg
		} else if cleanID := strings.TrimSpace(argStr); isNumeric(cleanID) && len(cleanID) >= 17 {
			msg, err := ctx.Session.ChannelMessage(ctx.Message.ChannelID, cleanID)
			if err == nil && msg != nil {
				targetMsg = msg
			}
		}
	}

	if targetMsg != nil {
		quoteText = targetMsg.Content
		if quoteText == "" && len(targetMsg.Embeds) > 0 {
			for _, emb := range targetMsg.Embeds {
				if emb.Description != "" {
					quoteText = emb.Description
					break
				} else if emb.Title != "" {
					quoteText = emb.Title
					break
				}
			}
		}
		author = targetMsg.Author

		if customImgURL == "" && len(targetMsg.Attachments) > 0 {
			for _, att := range targetMsg.Attachments {
				if att.Width > 0 || strings.HasPrefix(att.ContentType, "image/") {
					customImgURL = att.URL
					break
				}
			}
		}
	} else if len(cleanArgs) > 0 {
		quoteText = strings.Join(cleanArgs, " ")
		author = ctx.Message.Author
	} else {
		msgs, err := ctx.Session.ChannelMessages(ctx.Message.ChannelID, 10, ctx.Message.ID, "", "")
		if err == nil {
			for _, m := range msgs {
				if m.ID != ctx.Message.ID && (m.Content != "" || len(m.Embeds) > 0) {
					targetMsg = m
					quoteText = m.Content
					author = m.Author
					break
				}
			}
		}
	}

	if quoteText == "" || author == nil {
		return fmt.Errorf("could not find a message to quote. Reply to a message, provide a message ID/link, or text")
	}

	avatarURL := helpers.UserAvatar(author)

	imgURLToFetch := avatarURL
	if customImgURL != "" {
		imgURLToFetch = customImgURL
	}

	gen := graphics.NewQuoteGenerator()
	avatarBytes, err := gen.FetchBytes(imgURLToFetch)
	if err != nil && imgURLToFetch != avatarURL {
		avatarBytes, _ = gen.FetchBytes(avatarURL)
	}

	var guildIconBytes []byte
	var guildName string
	if ctx.Message.GuildID != "" {
		if guild, _ := ctx.Guild(); guild != nil {
			guildName = guild.Name
			if iconURL := guild.IconURL("256"); iconURL != "" {
				guildIconBytes, _ = gen.FetchBytes(iconURL)
			}
		}
	}

	displayName := author.DisplayName()
	if member, errM := ctx.GetMember(author.ID); errM == nil && member != nil && member.Nick != "" {
		displayName = member.Nick
	}

	opts := graphics.QuoteCardOptions{
		AvatarBytes:       avatarBytes,
		QuoteText:         quoteText,
		AuthorDisplayName: displayName,
		AuthorUsername:    author.Username,
		GuildName:         guildName,
		GuildIconBytes:    guildIconBytes,
		AccentColor:       customColor,
	}

	cardBytes, err := gen.CreateQuoteCard(opts)
	if err != nil {
		return fmt.Errorf("failed to render quote card: %w", err)
	}

	_, err = ctx.SendFile("quote.png", bytes.NewReader(cardBytes))
	return err
}

func parseColorArg(arg string) (color.Color, bool) {
	hexVal := helpers.ParseHexColor(arg)
	if hexVal == 0 {
		return nil, false
	}
	r := uint8((hexVal >> 16) & 0xFF)
	g := uint8((hexVal >> 8) & 0xFF)
	b := uint8(hexVal & 0xFF)
	return color.RGBA{R: r, G: g, B: b, A: 255}, true
}

func isNumeric(s string) bool {
	_, err := strconv.ParseInt(s, 10, 64)
	return err == nil
}
