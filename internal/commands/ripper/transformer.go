package ripper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

var (
	urlRegex = regexp.MustCompile(`https?://[^\s]+`)
)

func CleanTrackingParams(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	q := u.Query()
	trackingKeys := []string{"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content", "igsh", "s", "t", "si", "feature", "ref_src", "gclid", "fbclid"}
	for _, key := range trackingKeys {
		q.Del(key)
	}

	u.RawQuery = q.Encode()
	return u.String()
}

func ExtractURLFromMessage(m *discordgo.Message) string {
	if m == nil {
		return ""
	}
	if match := urlRegex.FindString(m.Content); match != "" {
		return strings.TrimRight(match, ".,!?;:)]}")
	}
	for _, emb := range m.Embeds {
		if emb.URL != "" {
			return emb.URL
		}
	}
	return ""
}

func ExtractAuthorFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		if strings.HasPrefix(parts[0], "@") {
			return parts[0]
		}
		if len(parts) >= 3 && (parts[1] == "status" || parts[1] == "statuses" || parts[1] == "video" || parts[1] == "photo") {
			return "@" + parts[0]
		}
	}
	return ""
}

func formatDescriptionWithHashtags(title, desc string, tags []string, fallbackURL string) string {
	text := title
	if text == "" {
		text = desc
	}
	if text == "" {
		return fmt.Sprintf("[Source Link](%s)", fallbackURL)
	}

	runes := []rune(text)
	if len(runes) > 500 {
		text = string(runes[:497]) + "..."
	}

	if fallbackURL = strings.TrimSpace(fallbackURL); fallbackURL != "" {
		cleanText := strings.ReplaceAll(text, "[", "\\[")
		cleanText = strings.ReplaceAll(cleanText, "]", "\\]")
		text = fmt.Sprintf("[%s](%s)", cleanText, fallbackURL)
	}

	var hashtags []string
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "#"))
		if tag != "" {
			hashtags = append(hashtags, "#"+tag)
		}
	}
	if len(hashtags) > 0 {
		text += "\n\n" + strings.Join(hashtags, " ")
		if len([]rune(text)) > 500 {
			text = string([]rune(text)[:497]) + "..."
		}
	}

	return text
}
