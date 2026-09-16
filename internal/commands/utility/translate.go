package utility

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type TranslateCmd struct{}

func (c *TranslateCmd) Name() string      { return "translate" }
func (c *TranslateCmd) Aliases() []string { return []string{"trans", "tr"} }
func (c *TranslateCmd) Category() string  { return "Utility" }
func (c *TranslateCmd) Description() string {
	return "Translate text into another language (default: English)."
}
func (c *TranslateCmd) Usage() string   { return "[target_lang] <text|reply>" }
func (c *TranslateCmd) Example() string { return "es Hello, how are you?" }

func (c *TranslateCmd) Execute(ctx *bot.Context) error {
	if len(ctx.Args) == 0 && ctx.Message.ReferencedMessage == nil {
		_, _ = ctx.SendUsage(c)
		return nil
	}

	targetLang := "en"
	textToTranslate := ""

	if len(ctx.Args) >= 2 && len(ctx.Args[0]) <= 5 {
		targetLang = strings.ToLower(ctx.Args[0])
		textToTranslate = strings.Join(ctx.Args[1:], " ")
	} else if len(ctx.Args) == 1 && len(ctx.Args[0]) <= 5 {
		if ctx.Message.ReferencedMessage != nil && ctx.Message.ReferencedMessage.Content != "" {
			targetLang = strings.ToLower(ctx.Args[0])
			textToTranslate = ctx.Message.ReferencedMessage.Content
		} else {
			_, _ = ctx.SendUsage(c)
			return nil
		}
	} else if len(ctx.Args) >= 1 {
		textToTranslate = strings.Join(ctx.Args, " ")
	} else if ctx.Message.ReferencedMessage != nil {
		textToTranslate = ctx.Message.ReferencedMessage.Content
	}

	if textToTranslate == "" {
		_, _ = ctx.SendUsage(c)
		return nil
	}

	apiURL := fmt.Sprintf("https://translate.googleapis.com/translate_a/single?client=gtx&sl=auto&tl=%s&dt=t&q=%s",
		targetLang, url.QueryEscape(textToTranslate))

	client := helpers.NewSafeHTTPClient(helpers.DurationTimeoutHTTP)
	req, err := http.NewRequestWithContext(ctx.Context(), http.MethodGet, apiURL, nil)
	if err != nil {
		return ctx.SendError("Failed to construct translation request.")
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return ctx.SendError("Translation service is currently unreachable.")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ctx.SendError("Translation service returned an unexpected response.")
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return ctx.SendError("Failed to read translation response.")
	}

	var raw []any
	if err := json.Unmarshal(body, &raw); err != nil || len(raw) == 0 {
		return ctx.SendError("Failed to parse translation result.")
	}

	translatedText := ""
	if sentences, ok := raw[0].([]any); ok {
		for _, s := range sentences {
			if sentencePart, ok := s.([]any); ok && len(sentencePart) > 0 {
				if partStr, ok := sentencePart[0].(string); ok {
					translatedText += partStr
				}
			}
		}
	}

	if translatedText == "" {
		return ctx.SendError("No translation output received.")
	}

	desc := fmt.Sprintf("**Original:** %s\n\n**Translated:** %s", textToTranslate, translatedText)
	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Translation (%s)", strings.ToUpper(targetLang)),
		Description: helpers.TruncateStringWithEllipsis(desc, 4000),
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}
