package voice

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/database"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

var Commands = []commands.Command{
	&VoiceCmd{},
}

type VoiceCmd struct{}

func (c *VoiceCmd) Name() string        { return "voice" }
func (c *VoiceCmd) Aliases() []string   { return []string{"vc", "vm"} }
func (c *VoiceCmd) Category() string    { return "Voice & VC" }
func (c *VoiceCmd) Description() string { return "Manage temporary voice channels." }
func (c *VoiceCmd) Usage() string       { return "<subcommand> [args]" }
func (c *VoiceCmd) Example() string     { return "lock" }

func (c *VoiceCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "setup", Description: "Setup the VoiceMaster system (Admin)", Usage: "[Category | Channel | auto | disable]", Example: "auto"},
		{Name: "lock", Description: "Lock your voice channel", Usage: "", Example: ""},
		{Name: "unlock", Description: "Unlock your voice channel", Usage: "", Example: ""},
		{Name: "permit", Description: "Allow a user to join your locked channel", Usage: "<@user|userID>", Example: "@Cloudyy"},
		{Name: "reject", Description: "Deny a user access and move them out", Usage: "<@user|userID>", Example: "@Cloudyy"},
		{Name: "limit", Description: "Set maximum member limit (0-99)", Usage: "<0-99>", Example: "5"},
		{Name: "name", Description: "Change your voice channel name", Usage: "<new_name>", Example: "Cloudyy's Lounge"},
		{Name: "claim", Description: "Claim ownership if owner left the room", Usage: "", Example: ""},
		{Name: "hide", Description: "Hide your voice channel from everyone", Usage: "", Example: ""},
		{Name: "reveal", Description: "Make your voice channel visible to everyone", Usage: "", Example: ""},
		{Name: "kick", Description: "Kick a user out of your channel", Usage: "<@user|userID>", Example: "@Cloudyy"},
		{Name: "transfer", Description: "Transfer channel ownership to another member", Usage: "<@user|userID>", Example: "@Cloudyy"},
		{Name: "reset", Description: "Reset channel permissions to default", Usage: "", Example: ""},
		{Name: "info", Description: "View details about your voice channel", Usage: "", Example: ""},
	}
}

func (c *VoiceCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if len(ctx.Args) == 0 {
		return c.handleHelp(ctx)
	}

	sub := strings.ToLower(ctx.Args[0])
	var err error
	switch sub {
	case "setup":
		err = c.handleSetup(ctx)
	case "lock":
		err = c.handleLock(ctx)
	case "unlock":
		err = c.handleUnlock(ctx)
	case "permit", "allow":
		err = c.handlePermit(ctx)
	case "reject", "deny":
		err = c.handleReject(ctx)
	case "limit":
		err = c.handleLimit(ctx)
	case "name":
		err = c.handleName(ctx)
	case "claim":
		err = c.handleClaim(ctx)
	case "hide":
		err = c.handleHide(ctx)
	case "reveal", "show":
		err = c.handleReveal(ctx)
	case "kick":
		err = c.handleKick(ctx)
	case "transfer":
		err = c.handleTransfer(ctx)
	case "reset":
		err = c.handleReset(ctx)
	case "info":
		err = c.handleInfo(ctx)
	default:
		return c.handleHelp(ctx)
	}
	if err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}

func (c *VoiceCmd) handleHelp(ctx *bot.Context) error {
	embed := &discordgo.MessageEmbed{
		Title:       "Voice Commands",
		Description: fmt.Sprintf("Use `%svoice <subcommand>` to manage your voice channel.", ctx.Prefix),
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Setup", Value: fmt.Sprintf("`%svoice setup [Category] | <Trigger Channel>`", ctx.Prefix), Inline: false},
			{Name: "Privacy", Value: "`lock` • `unlock` • `hide` • `reveal`", Inline: true},
			{Name: "Access", Value: "`permit @user` • `reject @user` • `kick @user`", Inline: true},
			{Name: "Control", Value: "`claim` • `transfer @user` • `name <text>` • `limit <num>` • `reset` • `info`", Inline: false},
		},
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func (c *VoiceCmd) handleSetup(ctx *bot.Context) error {
	if !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
		return nil
	}

	var catName, chanName string
	if len(ctx.Args) > 1 {
		rawArgs := strings.TrimSpace(strings.Join(ctx.Args[1:], " "))
		if strings.EqualFold(rawArgs, "disable") || strings.EqualFold(rawArgs, "clear") || strings.EqualFold(rawArgs, "reset") || strings.EqualFold(rawArgs, "off") || strings.EqualFold(rawArgs, "none") {
			confirmed, err := ctx.PromptConfirmation("Are you sure you want to disable and reset VoiceMaster dynamic voice channels for this server?")
			if err != nil || !confirmed {
				return err
			}
			_ = ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingVoiceMasterTriggerChannelID, "")
			_ = ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingVoiceMasterCategoryID, "")
			return ctx.SendSuccess("Disabled VoiceMaster dynamic voice channels.")
		}
		if !strings.EqualFold(rawArgs, "auto") && !strings.EqualFold(rawArgs, "default") {
			parts := strings.Split(rawArgs, "|")
			if len(parts) >= 2 {
				catName = strings.TrimSpace(parts[0])
				chanName = strings.TrimSpace(parts[1])
			} else {
				chanName = strings.TrimSpace(parts[0])
			}
		}
	}

	if catName == "" {
		catName = "Voice Channels"
	}
	if chanName == "" {
		chanName = "Join to Create"
	}

	var categoryID string
	var categoryName string
	channels, err := ctx.Session.GuildChannels(ctx.Message.GuildID)
	if err == nil {
		for _, ch := range channels {
			if ch.Type == discordgo.ChannelTypeGuildCategory {
				if strings.EqualFold(ch.Name, catName) || ch.ID == catName {
					categoryID = ch.ID
					categoryName = ch.Name
					break
				}
			}
		}
	}

	if categoryID == "" {
		category, err := ctx.Session.GuildChannelCreateComplex(ctx.Message.GuildID, discordgo.GuildChannelCreateData{
			Name: catName,
			Type: discordgo.ChannelTypeGuildCategory,
		})
		if err != nil {
			return fmt.Errorf("failed to create category: %w", err)
		}
		categoryID = category.ID
		categoryName = category.Name
	}

	triggerChannel, err := ctx.Session.GuildChannelCreateComplex(ctx.Message.GuildID, discordgo.GuildChannelCreateData{
		Name:     chanName,
		Type:     discordgo.ChannelTypeGuildVoice,
		ParentID: categoryID,
	})
	if err != nil {
		return fmt.Errorf("failed to create voice channel: %w", err)
	}

	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingVoiceMasterTriggerChannelID, triggerChannel.ID); err != nil {
		return fmt.Errorf("failed to save trigger channel to database: %w", err)
	}
	if err := ctx.DB.UpdateGuildSetting(ctx.Message.GuildID, database.SettingVoiceMasterCategoryID, categoryID); err != nil {
		return fmt.Errorf("failed to save voice category to database: %w", err)
	}

	embed := &discordgo.MessageEmbed{
		Title:       "VoiceMaster Configured",
		Description: fmt.Sprintf("Set up VoiceMaster trigger channel <#%s> under category **%s**.\nMembers joining this channel will automatically get their own temporary voice channel.", triggerChannel.ID, categoryName),
		Color:       helpers.ColorSuccess,
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *VoiceCmd) getOwnedChannel(ctx *bot.Context) (string, error) {
	channelID, found := GlobalManager.GetUserOwnedChannel(ctx.Message.GuildID, ctx.Message.Author.ID)
	if !found || channelID == "" {
		return "", fmt.Errorf("you do not currently own an active voice channel in this server")
	}
	return channelID, nil
}

func fetchLiveVoiceChannel(s *discordgo.Session, guildID, userID string) (string, error) {
	body, err := s.RequestWithBucketID(
		"GET",
		discordgo.EndpointGuildMemberVoiceState(guildID, userID),
		nil,
		discordgo.EndpointGuildMemberVoiceState(guildID, userID),
	)
	if err != nil {
		return "", err
	}
	var vs struct {
		ChannelID string `json:"channel_id"`
	}
	if err := json.Unmarshal(body, &vs); err != nil {
		return "", err
	}
	return vs.ChannelID, nil
}

func (c *VoiceCmd) isUserInChannelLive(ctx *bot.Context, userID, channelID string) bool {
	liveChannel, err := fetchLiveVoiceChannel(ctx.Session, ctx.Message.GuildID, userID)
	if err != nil {
		return false
	}
	return liveChannel == channelID
}

func (c *VoiceCmd) handleLock(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	err = helpers.UpdateEveryoneRoleOverwrite(ctx.Session, ctx.Message.GuildID, channelID, func(allow, deny int64) (int64, int64) {
		return allow &^ discordgo.PermissionVoiceConnect, deny | discordgo.PermissionVoiceConnect
	})
	if err != nil {
		return fmt.Errorf("failed to lock channel: %w", err)
	}

	return ctx.SendSuccess("Voice channel locked.")
}

func (c *VoiceCmd) handleUnlock(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	err = helpers.UpdateEveryoneRoleOverwrite(ctx.Session, ctx.Message.GuildID, channelID, func(allow, deny int64) (int64, int64) {
		return allow | discordgo.PermissionVoiceConnect, deny &^ discordgo.PermissionVoiceConnect
	})
	if err != nil {
		return fmt.Errorf("failed to unlock channel: %w", err)
	}

	return ctx.SendSuccess("Voice channel unlocked.")
}

func (c *VoiceCmd) handleHide(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	err = helpers.HideChannel(ctx.Session, ctx.Message.GuildID, channelID)
	if err != nil {
		return fmt.Errorf("failed to hide channel: %w", err)
	}

	return ctx.SendSuccess("Voice channel hidden.")
}

func (c *VoiceCmd) handleReveal(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	err = helpers.UnhideChannel(ctx.Session, ctx.Message.GuildID, channelID)
	if err != nil {
		return fmt.Errorf("failed to reveal channel: %w", err)
	}

	return ctx.SendSuccess("Voice channel visible.")
}

func (c *VoiceCmd) handlePermit(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	targetUser, _, targetViaArg, err := ctx.TargetUserAndMemberArg(1)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		return fmt.Errorf("please mention a valid member to permit")
	}

	err = ctx.Session.ChannelPermissionSet(channelID, targetUser.ID, discordgo.PermissionOverwriteTypeMember, discordgo.PermissionVoiceConnect|discordgo.PermissionViewChannel, 0)
	if err != nil {
		return fmt.Errorf("failed to update permissions: %w", err)
	}

	return ctx.SendSuccess("Permitted **%s** to join channel.", targetUser.Username)
}

func (c *VoiceCmd) handleReject(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	targetUser, _, targetViaArg, err := ctx.TargetUserAndMemberArg(1)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		return fmt.Errorf("please mention a valid member to reject")
	}

	if err := ctx.Session.ChannelPermissionSet(channelID, targetUser.ID, discordgo.PermissionOverwriteTypeMember, 0, discordgo.PermissionVoiceConnect); err != nil {
		return fmt.Errorf("failed to update channel permissions: %w", err)
	}

	if c.isUserInChannelLive(ctx, targetUser.ID, channelID) {
		if err := ctx.Session.GuildMemberMove(ctx.Message.GuildID, targetUser.ID, nil); err != nil {
			bot.Warnf("[VOICE] Failed to disconnect rejected member %s: %v", targetUser.ID, err)
		}
	}

	return ctx.SendSuccess("Rejected access for **%s**.", targetUser.Username)
}

func (c *VoiceCmd) handleLimit(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	if !ctx.RequireSubArgs(2, "voice limit <0-99>") {
		return nil
	}

	limit, parseErr := strconv.Atoi(ctx.Args[1])
	if parseErr != nil || limit < 0 || limit > 99 {
		return fmt.Errorf("please specify a valid member limit between 0 and 99")
	}

	_, err = ctx.Session.ChannelEdit(channelID, &discordgo.ChannelEdit{
		UserLimit: limit,
	})
	if err != nil {
		return fmt.Errorf("failed to update user limit: %w", err)
	}

	GlobalManager.SetUserSettings(ctx.Message.GuildID, ctx.Message.Author.ID, "", limit)

	return ctx.SendSuccess("Member limit set to **%d**.", limit)
}

func (c *VoiceCmd) handleName(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	if !ctx.RequireSubArgs(2, "voice name <new channel name>") {
		return nil
	}

	newName := strings.Join(ctx.Args[1:], " ")
	if len(newName) > 100 {
		return fmt.Errorf("channel name cannot exceed 100 characters")
	}

	_, err = ctx.Session.ChannelEdit(channelID, &discordgo.ChannelEdit{
		Name: newName,
	})
	if err != nil {
		return fmt.Errorf("failed to update channel name: %w", err)
	}

	GlobalManager.SetUserSettings(ctx.Message.GuildID, ctx.Message.Author.ID, newName, -1)

	return ctx.SendSuccess("Channel renamed to **%s**.", newName)
}

func (c *VoiceCmd) handleClaim(ctx *bot.Context) error {
	guild, err := ctx.Guild()
	if err != nil {
		return err
	}

	var userChannelID string
	for _, vs := range guild.VoiceStates {
		if vs.UserID == ctx.Message.Author.ID {
			userChannelID = vs.ChannelID
			break
		}
	}

	if userChannelID == "" || !GlobalManager.IsActiveChannel(userChannelID) {
		return fmt.Errorf("you must be inside an active voice channel to claim it")
	}

	currentOwnerID, exists := GlobalManager.GetChannelOwner(userChannelID)
	if exists && currentOwnerID != "" {
		ownerStillHere := false
		for _, vs := range guild.VoiceStates {
			if vs.ChannelID == userChannelID && vs.UserID == currentOwnerID {
				ownerStillHere = true
				break
			}
		}
		if ownerStillHere {
			return fmt.Errorf("the owner (<@%s>) is still present in the channel", currentOwnerID)
		}
	}

	if err := GlobalManager.TransferOwnership(userChannelID, ctx.Message.Author.ID); err != nil {
		return fmt.Errorf("failed to claim channel: %w", err)
	}

	return ctx.SendSuccess("Claimed ownership of <#%s>.", userChannelID)
}

func (c *VoiceCmd) handleKick(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	targetUser, _, targetViaArg, err := ctx.TargetUserAndMemberArg(1)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		return fmt.Errorf("please mention a valid member to kick")
	}

	if !c.isUserInChannelLive(ctx, targetUser.ID, channelID) {
		return fmt.Errorf("that user is not currently in your channel")
	}

	err = ctx.Session.GuildMemberMove(ctx.Message.GuildID, targetUser.ID, nil)
	if err != nil {
		return fmt.Errorf("failed to kick user: %w", err)
	}

	return ctx.SendSuccess("Kicked **%s** from channel.", targetUser.Username)
}

func (c *VoiceCmd) handleTransfer(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	targetUser, _, targetViaArg, err := ctx.TargetUserAndMemberArg(1)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		return fmt.Errorf("please mention a valid member to transfer ownership to")
	}

	if targetUser.Bot {
		return fmt.Errorf("cannot transfer voice channel ownership to a bot")
	}

	if targetUser.ID == ctx.Message.Author.ID {
		return fmt.Errorf("you already own this channel")
	}

	if err := GlobalManager.TransferOwnership(channelID, targetUser.ID); err != nil {
		return fmt.Errorf("failed to transfer channel: %w", err)
	}

	return ctx.SendSuccess("Transferred channel ownership to **%s**.", targetUser.Username)
}

func (c *VoiceCmd) handleReset(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	ch, err := ctx.ResolveChannel(channelID)
	if err != nil {
		return err
	}

	var punishmentRoleIDs map[string]bool
	cfg, errCfg := ctx.DB.GetGuildConfig(ctx.Message.GuildID)
	if errCfg != nil {
		return fmt.Errorf("failed to retrieve guild configuration for voice reset")
	}
	if cfg != nil {
		punishmentRoleIDs = make(map[string]bool)
		if cfg.JailRoleID != "" {
			punishmentRoleIDs[cfg.JailRoleID] = true
		}
		if cfg.MuteRoleID != "" {
			punishmentRoleIDs[cfg.MuteRoleID] = true
		}
	}

	for _, ow := range ch.PermissionOverwrites {
		if punishmentRoleIDs != nil && punishmentRoleIDs[ow.ID] {
			continue
		}
		_ = ctx.Session.ChannelPermissionDelete(channelID, ow.ID)
	}

	return ctx.SendSuccess("Channel permissions reset to default.")
}

func (c *VoiceCmd) handleInfo(ctx *bot.Context) error {
	channelID, err := c.getOwnedChannel(ctx)
	if err != nil {
		return err
	}

	ch, err := ctx.Session.Channel(channelID)
	if err != nil {
		return err
	}

	ownerID, _ := GlobalManager.GetChannelOwner(channelID)

	visibility := "Visible"
	if helpers.IsChannelHidden(ch, ctx.Message.GuildID) {
		visibility = "Hidden"
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("Channel Info: %s", ch.Name),
		Color: helpers.ColorDefault,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Owner", Value: fmt.Sprintf("<@%s>", ownerID), Inline: true},
			{Name: "Channel ID", Value: fmt.Sprintf("`%s`", ch.ID), Inline: true},
			{Name: "User Limit", Value: fmt.Sprintf("%d", ch.UserLimit), Inline: true},
			{Name: "Visibility", Value: visibility, Inline: true},
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}
