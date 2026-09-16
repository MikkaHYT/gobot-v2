package moderation

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type BanCmd struct{}

func (c *BanCmd) Name() string        { return "ban" }
func (c *BanCmd) Aliases() []string   { return []string{} }
func (c *BanCmd) Category() string    { return "Moderation" }
func (c *BanCmd) Description() string { return "Bans a member from the server." }
func (c *BanCmd) Usage() string       { return "<user> [reason]" }
func (c *BanCmd) Example() string     { return "@Cloudyy Spamming in chat" }
func (c *BanCmd) Permissions() int64  { return discordgo.PermissionBanMembers }

func (c *BanCmd) Execute(ctx *bot.Context) error {
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
		caseType:    "Ban",
		modlogLabel: "Banned",
		reason:      reason,
		dm: &punishmentDM{
			Title:   "Banned",
			Color:   dmColorPunishment,
			Dispute: true,
		},
		notifyFirst: true,
	}
	action := func() error {
		auditReason := helpers.TruncateString(fmt.Sprintf("%s (Banned by %s)", reason, ctx.Message.Author.String()), 450)
		if err := ctx.Session.GuildBanCreateWithReason(ctx.Message.GuildID, targetUser.ID, auditReason, 0); err != nil {
			return fmt.Errorf("failed to ban user: %w", err)
		}
		return nil
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Successfully banned user **%s**. Reason: %s", targetUser.String(), reason))
}

type UnbanCmd struct{}

func (c *UnbanCmd) Name() string        { return "unban" }
func (c *UnbanCmd) Aliases() []string   { return []string{} }
func (c *UnbanCmd) Category() string    { return "Moderation" }
func (c *UnbanCmd) Description() string { return "Unbans a user by ID." }
func (c *UnbanCmd) Usage() string       { return "<user_id> [reason]" }
func (c *UnbanCmd) Example() string     { return "123456789012345678 Appealed" }
func (c *UnbanCmd) Permissions() int64  { return discordgo.PermissionBanMembers }

func (c *UnbanCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	targetID := ctx.ParseUserID(ctx.Args[0])
	if targetID == "" {
		return ctx.SendError("Invalid user ID.")
	}

	reason := "No reason provided."
	if len(ctx.Args) > 1 {
		reason = strings.Join(ctx.Args[1:], " ")
	}

	targetUser, err := ctx.Session.User(targetID)
	if err != nil {
		targetUser = &discordgo.User{ID: targetID, Username: "Unknown User"}
	}

	p := punishment{
		caseType:    "Unban",
		modlogLabel: "Unbanned",
		reason:      reason,
		dm: &punishmentDM{
			Title: "Unbanned",
			Color: dmColorReversal,
		},
	}
	action := func() error {
		if err := ctx.Session.GuildBanDelete(ctx.Message.GuildID, targetID); err != nil {
			return fmt.Errorf("failed to unban user: %w", err)
		}
		return nil
	}
	return executePunishment(ctx, p, targetUser, nil, action,
		fmt.Sprintf("Successfully unbanned user **%s**. Reason: %s", targetUser.String(), reason))
}
