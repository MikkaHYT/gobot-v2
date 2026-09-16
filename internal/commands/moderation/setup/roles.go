package setup

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

func ConfigureRoles(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	if err := configureAutoroles(ctx, cfg, wizardMsgID); err != nil {
		return err
	}
	return configurePunishmentRoles(ctx, cfg, wizardMsgID)
}

func configureAutoroles(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	confirmed, timedOut := promptYesNo(ctx, wizardMsgID, "Roles Setup: Autoroles", "Would you like to configure Autoroles (roles automatically granted when new members join)?", "setup_ar")
	if timedOut || !confirmed {
		return nil
	}

	autoBtnID := fmt.Sprintf("setup_ar_auto_%s", ctx.Message.ID)
	prompt := &discordgo.MessageEmbed{
		Title:       "Roles Setup: Autorole Selection",
		Description: "Please mention the role(s) to assign to new members (e.g. `@Member`), or type their names/IDs.\n\nYou can also click **Auto-Create Member Role**.",
		Footer:      defaultFooter,
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Auto-Create Member Role", Style: discordgo.SuccessButton, CustomID: autoBtnID},
				},
			},
		},
	})

	res, err := awaitMessageOrButton(ctx, autoBtnID, 45*time.Second)
	if err != nil {
		return err
	}

	if res == "auto" {
		cfg.AutoroleIDs = []string{"auto"}
		return nil
	}

	if !strings.EqualFold(res, "skip") {
		var roleIDs []string
		for _, p := range strings.Fields(res) {
			if role, errRes := ctx.ResolveRole(p); errRes == nil && role != nil {
				if helpers.IsRoleDangerous(role) {
					continue
				}
				roleIDs = append(roleIDs, role.ID)
			} else if rID := ctx.ParseRoleID(p); rID != "" {
				if role, errFind := ctx.Session.State.Role(ctx.Message.GuildID, rID); errFind == nil && role != nil && helpers.IsRoleDangerous(role) {
					continue
				}
				roleIDs = append(roleIDs, rID)
			}
		}
		if len(roleIDs) > 0 {
			cfg.AutoroleIDs = roleIDs
		}
	}
	return nil
}

func configurePunishmentRoles(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	confirmed, timedOut := promptYesNo(ctx, wizardMsgID, "Roles Setup: Punishment Roles", "Would you like to configure punishment roles (Jail, Mute, Image Mute, Reaction Mute)?", "setup_p")
	if timedOut || !confirmed {
		return nil
	}

	_ = configureJailSystem(ctx, cfg, wizardMsgID)

	if id := promptRole(ctx, wizardMsgID, "Roles Setup: Standard Mute Role", "Please mention the role used for muted members (e.g. `@Muted`), or type its name/ID.\n\nOr click **Auto-Create**.", "Auto-Create Mute Role", "setup_mute"); id != "" {
		cfg.MuteRoleID = id
	}
	if id := promptRole(ctx, wizardMsgID, "Roles Setup: Image Mute Role", "Please mention the role to restrict image permissions (e.g. `@Image Muted`), or type its name/ID.\n\nOr click **Auto-Create**.", "Auto-Create Image Mute Role", "setup_imgmute"); id != "" {
		cfg.ImageMuteRoleID = id
	}
	if id := promptRole(ctx, wizardMsgID, "Roles Setup: Reaction Mute Role", "Please mention the role to restrict reaction permissions (e.g. `@Reaction Muted`), or type its name/ID.\n\nOr click **Auto-Create**.", "Auto-Create Reaction Mute Role", "setup_reacmute"); id != "" {
		cfg.ReactionMuteRoleID = id
	}

	return nil
}

func configureJailSystem(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID string) error {
	autoJailBtnID := fmt.Sprintf("setup_jail_auto_%s", ctx.Message.ID)
	prompt := &discordgo.MessageEmbed{
		Title:       "Roles Setup: Jail System",
		Description: "Please mention the role for jailed users (e.g. `@Jailed`), or type its name/ID.\n\nAlternatively, click **Auto-Create** to let the bot set up the Jail role & channel automatically.",
		Footer:      defaultFooter,
	}

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{prompt},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Auto-Create Jail System", Style: discordgo.SuccessButton, CustomID: autoJailBtnID},
				},
			},
		},
	})

	res, err := awaitMessageOrButton(ctx, autoJailBtnID, 45*time.Second)
	if err != nil {
		return err
	}

	if res == "auto" {
		cfg.JailRoleID = "auto"
		cfg.JailChannelID = "auto"
		return nil
	}

	if strings.EqualFold(res, "skip") {
		return nil
	}

	if role, errRes := ctx.ResolveRole(res); errRes == nil && role != nil {
		cfg.JailRoleID = role.ID
	} else if rID := ctx.ParseRoleID(res); rID != "" {
		cfg.JailRoleID = rID
	} else if strings.EqualFold(res, "none") || strings.EqualFold(res, "clear") {
		cfg.JailRoleID = ""
	}

	promptJailChan := &discordgo.MessageEmbed{
		Title:       "Roles Setup: Jail Channel",
		Description: "Please mention the channel used to jail members (e.g. `#jail-cell`), or type its name/ID.",
		Footer:      defaultFooter,
	}
	editPromptNoButtons(ctx.Session, ctx.Message.ChannelID, wizardMsgID, promptJailChan)

	if mJailChan, err := awaitMessage(ctx, 45*time.Second); err == nil {
		text := strings.TrimSpace(mJailChan.Content)
		_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, mJailChan.ID)
		if !strings.EqualFold(text, "skip") {
			if ch, errRes := ctx.ResolveChannel(text); errRes == nil && ch != nil {
				cfg.JailChannelID = ch.ID
			} else if chID := ctx.ParseChannelID(text); chID != "" {
				cfg.JailChannelID = chID
			} else if strings.EqualFold(text, "none") || strings.EqualFold(text, "clear") {
				cfg.JailChannelID = ""
			}
		}
	}
	return nil
}

func promptYesNo(ctx *bot.Context, wizardMsgID, title, description, prefix string) (bool, bool) {
	yesID := fmt.Sprintf("%s_yes_%s", prefix, ctx.Message.ID)
	noID := fmt.Sprintf("%s_no_%s", prefix, ctx.Message.ID)

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{{Title: title, Description: description}},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: "Yes", Style: discordgo.PrimaryButton, CustomID: yesID},
					discordgo.Button{Label: "No", Style: discordgo.SecondaryButton, CustomID: noID},
				},
			},
		},
	})

	sel, err := awaitInteraction(ctx, 45*time.Second, yesID, noID)
	if err != nil {
		return false, true
	}
	return sel == yesID, false
}

func promptRole(ctx *bot.Context, wizardMsgID, title, desc, btnLabel, prefix string) string {
	btnID := fmt.Sprintf("%s_auto_%s", prefix, ctx.Message.ID)

	_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: ctx.Message.ChannelID,
		ID:      wizardMsgID,
		Embeds:  &[]*discordgo.MessageEmbed{{Title: title, Description: desc, Footer: defaultFooter}},
		Components: &[]discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{Label: btnLabel, Style: discordgo.SuccessButton, CustomID: btnID},
				},
			},
		},
	})

	res, err := awaitMessageOrButton(ctx, btnID, 45*time.Second)
	if err != nil || strings.EqualFold(res, "skip") {
		return ""
	}
	if res == "auto" {
		return "auto"
	}

	if role, errRes := ctx.ResolveRole(res); errRes == nil && role != nil {
		return role.ID
	}
	return ctx.ParseRoleID(res)
}
