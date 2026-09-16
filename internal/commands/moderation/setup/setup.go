package setup

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/database"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type SetupCmd struct{}

func (c *SetupCmd) Name() string      { return "setup" }
func (c *SetupCmd) Aliases() []string { return []string{"guildsetup", "configwizard"} }
func (c *SetupCmd) Category() string  { return "Server Configuration" }
func (c *SetupCmd) Description() string {
	return "Interactive dashboard for configuring and reviewing server settings."
}
func (c *SetupCmd) Usage() string {
	return "[general|logs|starboard|welcome|roles|voice|leveling|safeguards]"
}
func (c *SetupCmd) Example() string    { return "roles" }
func (c *SetupCmd) Permissions() int64 { return discordgo.PermissionAdministrator }

func (c *SetupCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "general", Description: "Configure command prefix and embed color"},
		{Name: "logs", Description: "Configure moderation audit log channel"},
		{Name: "starboard", Description: "Configure starboard channel and star threshold"},
		{Name: "welcome", Description: "Configure member welcome and goodbye channels"},
		{Name: "roles", Description: "Configure autoroles, mute roles, and jail system"},
		{Name: "voice", Description: "Configure voice radio restriction, autojoin, and dedicated player"},
		{Name: "leveling", Description: "Configure XP multiplier, level-up messages, and role stacking"},
		{Name: "safeguards", Description: "Configure self-reactions and snipe history limit"},
	}
}

func (c *SetupCmd) Execute(ctx *bot.Context) error {
	acquired, activeUser := AcquireSetupLock(ctx.Message.GuildID, ctx.Message.Author.ID)
	if !acquired {
		return ctx.SendError(fmt.Sprintf("<@%s> is already running a setup session in this server. Wait until it finishes.", activeUser))
	}
	defer ReleaseSetupLock(ctx.Message.GuildID)

	cfg, err := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			cfg = &database.GuildConfig{
				GuildID:              ctx.Message.GuildID,
				Prefix:               ",",
				EmbedColor:           "3498DB",
				AntiMP3Mode:          "disable",
				StarboardThreshold:   3,
				SnipeLimit:           10,
				RadioRestrictionMode: database.RadioRestrictionModeAll,
			}
		} else {
			return ctx.SendError("Failed to load server configuration due to an internal database error.")
		}
	}

	welcomeEmbed := &discordgo.MessageEmbed{
		Title:       "Server Settings",
		Description: fmt.Sprintf("Stage changes by selecting a section below. Nothing is saved until you click **Save Changes**.\n\nUse `%ssetup <section>` to open one section directly.", ctx.Prefix),
	}
	selectID := fmt.Sprintf("setup_select_%s", ctx.Message.ID)
	fullID := fmt.Sprintf("setup_full_%s", ctx.Message.ID)
	saveID := fmt.Sprintf("setup_save_%s", ctx.Message.ID)
	cancelID := fmt.Sprintf("setup_cancel_%s", ctx.Message.ID)
	components := buildDashboardComponents(selectID, fullID, saveID, cancelID)

	wizardMsg, err := ctx.Session.ChannelMessageSendComplex(ctx.Message.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{welcomeEmbed},
		Components: components,
	})
	if err != nil {
		return err
	}

	abortWizard := func(msg string) error {
		_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, wizardMsg.ID)
		return ctx.SendError(msg)
	}

	if len(ctx.Args) > 0 {
		if err := runSubcommand(ctx, cfg, wizardMsg.ID, ctx.Args[0]); err != nil {
			return abortWizard("Error configuring module: " + err.Error())
		}
	}

	for {
		_, _ = ctx.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    ctx.Message.ChannelID,
			ID:         wizardMsg.ID,
			Embeds:     &[]*discordgo.MessageEmbed{buildSummaryEmbed(cfg)},
			Components: &components,
		})

		btnID, selectedValues, timedOut := awaitComponentInteraction(ctx, 90*time.Second, selectID, fullID, saveID, cancelID)
		if timedOut {
			return abortWizard("Setup dashboard timed out.")
		}

		if btnID == cancelID || (len(selectedValues) > 0 && selectedValues[0] == "cancel") {
			return abortWizard("Setup cancelled. Settings were not saved.")
		}

		if btnID == saveID || (len(selectedValues) > 0 && selectedValues[0] == "save") {
			break
		}

		subToRun := ""
		if btnID == fullID {
			subToRun = "full"
		} else if len(selectedValues) > 0 {
			subToRun = selectedValues[0]
		}

		if subToRun != "" {
			if err := runSubcommand(ctx, cfg, wizardMsg.ID, subToRun); err != nil {
				return abortWizard("Error configuring module: " + err.Error())
			}
		}
	}
	_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, wizardMsg.ID)

	if err := provisionAndSave(ctx, cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	_, err = ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: "Configuration saved successfully. Roles and channels have been updated.",
		Color:       helpers.ColorSuccess,
	})
	return err
}

func runSubcommand(ctx *bot.Context, cfg *database.GuildConfig, wizardMsgID, module string) error {
	switch strings.ToLower(module) {
	case "general":
		return ConfigureGeneral(ctx, cfg, wizardMsgID)
	case "logs":
		return ConfigureLogs(ctx, cfg, wizardMsgID)
	case "starboard":
		return ConfigureStarboard(ctx, cfg, wizardMsgID)
	case "welcome":
		return ConfigureWelcome(ctx, cfg, wizardMsgID)
	case "roles":
		return ConfigureRoles(ctx, cfg, wizardMsgID)
	case "voice":
		return ConfigureVoice(ctx, cfg, wizardMsgID)
	case "leveling":
		return ConfigureLeveling(ctx, cfg, wizardMsgID)
	case "safeguards":
		return ConfigureSafeguards(ctx, cfg, wizardMsgID)
	case "full":
		for _, fn := range []func(*bot.Context, *database.GuildConfig, string) error{
			ConfigureGeneral, ConfigureLogs, ConfigureStarboard, ConfigureWelcome, ConfigureRoles, ConfigureVoice, ConfigureLeveling, ConfigureSafeguards,
		} {
			if err := fn(ctx, cfg, wizardMsgID); err != nil {
				return err
			}
		}
	}
	return nil
}

func provisionAndSave(ctx *bot.Context, cfg *database.GuildConfig) error {
	rolesToCreate := []struct {
		id    *string
		name  string
		color int
	}{
		{&cfg.MuteRoleID, "Muted", 0},
		{&cfg.ImageMuteRoleID, "Image Muted", 0},
		{&cfg.ReactionMuteRoleID, "Reaction Muted", 0},
		{&cfg.JailRoleID, "Jailed", 0},
	}

	for _, r := range rolesToCreate {
		if *r.id == "auto" {
			if role, err := autoCreateRole(ctx.Session, ctx.Message.GuildID, r.name, r.color); err == nil {
				*r.id = role.ID
			} else {
				*r.id = ""
			}
		}
	}

	for i, rID := range cfg.AutoroleIDs {
		if rID == "auto" {
			if role, err := autoCreateRole(ctx.Session, ctx.Message.GuildID, "Member", helpers.ColorInfo); err == nil {
				cfg.AutoroleIDs[i] = role.ID
			}
		}
	}

	if cfg.ModLogChannelID == "auto" {
		if ch, err := autoCreateTextChannel(ctx.Session, ctx.Message.GuildID, "mod-logs", "Moderation logs channel automatically created."); err == nil {
			cfg.ModLogChannelID = ch.ID
		} else {
			cfg.ModLogChannelID = ""
		}
	}
	if cfg.StarboardChannelID == "auto" {
		if ch, err := autoCreateTextChannel(ctx.Session, ctx.Message.GuildID, "starboard", "starred messages."); err == nil {
			cfg.StarboardChannelID = ch.ID
		} else {
			cfg.StarboardChannelID = ""
		}
	}
	if cfg.JailChannelID == "auto" {
		overwrites := []*discordgo.PermissionOverwrite{
			{
				ID:   ctx.Message.GuildID,
				Type: discordgo.PermissionOverwriteTypeRole,
				Deny: discordgo.PermissionViewChannel,
			},
		}
		if cfg.JailRoleID != "" && cfg.JailRoleID != "auto" {
			overwrites = append(overwrites, &discordgo.PermissionOverwrite{
				ID:    cfg.JailRoleID,
				Type:  discordgo.PermissionOverwriteTypeRole,
				Allow: discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory,
			})
		}
		if ch, err := ctx.Session.GuildChannelCreateComplex(ctx.Message.GuildID, discordgo.GuildChannelCreateData{
			Name:                 "jail-cell",
			Type:                 discordgo.ChannelTypeGuildText,
			Topic:                "Jail cell for punished users.",
			PermissionOverwrites: overwrites,
		}); err == nil {
			cfg.JailChannelID = ch.ID
		} else {
			cfg.JailChannelID = ""
		}
	}
	if cfg.VoiceMasterTriggerChannelID == "auto" {
		categoryID := cfg.VoiceMasterCategoryID
		channels, _ := ctx.Session.GuildChannels(ctx.Message.GuildID)
		if categoryID == "" || categoryID == "auto" {
			for _, ch := range channels {
				if ch.Type == discordgo.ChannelTypeGuildCategory && strings.EqualFold(ch.Name, "Voice Channels") {
					categoryID = ch.ID
					break
				}
			}
			if categoryID == "" {
				if cat, err := ctx.Session.GuildChannelCreateComplex(ctx.Message.GuildID, discordgo.GuildChannelCreateData{
					Name: "Voice Channels",
					Type: discordgo.ChannelTypeGuildCategory,
				}); err == nil {
					categoryID = cat.ID
				}
			}
			cfg.VoiceMasterCategoryID = categoryID
		}
		if trigger, err := ctx.Session.GuildChannelCreateComplex(ctx.Message.GuildID, discordgo.GuildChannelCreateData{
			Name:     "Join to Create",
			Type:     discordgo.ChannelTypeGuildVoice,
			ParentID: categoryID,
		}); err == nil {
			cfg.VoiceMasterTriggerChannelID = trigger.ID
		} else {
			cfg.VoiceMasterTriggerChannelID = ""
		}
	}

	if cfg.ModLogChannelID != "" {
		if err := helpers.HideChannel(ctx.Session, ctx.Message.GuildID, cfg.ModLogChannelID); err != nil {
			bot.Warnf("[SETUP] Failed to hide modlog channel %s: %v", cfg.ModLogChannelID, err)
		}
	}
	if cfg.StarboardChannelID != "" {
		if err := helpers.LockStarboardChannel(ctx.Session, ctx.Message.GuildID, cfg.StarboardChannelID); err != nil {
			bot.Warnf("[SETUP] Failed to lock starboard channel %s: %v", cfg.StarboardChannelID, err)
		}
	}
	if cfg.ImageMuteRoleID != "" {
		if err := helpers.EnsureRoleOverwrites(ctx.Session, ctx.Message.GuildID, cfg.ImageMuteRoleID, helpers.ImageMuteDenyFlags); err != nil {
			bot.Warnf("[SETUP] Failed to set overwrites for image mute role %s: %v", cfg.ImageMuteRoleID, err)
		}
	}
	if cfg.ReactionMuteRoleID != "" {
		if err := helpers.EnsureRoleOverwrites(ctx.Session, ctx.Message.GuildID, cfg.ReactionMuteRoleID, helpers.ReactionMuteDenyFlags); err != nil {
			bot.Warnf("[SETUP] Failed to set overwrites for reaction mute role %s: %v", cfg.ReactionMuteRoleID, err)
		}
	}
	if cfg.MuteRoleID != "" {
		if err := helpers.EnsureRoleOverwrites(ctx.Session, ctx.Message.GuildID, cfg.MuteRoleID, helpers.MuteDenyFlags); err != nil {
			bot.Warnf("[SETUP] Failed to set overwrites for mute role %s: %v", cfg.MuteRoleID, err)
		}
	}
	if cfg.JailRoleID != "" {
		if err := helpers.EnsureRoleOverwrites(ctx.Session, ctx.Message.GuildID, cfg.JailRoleID, helpers.JailDenyFlags); err != nil {
			bot.Warnf("[SETUP] Failed to set overwrites for jail role %s: %v", cfg.JailRoleID, err)
		}
	}

	updates := map[database.GuildSetting]any{
		database.SettingPrefix:                      cfg.Prefix,
		database.SettingEmbedColor:                  cfg.EmbedColor,
		database.SettingModLogChannelID:             cfg.ModLogChannelID,
		database.SettingStarboardChannelID:          cfg.StarboardChannelID,
		database.SettingStarboardThreshold:          cfg.StarboardThreshold,
		database.SettingAutoroleIDs:                 cfg.AutoroleIDs,
		database.SettingSelfReactDisabled:           cfg.SelfReactDisabled,
		database.SettingAntiMP3Mode:                 cfg.AntiMP3Mode,
		database.SettingSnipeLimit:                  cfg.SnipeLimit,
		database.SettingRadioRestrictionMode:        cfg.RadioRestrictionMode,
		database.SettingRadioAutojoinChannelID:      cfg.RadioAutojoinChannelID,
		database.SettingRadioPlayerChannelID:        cfg.RadioPlayerChannelID,
		database.SettingWelcomeChannelID:            cfg.WelcomeChannelID,
		database.SettingGoodbyeChannelID:            cfg.GoodbyeChannelID,
		database.SettingXPMultiplier:                cfg.XPMultiplier,
		database.SettingLevelRolesStack:             cfg.LevelRolesStack,
		database.SettingJailRoleID:                  cfg.JailRoleID,
		database.SettingJailChannelID:               cfg.JailChannelID,
		database.SettingMuteRoleID:                  cfg.MuteRoleID,
		database.SettingImageMuteRoleID:             cfg.ImageMuteRoleID,
		database.SettingReactionMuteRoleID:          cfg.ReactionMuteRoleID,
		database.SettingVoiceMasterTriggerChannelID: cfg.VoiceMasterTriggerChannelID,
		database.SettingVoiceMasterCategoryID:       cfg.VoiceMasterCategoryID,
		database.SettingLevelUpMessagesEnabled:      cfg.LevelUpMessagesEnabled,
	}
	if err := ctx.DB.UpdateGuildSettings(ctx.Message.GuildID, updates); err != nil {
		return err
	}
	if ctx.Radio != nil {
		_ = ctx.Radio.RefreshSettings(ctx.Context(), ctx.Message.GuildID)
	}
	return nil
}

func buildDashboardComponents(selectID, fullID, saveID, cancelID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.SelectMenu{
					CustomID:    selectID,
					Placeholder: "Choose a settings section...",
					Options: []discordgo.SelectMenuOption{
						{Label: "General", Value: "general", Description: "Prefix and embed color"},
						{Label: "Logs", Value: "logs", Description: "Moderation audit-log channel"},
						{Label: "Starboard", Value: "starboard", Description: "Channel and reaction threshold"},
						{Label: "Welcome & Goodbye", Value: "welcome", Description: "Join and leave channel destinations"},
						{Label: "Roles & Punishments", Value: "roles", Description: "Autoroles, jail, and mute roles"},
						{Label: "Radio & Voice", Value: "voice", Description: "Radio access, player channel, and autojoin"},
						{Label: "Leveling", Value: "leveling", Description: "XP multiplier, level-up messages, and role stacking"},
						{Label: "Safeguards", Value: "safeguards", Description: "Self-reactions and snipe history"},
					},
				},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Run Full Setup", Style: discordgo.PrimaryButton, CustomID: fullID},
				discordgo.Button{Label: "Save Changes", Style: discordgo.SuccessButton, CustomID: saveID},
				discordgo.Button{Label: "Cancel", Style: discordgo.DangerButton, CustomID: cancelID},
			},
		},
	}
}

func buildSummaryEmbed(cfg *database.GuildConfig) *discordgo.MessageEmbed {
	modLogVal := "Disabled"
	if cfg.ModLogChannelID == "auto" {
		modLogVal = "*(To be created)*"
	} else if cfg.ModLogChannelID != "" {
		modLogVal = fmt.Sprintf("<#%s>", cfg.ModLogChannelID)
	}

	starboardVal := "Disabled"
	if cfg.StarboardChannelID == "auto" {
		starboardVal = fmt.Sprintf("*(To be created)* (Threshold: %d)", cfg.StarboardThreshold)
	} else if cfg.StarboardChannelID != "" {
		starboardVal = fmt.Sprintf("<#%s> (Threshold: %d)", cfg.StarboardChannelID, cfg.StarboardThreshold)
	}

	welcomeVal := "Disabled"
	if cfg.WelcomeChannelID != "" {
		welcomeVal = fmt.Sprintf("<#%s>", cfg.WelcomeChannelID)
	}
	goodbyeVal := "Disabled"
	if cfg.GoodbyeChannelID != "" {
		goodbyeVal = fmt.Sprintf("<#%s>", cfg.GoodbyeChannelID)
	}

	autoroleVal := "None"
	if len(cfg.AutoroleIDs) > 0 {
		var mentions []string
		for _, rID := range cfg.AutoroleIDs {
			if rID == "auto" {
				mentions = append(mentions, "*(To be created)*")
			} else {
				mentions = append(mentions, fmt.Sprintf("<@&%s>", rID))
			}
		}
		autoroleVal = strings.Join(mentions, ", ")
	}

	jailVal := "Not Configured"
	if cfg.JailRoleID == "auto" || cfg.JailChannelID == "auto" {
		jailVal = "Role: *(To be created)* | Channel: *(To be created)*"
	} else if cfg.JailRoleID != "" || cfg.JailChannelID != "" {
		jailVal = fmt.Sprintf("Role: <@&%s> | Channel: <#%s>", cfg.JailRoleID, cfg.JailChannelID)
	}

	muteVal := "Not Configured"
	if cfg.MuteRoleID == "auto" {
		muteVal = "Standard: *(To be created)*"
	} else if cfg.MuteRoleID != "" {
		muteVal = fmt.Sprintf("<@&%s>", cfg.MuteRoleID)
	}
	if cfg.ImageMuteRoleID == "auto" {
		if muteVal == "Not Configured" {
			muteVal = "Image: *(To be created)*"
		} else {
			muteVal += "\nImage: *(To be created)*"
		}
	} else if cfg.ImageMuteRoleID != "" {
		if muteVal == "Not Configured" {
			muteVal = fmt.Sprintf("Image: <@&%s>", cfg.ImageMuteRoleID)
		} else {
			muteVal += fmt.Sprintf("\nImage: <@&%s>", cfg.ImageMuteRoleID)
		}
	}
	if cfg.ReactionMuteRoleID == "auto" {
		if muteVal == "Not Configured" {
			muteVal = "Reaction: *(To be created)*"
		} else {
			muteVal += "\nReaction: *(To be created)*"
		}
	} else if cfg.ReactionMuteRoleID != "" {
		if muteVal == "Not Configured" {
			muteVal = fmt.Sprintf("Reaction: <@&%s>", cfg.ReactionMuteRoleID)
		} else {
			muteVal += fmt.Sprintf("\nReaction: <@&%s>", cfg.ReactionMuteRoleID)
		}
	}

	prefixVal := cfg.Prefix
	if prefixVal == "" {
		prefixVal = ","
	}
	colorVal := cfg.EmbedColor
	if colorVal == "" {
		colorVal = "default"
	} else {
		colorVal = "#" + colorVal
	}
	radioMode := database.NormalizeRadioRestrictionMode(cfg.RadioRestrictionMode)
	if radioMode == "" {
		radioMode = database.RadioRestrictionModeAll
	}
	radioModeLabel := map[string]string{
		database.RadioRestrictionModeAll:          "Everyone",
		database.RadioRestrictionModeVoiceChannel: "Voice channel only",
		database.RadioRestrictionModeModerators:   "Moderators only",
	}[radioMode]
	if radioModeLabel == "" {
		radioModeLabel = radioMode
	}
	radioVal := fmt.Sprintf("**Access:** `%s`", radioModeLabel)
	if cfg.RadioAutojoinChannelID != "" {
		radioVal += fmt.Sprintf("\n**Radio autojoin:** <#%s>", cfg.RadioAutojoinChannelID)
	} else {
		radioVal += "\n**autojoin:** Disabled"
	}
	voiceMasterVal := "Disabled"
	if cfg.VoiceMasterTriggerChannelID != "" {
		voiceMasterVal = fmt.Sprintf("<#%s>", cfg.VoiceMasterTriggerChannelID)
	}
	radioPlayerVal := "None"
	if cfg.RadioPlayerChannelID != "" {
		radioPlayerVal = fmt.Sprintf("<#%s>", cfg.RadioPlayerChannelID)
	}

	return &discordgo.MessageEmbed{
		Title:       "Server Settings",
		Description: "Select a section to edit it. Changes remain unchanged until you click **Save Changes**.",
		Color:       helpers.ColorBlurple,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Admin-only dashboard",
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "General", Value: fmt.Sprintf("**Prefix:** `%s`\n**Embed color:** `%s`", prefixVal, colorVal), Inline: false},
			{Name: "Moderation", Value: fmt.Sprintf("**Anti-MP3:** `%s`\n**Self-reaction prevention:** `%s`\n**Snipe limit:** `%d`", cfg.AntiMP3Mode, boolLabel(cfg.SelfReactDisabled), cfg.SnipeLimit), Inline: false},
			{Name: "Destinations", Value: fmt.Sprintf("**Mod logs:** %s\n**Starboard:** %s\n**Welcome:** %s\n**Goodbye:** %s", modLogVal, starboardVal, welcomeVal, goodbyeVal), Inline: false},
			{Name: "Roles", Value: fmt.Sprintf("**Autoroles:** %s\n**Jail:** %s\n**Mute roles:** %s", autoroleVal, jailVal, muteVal), Inline: false},
			{Name: "Radio & Voice", Value: fmt.Sprintf("%s\n**Dedicated player:** %s\n**Voice Master trigger Channel:** %s", radioVal, radioPlayerVal, voiceMasterVal), Inline: false},
			{Name: "Leveling & Filters", Value: fmt.Sprintf("**Level-up messages:** `%s`\n**XP multiplier:** `%.2fx`\n**Level-role stacking:** `%s`\n**Invite filter:** `%s`\n**Word-filter punishment:** `%s`", boolLabel(cfg.LevelUpMessagesEnabled), cfg.XPMultiplier, boolLabel(cfg.LevelRolesStack), boolLabel(cfg.InviteFilterEnabled), cfg.WordFilterPunishment), Inline: false},
		},
	}
}
