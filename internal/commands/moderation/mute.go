package moderation

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/roles"

	"github.com/bwmarrin/discordgo"
)

type MuteCmd struct{}

func (c *MuteCmd) Name() string        { return "mute" }
func (c *MuteCmd) Aliases() []string   { return []string{"tempmute", "m"} }
func (c *MuteCmd) Category() string    { return "Moderation" }
func (c *MuteCmd) Description() string { return "Applies the server mute role to a member." }
func (c *MuteCmd) Usage() string       { return "<user> [duration] [reason]" }
func (c *MuteCmd) Example() string     { return "@User 1d Breaking server rules" }
func (c *MuteCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *MuteCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	if targetMember == nil {
		targetMember, _ = ctx.GetMember(targetUser.ID)
	}
	if targetMember == nil {
		return ctx.SendError("The specified user is not a member of this server.")
	}

	muteRoleID, err := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingMuteRoleID)
	if err != nil || muteRoleID == "" {
		return ctx.SendError(fmt.Sprintf("No mute role configured. Use `%ssetup` or `%smuterole` first.", ctx.Prefix, ctx.Prefix))
	}
	if err := checkRoleAuthority(ctx, muteRoleID); err != nil {
		return ctx.SendError(err.Error())
	}

	var duration time.Duration
	reasonIdx := 0
	if targetViaArg {
		reasonIdx = 1
	}
	if len(ctx.Args) > reasonIdx {
		candidate := ctx.Args[reasonIdx]
		if d, parseErr := helpers.ParseDuration(candidate); parseErr == nil {
			duration = d
			reasonIdx++
		} else {
			switch strings.ToLower(candidate) {
			case "perm", "permanent", "forever":
				reasonIdx++
			default:
				if strings.ContainsAny(candidate, "0123456789") {
					return ctx.SendError(fmt.Sprintf("Invalid duration format `%s`. Use formats like 10m, 1h, 1d, or `perm` for a permanent mute.", candidate))
				}
			}
		}
	}

	reason := "No reason provided"
	if len(ctx.Args) > reasonIdx {
		reason = strings.Join(ctx.Args[reasonIdx:], " ")
	}

	durStr := ""
	if duration > 0 {
		durStr = helpers.FormatDuration(duration)
	}

	p := punishment{
		caseType:    "Mute",
		modlogLabel: "Muted",
		reason:      reason,
		duration:    durStr,
		dm: &punishmentDM{
			Title:    "Muted",
			Color:    dmColorPunishment,
			Duration: durStr,
			Dispute:  true,
		},
	}
	action := func() error {
		roleMgr := roles.DefaultRoleManager(ctx.Session, ctx.DB)
		return roleMgr.Assign(ctx.Context(), roles.AssignRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       muteRoleID,
			Duration:     duration,
			AssignedBy:   ctx.Message.Author.ID,
			Overwrite:    roles.OverwriteMute,
		})
	}

	desc := fmt.Sprintf("Permanently muted **%s**.", targetUser.String())
	if duration > 0 {
		desc = fmt.Sprintf("Successfully muted user **%s**. Duration: **%s**.", targetUser.String(), durStr)
	}

	return executePunishment(ctx, p, targetUser, targetMember, action, desc)
}

type UnmuteCmd struct{}

func (c *UnmuteCmd) Name() string        { return "unmute" }
func (c *UnmuteCmd) Aliases() []string   { return []string{} }
func (c *UnmuteCmd) Category() string    { return "Moderation" }
func (c *UnmuteCmd) Description() string { return "Unmute a member." }
func (c *UnmuteCmd) Usage() string       { return "<@user> [reason]" }
func (c *UnmuteCmd) Example() string     { return "@user appealed" }
func (c *UnmuteCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *UnmuteCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	muteRoleID, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingMuteRoleID)
	if muteRoleID == "" {
		return ctx.SendError("Mute role is not configured.")
	}
	if err := checkRoleAuthority(ctx, muteRoleID); err != nil {
		return ctx.SendError(err.Error())
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
		caseType:    "Unmute",
		modlogLabel: "Unmuted",
		reason:      reason,
		dm: &punishmentDM{
			Title: "Unmuted",
			Color: dmColorReversal,
		},
	}
	action := func() error {
		roleMgr := roles.DefaultRoleManager(ctx.Session, ctx.DB)
		return roleMgr.Revoke(ctx.Context(), roles.RevokeRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       muteRoleID,
		})
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Successfully removed mute role from user **%s**.", targetUser.String()))
}
