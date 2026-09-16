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

func ConfigureLeveling(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	configureLevelUpMessages(ctx, cfg, wizardMsgID)
	configureXPMultiplier(ctx, cfg, wizardMsgID)
	configureLevelRoleStacking(ctx, cfg, wizardMsgID)
	return nil
}

func configureLevelUpMessages(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	enableID := fmt.Sprintf("setup_lvl_enable_%s", ctx.Message.ID)
	disableID := fmt.Sprintf("setup_lvl_disable_%s", ctx.Message.ID)
	skipID := fmt.Sprintf("setup_lvl_skip_%s", ctx.Message.ID)

	prompt := &discordgo.MessageEmbed{
		Title:       "Leveling Setup: Level-up Announcements",
		Description: fmt.Sprintf("Current status: `%s`\n\nChoose whether the bot should send announcement messages when members level up.", boolLabel(cfg.LevelUpMessagesEnabled)),
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Enable", Style: discordgo.SuccessButton, CustomID: enableID},
					discordgo.Button{Label: "Disable", Style: discordgo.DangerButton, CustomID: disableID},
					discordgo.Button{Label: "Skip", Style: discordgo.SecondaryButton, CustomID: skipID},
				},
			},
		},
	})

	sel, err := awaitInteraction(ctx, 45*time.Second, enableID, disableID, skipID)
	if err != nil || sel == skipID {
		return
	}

	cfg.LevelUpMessagesEnabled = (sel == enableID)
}

func configureXPMultiplier(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	prompt := &discordgo.MessageEmbed{
		Title:       "Leveling Setup: XP Multiplier",
		Description: fmt.Sprintf("Current multiplier: `%.2fx`\n\nPlease enter an XP multiplier between `0.1` and `10.0` (e.g. `1.5`), or type `skip`.", cfg.XPMultiplier),
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

	val, errParse := strconv.ParseFloat(text, 64)
	if errParse == nil && val >= 0.1 && val <= 10.0 {
		cfg.XPMultiplier = val
	}
}

func configureLevelRoleStacking(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	stackID := fmt.Sprintf("setup_stack_yes_%s", ctx.Message.ID)
	highestID := fmt.Sprintf("setup_stack_no_%s", ctx.Message.ID)
	skipID := fmt.Sprintf("setup_stack_skip_%s", ctx.Message.ID)

	status := "Stack All Roles"
	if !cfg.LevelRolesStack {
		status = "Keep Highest Role Only"
	}

	prompt := &discordgo.MessageEmbed{
		Title:       "Leveling Setup: Level Role Stacking",
		Description: fmt.Sprintf("Current setting: `%s`\n\nChoose whether members keep all unlocked level roles or only retain the highest level role.", status),
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Stack All", Style: discordgo.PrimaryButton, CustomID: stackID},
					discordgo.Button{Label: "Highest Only", Style: discordgo.SecondaryButton, CustomID: highestID},
					discordgo.Button{Label: "Skip", Style: discordgo.SecondaryButton, CustomID: skipID},
				},
			},
		},
	})

	sel, err := awaitInteraction(ctx, 45*time.Second, stackID, highestID, skipID)
	if err != nil || sel == skipID {
		return
	}

	cfg.LevelRolesStack = (sel == stackID)
}
