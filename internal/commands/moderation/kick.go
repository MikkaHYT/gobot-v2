package moderation

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type KickCmd struct{}

func (c *KickCmd) Name() string        { return "kick" }
func (c *KickCmd) Aliases() []string   { return []string{} }
func (c *KickCmd) Category() string    { return "Moderation" }
func (c *KickCmd) Description() string { return "Kicks a member from the server." }
func (c *KickCmd) Usage() string       { return "<user> [reason]" }
func (c *KickCmd) Example() string     { return "@User Rule violation" }
func (c *KickCmd) Permissions() int64  { return discordgo.PermissionKickMembers }

func (c *KickCmd) Execute(ctx *bot.Context) error {
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
		caseType:    "Kick",
		modlogLabel: "Kicked",
		reason:      reason,
		dm: &punishmentDM{
			Title:   "Kicked",
			Color:   dmColorPunishment,
			Dispute: true,
		},
		notifyFirst: true,
	}
	action := func() error {
		auditReason := helpers.TruncateString(fmt.Sprintf("%s (Kicked by %s)", reason, ctx.Message.Author.String()), 450)
		if err := ctx.Session.GuildMemberDeleteWithReason(ctx.Message.GuildID, targetUser.ID, auditReason); err != nil {
			return fmt.Errorf("failed to kick user: %w", err)
		}
		return nil
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Successfully kicked user **%s**. Reason: %s", targetUser.String(), reason))
}
