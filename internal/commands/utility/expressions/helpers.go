package expressions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"regexp"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

var customEmojiRegex = regexp.MustCompile(`<(?P<animated>a)?:(?P<name>[a-zA-Z0-9_]+):(?P<id>[0-9]+)>`)

func getReferencedMessage(ctx *bot.Context) *discordgo.Message {
	if ctx.Message.ReferencedMessage != nil {
		return ctx.Message.ReferencedMessage
	}
	if ctx.Message.MessageReference != nil && ctx.Message.MessageReference.MessageID != "" {
		chID := ctx.Message.MessageReference.ChannelID
		if chID == "" {
			chID = ctx.Message.ChannelID
		}
		if fetched, err := ctx.Session.ChannelMessage(chID, ctx.Message.MessageReference.MessageID); err == nil {
			return fetched
		}
	}
	return nil
}

func getStickerURL(item *discordgo.StickerItem) string {
	if item == nil {
		return ""
	}
	if item.FormatType == discordgo.StickerFormatTypeGIF {
		return fmt.Sprintf("https://cdn.discordapp.com/stickers/%s.gif", item.ID)
	} else if item.FormatType == discordgo.StickerFormatTypeLottie {
		return fmt.Sprintf("https://cdn.discordapp.com/stickers/%s.json", item.ID)
	}
	return fmt.Sprintf("https://cdn.discordapp.com/stickers/%s.png", item.ID)
}

type CreateStickerParams struct {
	GuildID     string
	Name        string
	Description string
	Tags        string
	FileBytes   []byte
	ContentType string
}

func CreateGuildSticker(s *discordgo.Session, params CreateStickerParams) (*discordgo.Sticker, error) {
	endpoint := fmt.Sprintf("%sguilds/%s/stickers", discordgo.EndpointAPI, params.GuildID)

	name := strings.TrimSpace(params.Name)
	if len([]rune(name)) < 2 {
		name = name + "_sticker"
	}
	name = helpers.TruncateString(name, 30)

	tags := strings.TrimSpace(params.Tags)
	if len([]rune(tags)) < 2 {
		tags = "sticker"
	}
	tags = helpers.TruncateString(tags, 200)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("name", name); err != nil {
		return nil, err
	}
	if len(params.Description) >= 2 && len(params.Description) <= 100 {
		if err := writer.WriteField("description", params.Description); err != nil {
			return nil, err
		}
	}
	if err := writer.WriteField("tags", tags); err != nil {
		return nil, err
	}

	filename := "sticker.png"
	if strings.Contains(params.ContentType, "gif") {
		filename = "sticker.gif"
	} else if strings.Contains(params.ContentType, "json") {
		filename = "sticker.json"
	}

	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	if params.ContentType != "" {
		partHeader.Set("Content-Type", params.ContentType)
	} else {
		partHeader.Set("Content-Type", "image/png")
	}

	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(params.FileBytes); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	respBody, err := s.RequestRaw("POST", endpoint, writer.FormDataContentType(), body.Bytes(), endpoint, 0)
	if err != nil {
		return nil, err
	}

	var sticker discordgo.Sticker
	if err := json.Unmarshal(respBody, &sticker); err != nil {
		return nil, err
	}
	return &sticker, nil
}

func GetGuildStickers(s *discordgo.Session, guildID string) ([]*discordgo.Sticker, error) {
	endpoint := fmt.Sprintf("%sguilds/%s/stickers", discordgo.EndpointAPI, guildID)
	respBody, err := s.RequestWithBucketID("GET", endpoint, nil, endpoint)
	if err != nil {
		return nil, err
	}

	var stickers []*discordgo.Sticker
	if err := json.Unmarshal(respBody, &stickers); err != nil {
		return nil, err
	}
	return stickers, nil
}

func EditGuildSticker(s *discordgo.Session, guildID, stickerID, name, description, tags string) (*discordgo.Sticker, error) {
	endpoint := fmt.Sprintf("%sguilds/%s/stickers/%s", discordgo.EndpointAPI, guildID, stickerID)
	payload := map[string]string{}
	if name != "" {
		payload["name"] = name
	}
	if description != "" {
		payload["description"] = description
	}
	if tags != "" {
		payload["tags"] = tags
	}

	respBody, err := s.RequestWithBucketID("PATCH", endpoint, payload, endpoint)
	if err != nil {
		return nil, err
	}

	var sticker discordgo.Sticker
	if err := json.Unmarshal(respBody, &sticker); err != nil {
		return nil, err
	}
	return &sticker, nil
}

func DeleteGuildSticker(s *discordgo.Session, guildID, stickerID string) error {
	endpoint := fmt.Sprintf("%sguilds/%s/stickers/%s", discordgo.EndpointAPI, guildID, stickerID)
	_, err := s.RequestWithBucketID("DELETE", endpoint, nil, endpoint)
	return err
}

func parseEmoji(arg string) (id string, name string, animated bool, url string, err error) {
	match := customEmojiRegex.FindStringSubmatch(arg)
	if len(match) == 0 {
		return "", "", false, "", fmt.Errorf("invalid emoji syntax")
	}

	animated = match[1] == "a"
	name = match[2]
	id = match[3]

	ext := "png"
	if animated {
		ext = "gif"
	}

	url = fmt.Sprintf("https://cdn.discordapp.com/emojis/%s.%s?size=1024", id, ext)
	return id, name, animated, url, nil
}

func resolveGuildEmoji(ctx *bot.Context, arg string) (*discordgo.Emoji, error) {
	emojis, err := ctx.Session.GuildEmojis(ctx.Message.GuildID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch emojis")
	}

	id, name, _, _, parseErr := parseEmoji(arg)
	cleanArg := strings.Trim(arg, ":")

	for _, e := range emojis {
		if parseErr == nil && e.ID == id {
			return e, nil
		}
		if e.ID == arg || strings.EqualFold(e.Name, cleanArg) || strings.EqualFold(e.Name, name) {
			return e, nil
		}
	}

	return nil, fmt.Errorf("emoji `%s` not found in this server", arg)
}

func resolveGuildSticker(ctx *bot.Context, arg string) (*discordgo.Sticker, error) {
	stickers, err := GetGuildStickers(ctx.Session, ctx.Message.GuildID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stickers")
	}

	for _, s := range stickers {
		if s.ID == arg || strings.EqualFold(s.Name, arg) {
			return s, nil
		}
	}

	return nil, fmt.Errorf("sticker `%s` not found in this server", arg)
}
