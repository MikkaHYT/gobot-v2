package setup

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"

	"github.com/bwmarrin/discordgo"
)

func ConfigureWelcome(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	configureWelcomeChannel(ctx, cfg, wizardMsgID)
	configureGoodbyeChannel(ctx, cfg, wizardMsgID)
	return nil
}

func configureWelcomeChannel(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	curr := "Disabled"
	if cfg.WelcomeChannelID != "" {
		curr = fmt.Sprintf("<#%s>", cfg.WelcomeChannelID)
	}

	prompt := &discordgo.MessageEmbed{
		Title:       "Welcome Setup: Member Join Channel",
		Description: fmt.Sprintf("Current welcome channel: %s\n\nPlease mention or type the channel to send welcome cards/messages when new members join, or type `disable` / `skip`.", curr),
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, prompt)

	m, err := awaitMessage(ctx, 45*time.Second)
	if err != nil {
		return
	}

	text := strings.TrimSpace(m.Content)
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, m.ID)

	if strings.EqualFold(text, "skip") {
		return
	}
	if strings.EqualFold(text, "disable") || strings.EqualFold(text, "clear") || strings.EqualFold(text, "none") {
		cfg.WelcomeChannelID = ""
		return
	}

	if ch, errRes := ctx.ResolveChannel(text); errRes == nil && ch != nil {
		cfg.WelcomeChannelID = ch.ID
	} else {
		cfg.WelcomeChannelID = ctx.ParseChannelID(text)
	}
}

func configureGoodbyeChannel(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	curr := "Disabled"
	if cfg.GoodbyeChannelID != "" {
		curr = fmt.Sprintf("<#%s>", cfg.GoodbyeChannelID)
	}

	prompt := &discordgo.MessageEmbed{
		Title:       "Goodbye Setup: Member Leave Channel",
		Description: fmt.Sprintf("Current goodbye channel: %s\n\nPlease mention or type the channel to send goodbye cards/messages when members leave, or type `disable` / `skip`.", curr),
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, prompt)

	m, err := awaitMessage(ctx, 45*time.Second)
	if err != nil {
		return
	}

	text := strings.TrimSpace(m.Content)
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, m.ID)

	if strings.EqualFold(text, "skip") {
		return
	}
	if strings.EqualFold(text, "disable") || strings.EqualFold(text, "clear") || strings.EqualFold(text, "none") {
		cfg.GoodbyeChannelID = ""
		return
	}

	if ch, errRes := ctx.ResolveChannel(text); errRes == nil && ch != nil {
		cfg.GoodbyeChannelID = ch.ID
	} else {
		cfg.GoodbyeChannelID = ctx.ParseChannelID(text)
	}
}
