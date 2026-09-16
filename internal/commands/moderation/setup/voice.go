package setup

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"

	"github.com/bwmarrin/discordgo"
)

func ConfigureVoice(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	configureRadioRestriction(ctx, cfg, wizardMsgID)
	configureRadioAutojoin(ctx, cfg, wizardMsgID)
	configureRadioPlayerChannel(ctx, cfg, wizardMsgID)
	configureVoiceMaster(ctx, cfg, wizardMsgID)
	return nil
}

func configureRadioRestriction(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	everyoneID := fmt.Sprintf("setup_radio_everyone_%s", ctx.Message.ID)
	djID := fmt.Sprintf("setup_radio_dj_%s", ctx.Message.ID)
	adminID := fmt.Sprintf("setup_radio_admin_%s", ctx.Message.ID)

	prompt := &discordgo.MessageEmbed{
		Title:       "Voice Setup: Radio Restriction Mode",
		Description: "Choose who is allowed to control the Voice Radio player:\n\n**Everyone:** Open to all members.\n**Voice Channel Only:** Restricted to members in the active voice channel.\n**Moderators Only:** Restricted to moderators.",
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Everyone", Style: discordgo.SecondaryButton, CustomID: everyoneID},
					discordgo.Button{Label: "Voice Channel Only", Style: discordgo.PrimaryButton, CustomID: djID},
					discordgo.Button{Label: "Moderators Only", Style: discordgo.DangerButton, CustomID: adminID},
				},
			},
		},
	})

	sel, err := awaitInteraction(ctx, 45*time.Second, everyoneID, djID, adminID)
	if err != nil {
		return
	}

	switch sel {
	case djID:
		cfg.RadioRestrictionMode = database.RadioRestrictionModeVoiceChannel
	case adminID:
		cfg.RadioRestrictionMode = database.RadioRestrictionModeModerators
	default:
		cfg.RadioRestrictionMode = database.RadioRestrictionModeAll
	}
}

func configureRadioAutojoin(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	prompt := &discordgo.MessageEmbed{
		Title:       "Voice Setup: Radio Autojoin Channel",
		Description: "Please mention or type the name/ID of the voice channel for the bot to autojoin, or type `skip`.",
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, prompt)

	mVoice, err := awaitMessage(ctx, 45*time.Second)
	if err != nil {
		return
	}

	text := strings.TrimSpace(mVoice.Content)
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, mVoice.ID)

	if strings.EqualFold(text, "skip") {
		return
	}

	if ch, errRes := ctx.ResolveChannel(text); errRes == nil && ch != nil {
		cfg.RadioAutojoinChannelID = ch.ID
	} else {
		cfg.RadioAutojoinChannelID = ctx.ParseChannelID(text)
	}
}

func configureRadioPlayerChannel(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	curr := "Disabled"
	if cfg.RadioPlayerChannelID != "" {
		curr = fmt.Sprintf("<#%s>", cfg.RadioPlayerChannelID)
	}

	prompt := &discordgo.MessageEmbed{
		Title:       "Voice Setup: Dedicated Radio Player Channel",
		Description: fmt.Sprintf("Current player channel: %s\n\nPlease mention or type the text channel for the hands-free radio player controller, or type `disable` / `skip`.", curr),
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, prompt)

	mPlayer, err := awaitMessage(ctx, 45*time.Second)
	if err != nil {
		return
	}

	text := strings.TrimSpace(mPlayer.Content)
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, mPlayer.ID)

	if strings.EqualFold(text, "skip") {
		return
	}
	if strings.EqualFold(text, "disable") || strings.EqualFold(text, "clear") || strings.EqualFold(text, "none") {
		cfg.RadioPlayerChannelID = ""
		return
	}

	if ch, errRes := ctx.ResolveChannel(text); errRes == nil && ch != nil {
		cfg.RadioPlayerChannelID = ch.ID
	} else {
		cfg.RadioPlayerChannelID = ctx.ParseChannelID(text)
	}
}

func configureVoiceMaster(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) {
	autoBtnID := fmt.Sprintf("setup_vm_auto_%s", ctx.Message.ID)
	curr := "Disabled"
	if cfg.VoiceMasterTriggerChannelID == "auto" {
		curr = "*(To be created)*"
	} else if cfg.VoiceMasterTriggerChannelID != "" {
		curr = fmt.Sprintf("<#%s>", cfg.VoiceMasterTriggerChannelID)
	}

	prompt := &discordgo.MessageEmbed{
		Title:       "Voice Setup: VoiceMaster (Dynamic Voice Channels)",
		Description: fmt.Sprintf("Current trigger channel: %s\n\nVoiceMaster provides temporary channels created automatically when members join a trigger channel.\n\nPlease mention or type the voice channel to use (or type `disable` / `skip`).\n\nYou can also click **Auto-Create VoiceMaster** to set up a dedicated category & trigger channel automatically.", curr),
		Footer:      defaultFooter,
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Auto-Create VoiceMaster", Style: discordgo.SuccessButton, CustomID: autoBtnID},
				},
			},
		},
	})

	res, err := awaitMessageOrButton(ctx, autoBtnID, 45*time.Second)
	if err != nil || strings.EqualFold(res, "skip") {
		return
	}

	if res == "auto" {
		cfg.VoiceMasterTriggerChannelID = "auto"
		return
	}

	if strings.EqualFold(res, "disable") || strings.EqualFold(res, "clear") || strings.EqualFold(res, "off") || strings.EqualFold(res, "none") {
		cfg.VoiceMasterTriggerChannelID = ""
		cfg.VoiceMasterCategoryID = ""
		return
	}

	if ch, errRes := ctx.ResolveChannel(res); errRes == nil && ch != nil {
		cfg.VoiceMasterTriggerChannelID = ch.ID
		if ch.ParentID != "" {
			cfg.VoiceMasterCategoryID = ch.ParentID
		}
	} else if chanID := ctx.ParseChannelID(res); chanID != "" {
		cfg.VoiceMasterTriggerChannelID = chanID
		if ch, errCh := ctx.Session.Channel(chanID); errCh == nil && ch != nil && ch.ParentID != "" {
			cfg.VoiceMasterCategoryID = ch.ParentID
		}
	}
}
