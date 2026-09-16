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

func ConfigureStarboard(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	confirmed, timedOut := promptYesNo(ctx, wizardMsgID, "Starboard Setup: System Status", "Would you like to enable the Starboard system?", "setup_star")
	if timedOut {
		return nil
	}
	if !confirmed {
		cfg.StarboardChannelID = ""
		return nil
	}

	configureStarboardChannel(ctx, cfg, wizardMsgID)
	configureStarboardThreshold(ctx, cfg, wizardMsgID)

	return nil
}

func configureStarboardChannel(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	autoBtnID := fmt.Sprintf("setup_star_auto_%s", ctx.Message.ID)
	prompt := &discordgo.MessageEmbed{
		Title:       "Starboard Setup: Channel",
		Description: "Please mention the channel to post starred messages to (e.g. `#starboard`), or type its name/ID.\n\nYou can also click **Auto-Create Channel**.",
		Footer:      defaultFooter,
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Auto-Create Channel", Style: discordgo.SuccessButton, CustomID: autoBtnID},
				},
			},
		},
	})

	res, err := awaitMessageOrButton(ctx, autoBtnID, 45*time.Second)
	if err != nil || strings.EqualFold(res, "skip") {
		return
	}

	if res == "auto" {
		cfg.StarboardChannelID = "auto"
		return
	}

	if ch, errRes := ctx.ResolveChannel(res); errRes == nil && ch != nil {
		cfg.StarboardChannelID = ch.ID
	} else if chanID := ctx.ParseChannelID(res); chanID != "" {
		cfg.StarboardChannelID = chanID
	}
}

func configureStarboardThreshold(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	prompt := &discordgo.MessageEmbed{
		Title:       "Starboard Setup: Reaction Threshold",
		Description: fmt.Sprintf("Type the number of stars required to pin a message (default `%d`).", cfg.StarboardThreshold),
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, prompt)

	mThreshold, err := awaitMessage(ctx, 45*time.Second)
	if err != nil {
		return
	}

	text := strings.TrimSpace(mThreshold.Content)
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, mThreshold.ID)

	if strings.EqualFold(text, "skip") {
		return
	}

	if num, errParse := strconv.Atoi(text); errParse == nil && num > 0 {
		cfg.StarboardThreshold = num
	}
}
