package setup

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"

	"github.com/bwmarrin/discordgo"
)

func ConfigureSafeguards(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	configureSelfReact(ctx, cfg, wizardMsgID)
	configureAntiMP3(ctx, cfg, wizardMsgID)
	configureSnipeLimit(ctx, cfg, wizardMsgID)
	return nil
}

func configureSelfReact(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	yesID := fmt.Sprintf("setup_sr_yes_%s", ctx.Message.ID)
	noID := fmt.Sprintf("setup_sr_no_%s", ctx.Message.ID)

	prompt := &discordgo.MessageEmbed{
		Title:       "Safeguards Setup: Disable Self-Reactions",
		Description: "Should the bot prevent users from reacting to their own messages?",
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Disable Self-Reactions", Style: discordgo.PrimaryButton, CustomID: yesID},
					discordgo.Button{Label: "Allow Self-Reactions", Style: discordgo.SecondaryButton, CustomID: noID},
				},
			},
		},
	})

	sel, err := awaitInteraction(ctx, 45*time.Second, yesID, noID)
	if err != nil {
		return
	}

	cfg.SelfReactDisabled = (sel == yesID)
}

func configureAntiMP3(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	disID := fmt.Sprintf("setup_antimp3_dis_%s", ctx.Message.ID)
	normID := fmt.Sprintf("setup_antimp3_norm_%s", ctx.Message.ID)
	strID := fmt.Sprintf("setup_antimp3_str_%s", ctx.Message.ID)

	prompt := &discordgo.MessageEmbed{
		Title:       "Safeguards Setup: Anti-MP3 Protection",
		Description: fmt.Sprintf("Current mode: `%s`\n\nChoose the Anti-MP3 protection mode for uploaded audio files:\n\n**Disable:** No restrictions.\n**Normal:** Scans audio files and blocks suspicious metadata/sizes.\n**Strict:** Enforces strict validation on all audio attachments.", cfg.AntiMP3Mode),
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Disable", Style: discordgo.SecondaryButton, CustomID: disID},
					discordgo.Button{Label: "Normal", Style: discordgo.PrimaryButton, CustomID: normID},
					discordgo.Button{Label: "Strict", Style: discordgo.DangerButton, CustomID: strID},
				},
			},
		},
	})

	sel, err := awaitInteraction(ctx, 45*time.Second, disID, normID, strID)
	if err != nil {
		return
	}

	switch sel {
	case normID:
		cfg.AntiMP3Mode = "normal"
	case strID:
		cfg.AntiMP3Mode = "strict"
	default:
		cfg.AntiMP3Mode = "disable"
	}
}

func configureSnipeLimit(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	prompt := &discordgo.MessageEmbed{
		Title:       "Safeguards Setup: Snipe History Limit",
		Description: fmt.Sprintf("Type the maximum number of deleted messages to save for `%ssnipe` (1-100, default `%d`).", ctx.Prefix, cfg.SnipeLimit),
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, prompt)

	mSnipe, err := awaitMessage(ctx, 45*time.Second)
	if err != nil {
		return
	}

	text := strings.TrimSpace(mSnipe.Content)
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, mSnipe.ID)

	if strings.EqualFold(text, "skip") {
		return
	}

	if num, errParse := strconv.Atoi(text); errParse == nil && num >= 1 && num <= 100 {
		cfg.SnipeLimit = num
	}
}
