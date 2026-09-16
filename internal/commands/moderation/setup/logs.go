package setup

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"

	"github.com/bwmarrin/discordgo"
)

func ConfigureLogs(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	confirmed, timedOut := promptYesNo(ctx, wizardMsgID, "Logs Setup: Mod Audit Logs", "Would you like to enable mod audit logs in this server?", "setup_mod")
	if timedOut {
		return nil
	}
	if !confirmed {
		cfg.ModLogChannelID = ""
		return nil
	}

	configureModLogChannel(ctx, cfg, wizardMsgID)
	return nil
}

func configureModLogChannel(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	autoBtnID := fmt.Sprintf("setup_mod_auto_%s", ctx.Message.ID)
	prompt := &discordgo.MessageEmbed{
		Title:       "Logs Setup: Audit Logs Channel",
		Description: "Please mention the channel to send mod logs to (e.g. `#mod-logs`), or type its name/ID.\n\nYou can also click **Auto-Create Channel**.",
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
		cfg.ModLogChannelID = "auto"
		return
	}

	if ch, errRes := ctx.ResolveChannel(res); errRes == nil && ch != nil {
		cfg.ModLogChannelID = ch.ID
	} else if chanID := ctx.ParseChannelID(res); chanID != "" {
		cfg.ModLogChannelID = chanID
	}
}
