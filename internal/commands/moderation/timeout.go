package moderation

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type TimeoutCmd struct{}

func (c *TimeoutCmd) Name() string        { return "timeout" }
func (c *TimeoutCmd) Aliases() []string   { return []string{"to", "time-out", "timedout"} }
func (c *TimeoutCmd) Category() string    { return "Moderation" }
func (c *TimeoutCmd) Description() string { return "Timeouts a member for a specified duration." }
func (c *TimeoutCmd) Usage() string       { return "<user> <duration> [reason]" }
func (c *TimeoutCmd) Example() string     { return "@User 1d2h Spamming" }
func (c *TimeoutCmd) Permissions() int64  { return discordgo.PermissionModerateMembers }

func (c *TimeoutCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	durationIdx := 0
	if targetViaArg {
		durationIdx = 1
	}
	if len(ctx.Args) <= durationIdx {
		_, err := ctx.SendUsage(c)
		return err
	}

	durationStr := ctx.Args[durationIdx]
	duration, err := helpers.ParseDuration(durationStr)
	if err != nil {
		return fmt.Errorf("invalid duration format `%s`. Use formats like 10m, 1h, 1d, 1w", durationStr)
	}
	if duration <= 0 {
		return fmt.Errorf("timeout duration must be greater than 0")
	}
	if duration > 28*24*time.Hour {
		return fmt.Errorf("timeout duration cannot exceed 28 days")
	}

	reason := "No reason provided"
	if len(ctx.Args) > durationIdx+1 {
		reason = strings.Join(ctx.Args[durationIdx+1:], " ")
	}
	reason = helpers.TruncateString(reason, 450)

	until := time.Now().Add(duration)

	p := punishment{
		caseType:    "Timeout",
		modlogLabel: "Timed Out",
		reason:      reason,
		duration:    helpers.FormatDuration(duration),
		dm: &punishmentDM{
			Title:    "Timed Out",
			Color:    dmColorTimeout,
			Duration: helpers.FormatDuration(duration),
			Dispute:  true,
		},
	}
	action := func() error {
		if err := ctx.Session.GuildMemberTimeout(ctx.Message.GuildID, targetUser.ID, &until); err != nil {
			return fmt.Errorf("failed to timeout user: %w", err)
		}
		return nil
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Successfully timed out user **%s** for %s. Reason: %s", targetUser.String(), helpers.FormatDuration(duration), reason))
}

type UntimeoutCmd struct{}

func (c *UntimeoutCmd) Name() string        { return "untimeout" }
func (c *UntimeoutCmd) Aliases() []string   { return []string{} }
func (c *UntimeoutCmd) Category() string    { return "Moderation" }
func (c *UntimeoutCmd) Description() string { return "Removes a timeout from a user." }
func (c *UntimeoutCmd) Usage() string       { return "<@user/id>" }
func (c *UntimeoutCmd) Example() string     { return "@User" }
func (c *UntimeoutCmd) Permissions() int64  { return discordgo.PermissionModerateMembers }

func (c *UntimeoutCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	p := punishment{
		caseType:    "Untimeout",
		modlogLabel: "Untimed Out",
		reason:      "Timeout removed",
		dm: &punishmentDM{
			Title: "Untimed Out",
			Color: dmColorReversal,
		},
	}
	action := func() error {
		if err := ctx.Session.GuildMemberTimeout(ctx.Message.GuildID, targetUser.ID, nil); err != nil {
			return fmt.Errorf("failed to remove timeout from user: %w", err)
		}
		return nil
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Successfully removed timeout from user **%s**.", targetUser.String()))
}
