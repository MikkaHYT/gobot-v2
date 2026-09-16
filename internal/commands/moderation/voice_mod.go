package moderation

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

func getUserVoiceChannel(guild *discordgo.Guild, userID string) string {
	if guild == nil {
		return ""
	}
	for _, vs := range guild.VoiceStates {
		if vs.UserID == userID {
			return vs.ChannelID
		}
	}
	return ""
}

type VCKickCmd struct{}

func (c *VCKickCmd) Name() string        { return "vckick" }
func (c *VCKickCmd) Aliases() []string   { return []string{} }
func (c *VCKickCmd) Category() string    { return "Moderation" }
func (c *VCKickCmd) Description() string { return "Disconnects a member from voice." }
func (c *VCKickCmd) Usage() string       { return "<user> [reason]" }
func (c *VCKickCmd) Example() string     { return "@User Mic spam" }
func (c *VCKickCmd) Permissions() int64  { return discordgo.PermissionVoiceMoveMembers }

func (c *VCKickCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	guild, err := ctx.Guild()
	if err != nil {
		return ctx.SendError("Failed to fetch guild state.")
	}

	currentChannelID := getUserVoiceChannel(guild, targetUser.ID)
	if currentChannelID == "" {
		return ctx.SendError(fmt.Sprintf("**%s** is not currently in a voice channel.", targetUser.String()))
	}

	reason := "No reason provided"
	if targetViaArg {
		if len(ctx.Args) > 1 {
			reason = strings.Join(ctx.Args[1:], " ")
		}
	} else if len(ctx.Args) > 0 {
		reason = strings.Join(ctx.Args, " ")
	}

	p := punishment{
		caseType:    "VoiceKick",
		modlogLabel: "Voice Kicked",
		reason:      reason,
		dm: &punishmentDM{
			Title:   "Voice Kicked",
			Color:   helpers.ColorWarn,
			Dispute: true,
		},
		notifyFirst: false,
	}

	action := func() error {
		return ctx.Session.GuildMemberMove(ctx.Message.GuildID, targetUser.ID, nil)
	}

	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Disconnected **%s** from voice. Reason: %s", targetUser.String(), reason))
}

type VCMuteCmd struct{}

func (c *VCMuteCmd) Name() string        { return "vcmute" }
func (c *VCMuteCmd) Aliases() []string   { return []string{} }
func (c *VCMuteCmd) Category() string    { return "Moderation" }
func (c *VCMuteCmd) Description() string { return "Mutes a member in voice." }
func (c *VCMuteCmd) Usage() string       { return "<user> [reason]" }
func (c *VCMuteCmd) Example() string     { return "@User Loud audio" }
func (c *VCMuteCmd) Permissions() int64  { return discordgo.PermissionVoiceMuteMembers }

func (c *VCMuteCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	reason := "No reason provided"
	if targetViaArg {
		if len(ctx.Args) > 1 {
			reason = strings.Join(ctx.Args[1:], " ")
		}
	} else if len(ctx.Args) > 0 {
		reason = strings.Join(ctx.Args, " ")
	}

	p := punishment{
		caseType:    "VoiceMute",
		modlogLabel: "Voice Muted",
		reason:      reason,
		dm: &punishmentDM{
			Title:   "Voice Muted",
			Color:   helpers.ColorDarkRed,
			Dispute: true,
		},
		notifyFirst: false,
	}

	action := func() error {
		return ctx.Session.GuildMemberMute(ctx.Message.GuildID, targetUser.ID, true)
	}

	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Voice-muted **%s**. Reason: %s", targetUser.String(), reason))
}

type VCUnmuteCmd struct{}

func (c *VCUnmuteCmd) Name() string        { return "vcunmute" }
func (c *VCUnmuteCmd) Aliases() []string   { return []string{} }
func (c *VCUnmuteCmd) Category() string    { return "Moderation" }
func (c *VCUnmuteCmd) Description() string { return "Unmutes a member in voice." }
func (c *VCUnmuteCmd) Usage() string       { return "<user>" }
func (c *VCUnmuteCmd) Example() string     { return "@User" }
func (c *VCUnmuteCmd) Permissions() int64  { return discordgo.PermissionVoiceMuteMembers }

func (c *VCUnmuteCmd) Execute(ctx *bot.Context) error {
	targetUser, targetMember, _, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil {
		_, err := ctx.SendUsage(c)
		return err
	}

	if err := checkTargetAuthority(ctx, targetUser, targetMember); err != nil {
		return ctx.SendError(err.Error())
	}

	if err := ctx.Session.GuildMemberMute(ctx.Message.GuildID, targetUser.ID, false); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to unmute member: %v", err))
	}

	_ = ctx.ReactSuccess()
	return ctx.SendSuccess("Unmuted **%s** in voice.", targetUser.String())
}

type VCDeafenCmd struct{}

func (c *VCDeafenCmd) Name() string        { return "vcdeafen" }
func (c *VCDeafenCmd) Aliases() []string   { return []string{} }
func (c *VCDeafenCmd) Category() string    { return "Moderation" }
func (c *VCDeafenCmd) Description() string { return "Deafens a member in voice." }
func (c *VCDeafenCmd) Usage() string       { return "<user> [reason]" }
func (c *VCDeafenCmd) Example() string     { return "@User Disobeying rules" }
func (c *VCDeafenCmd) Permissions() int64  { return discordgo.PermissionVoiceDeafenMembers }

func (c *VCDeafenCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	reason := "No reason provided"
	if targetViaArg {
		if len(ctx.Args) > 1 {
			reason = strings.Join(ctx.Args[1:], " ")
		}
	} else if len(ctx.Args) > 0 {
		reason = strings.Join(ctx.Args, " ")
	}

	p := punishment{
		caseType:    "VoiceDeafen",
		modlogLabel: "Voice Deafened",
		reason:      reason,
		dm: &punishmentDM{
			Title:   "Voice Deafened",
			Color:   helpers.ColorDarkRed,
			Dispute: true,
		},
		notifyFirst: false,
	}

	action := func() error {
		return ctx.Session.GuildMemberDeafen(ctx.Message.GuildID, targetUser.ID, true)
	}

	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Voice-deafened **%s**. Reason: %s", targetUser.String(), reason))
}

type VCUndeafenCmd struct{}

func (c *VCUndeafenCmd) Name() string        { return "vcundeafen" }
func (c *VCUndeafenCmd) Aliases() []string   { return []string{} }
func (c *VCUndeafenCmd) Category() string    { return "Moderation" }
func (c *VCUndeafenCmd) Description() string { return "Undeafens a member in voice." }
func (c *VCUndeafenCmd) Usage() string       { return "<user>" }
func (c *VCUndeafenCmd) Example() string     { return "@User" }
func (c *VCUndeafenCmd) Permissions() int64  { return discordgo.PermissionVoiceDeafenMembers }

func (c *VCUndeafenCmd) Execute(ctx *bot.Context) error {
	targetUser, targetMember, _, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil {
		_, err := ctx.SendUsage(c)
		return err
	}

	if err := checkTargetAuthority(ctx, targetUser, targetMember); err != nil {
		return ctx.SendError(err.Error())
	}

	if err := ctx.Session.GuildMemberDeafen(ctx.Message.GuildID, targetUser.ID, false); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to undeafen member: %v", err))
	}

	_ = ctx.ReactSuccess()
	return ctx.SendSuccess("Undeafened **%s** in voice.", targetUser.String())
}

type VCMoveCmd struct{}

func (c *VCMoveCmd) Name() string        { return "vcmove" }
func (c *VCMoveCmd) Aliases() []string   { return []string{} }
func (c *VCMoveCmd) Category() string    { return "Moderation" }
func (c *VCMoveCmd) Description() string { return "Moves a member or all members to another voice channel." }
func (c *VCMoveCmd) Usage() string       { return "<user|all> <#channel>" }
func (c *VCMoveCmd) Example() string     { return "@User #General" }
func (c *VCMoveCmd) Permissions() int64  { return discordgo.PermissionVoiceMoveMembers }

func (c *VCMoveCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	targetArg := ctx.Args[0]
	destArg := ctx.Args[1]

	destChannel, err := ctx.ResolveChannel(destArg)
	if err != nil || destChannel == nil || (destChannel.Type != discordgo.ChannelTypeGuildVoice && destChannel.Type != discordgo.ChannelTypeGuildStageVoice) {
		if ch0, err0 := ctx.ResolveChannel(targetArg); err0 == nil && ch0 != nil && (ch0.Type == discordgo.ChannelTypeGuildVoice || ch0.Type == discordgo.ChannelTypeGuildStageVoice) {
			destChannel = ch0
			targetArg = destArg
		} else if err != nil || destChannel == nil {
			return ctx.SendError("Target voice channel not found.")
		}
	}

	if destChannel.GuildID != ctx.Message.GuildID {
		return ctx.SendError("Target channel does not belong to this server.")
	}
	if destChannel.Type != discordgo.ChannelTypeGuildVoice && destChannel.Type != discordgo.ChannelTypeGuildStageVoice {
		return ctx.SendError("Target channel must be a voice channel.")
	}

	guild, err := ctx.Guild()
	if err != nil {
		return ctx.SendError("Failed to fetch guild state.")
	}

	if strings.EqualFold(targetArg, "all") {
		callerChannelID := getUserVoiceChannel(guild, ctx.Message.Author.ID)
		if callerChannelID == "" {
			return ctx.SendError("You must be in a voice channel to move all members.")
		}
		if callerChannelID == destChannel.ID {
			return ctx.SendError("Members are already in the destination channel.")
		}

		moved := 0
		for _, vs := range guild.VoiceStates {
			if vs.ChannelID == callerChannelID {
				if moveErr := ctx.Session.GuildMemberMove(ctx.Message.GuildID, vs.UserID, &destChannel.ID); moveErr == nil {
					moved++
				}
			}
		}

		return ctx.SendSuccess("Moved **%d** member(s) to <#%s>.", moved, destChannel.ID)
	}

	targetUser, targetMember, errUser := ctx.ResolveUserAndMember(targetArg)
	if errUser != nil || targetUser == nil {
		return ctx.SendError("Could not find a valid user to move.")
	}

	if err := checkTargetAuthority(ctx, targetUser, targetMember); err != nil {
		return ctx.SendError(err.Error())
	}

	currentChannelID := getUserVoiceChannel(guild, targetUser.ID)
	if currentChannelID == "" {
		return ctx.SendError(fmt.Sprintf("**%s** is not currently in a voice channel.", targetUser.String()))
	}
	if currentChannelID == destChannel.ID {
		return ctx.SendError(fmt.Sprintf("**%s** is already in that voice channel.", targetUser.String()))
	}

	if err := ctx.Session.GuildMemberMove(ctx.Message.GuildID, targetUser.ID, &destChannel.ID); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to move member: %v", err))
	}

	return ctx.SendSuccess("Moved **%s** to <#%s>.", targetUser.String(), destChannel.ID)
}
