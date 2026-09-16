package helpers

import (
	"encoding/base64"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"strings"

	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

var (
	LogWarn  = func(format string, v ...any) { log.Printf("[WARN] "+format, v...) }
	LogError = func(format string, v ...any) { log.Printf("[ERROR] "+format, v...) }
)

func Spawn(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[PANIC] recovered: %v\n%s", r, debug.Stack())
			}
		}()
		fn()
	}()
}

func IsSnowflake(s string) bool {
	if len(s) < 17 || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func SplitQuoted(input string) []string {
	var tokens []string
	var cur strings.Builder
	var quote rune
	for _, r := range input {
		switch {
		case r == '\'' || r == '"':
			if quote == r {
				quote = 0
			} else if quote == 0 && cur.Len() == 0 {
				quote = r
			} else {
				cur.WriteRune(r)
			}
		case r == ' ' && quote == 0:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

func Ptr[T any](v T) *T { return &v }

func UserAvatar(u *discordgo.User) string {
	if u == nil {
		return "https://cdn.discordapp.com/embed/avatars/0.png"
	}
	if u.Avatar != "" {
		return u.AvatarURL("256")
	}
	if u.Discriminator == "0" || u.Discriminator == "" {
		if id, err := strconv.ParseUint(u.ID, 10, 64); err == nil {
			return fmt.Sprintf("https://cdn.discordapp.com/embed/avatars/%d.png", (id>>22)%6)
		}
	} else {
		if disc, err := strconv.Atoi(u.Discriminator); err == nil {
			return fmt.Sprintf("https://cdn.discordapp.com/embed/avatars/%d.png", disc%5)
		}
	}
	return "https://cdn.discordapp.com/embed/avatars/0.png"
}

func MemberAvatar(m *discordgo.Member) string {
	if m == nil {
		return "https://cdn.discordapp.com/embed/avatars/0.png"
	}
	if m.Avatar != "" {
		return m.AvatarURL("256")
	}
	return UserAvatar(m.User)
}

func FetchImageAsBase64(imageURL string) (string, error) {
	data, contentType, err := FetchImageData(imageURL)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", contentType, encoded), nil
}

func MessageURL(guildID, channelID, messageID string) string {
	if guildID == "" {
		return fmt.Sprintf("https://discord.com/channels/@me/%s/%s", channelID, messageID)
	}
	return fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
}

func EscapeMarkdown(text string) string {
	if text == "" {
		return ""
	}
	var sb strings.Builder
	sb.Grow(len(text) + 16)
	for _, r := range text {
		switch r {
		case '*', '_', '~', '`', '|', '[', ']', '(', ')', '>', '#', '\\':
			sb.WriteRune('\\')
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func EscapeMarkdownContent(text string) string {
	if text == "" {
		return ""
	}
	var sb strings.Builder
	sb.Grow(len(text) + 16)
	for _, r := range text {
		switch r {
		case '\n':
			sb.WriteRune('\n')
		case '*', '_', '~', '`', '|', '[', ']', '(', ')', '>', '<', '#', '\\':
			sb.WriteRune('\\')
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func RespondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	if s == nil || i == nil || i.Interaction == nil {
		return
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}
