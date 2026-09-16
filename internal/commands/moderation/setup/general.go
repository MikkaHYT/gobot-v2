package setup

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"

	"github.com/bwmarrin/discordgo"
)

var hexRegex = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

func ConfigureGeneral(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	configurePrefix(ctx, cfg, wizardMsgID)
	configureEmbedColor(ctx, cfg, wizardMsgID)
	return nil
}

func configurePrefix(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	prompt := &discordgo.MessageEmbed{
		Title:       "Setup: Command Prefix",
		Description: fmt.Sprintf("Type your desired command prefix in chat (e.g. `!`, `?`, or `-`).\nCurrent prefix: `%s`", cfg.Prefix),
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, prompt)

	mPrefix, err := awaitMessage(ctx, 45*time.Second)
	if err != nil {
		return
	}

	text := strings.TrimSpace(mPrefix.Content)
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, mPrefix.ID)

	if strings.EqualFold(text, "skip") || text == "" || len(text) > 5 {
		return
	}

	cfg.Prefix = text
}

func configureEmbedColor(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	prompt := &discordgo.MessageEmbed{
		Title:       "Setup: Embed Color",
		Description: fmt.Sprintf("Type the default hex color code for your server's embeds (e.g. `7C5CFF` or `#7C5CFF`).\nCurrent color: `#%s`.", cfg.EmbedColor),
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, prompt)

	mColor, err := awaitMessage(ctx, 45*time.Second)
	if err != nil {
		return
	}

	text := strings.TrimPrefix(strings.TrimSpace(mColor.Content), "#")
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, mColor.ID)

	if strings.EqualFold(text, "skip") || !hexRegex.MatchString(text) {
		return
	}

	cfg.EmbedColor = strings.ToUpper(text)
}
