package moderation

import (
	"fmt"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type WarnCmd struct{}

func (c *WarnCmd) Name() string        { return "warn" }
func (c *WarnCmd) Aliases() []string   { return []string{} }
func (c *WarnCmd) Category() string    { return "Moderation" }
func (c *WarnCmd) Description() string { return "Warns a member in the server." }
func (c *WarnCmd) Usage() string       { return "<user> [reason]" }
func (c *WarnCmd) Example() string     { return "@User Inappropriate language" }
func (c *WarnCmd) Permissions() int64  { return discordgo.PermissionModerateMembers }

func (c *WarnCmd) Execute(ctx *bot.Context) error {
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
		caseType:    "Warn",
		modlogLabel: "Warned",
		reason:      reason,
		dm: &punishmentDM{
			Title:   "Warned",
			Color:   helpers.ColorWarn,
			Dispute: true,
		},
		notifyFirst: false,
	}

	return executePunishment(ctx, p, targetUser, targetMember, nil,
		fmt.Sprintf("Successfully warned user **%s**. Reason: %s", targetUser.String(), reason))
}

type WarningsCmd struct{}

func (c *WarningsCmd) Name() string        { return "warnings" }
func (c *WarningsCmd) Aliases() []string   { return []string{"warns"} }
func (c *WarningsCmd) Category() string    { return "Moderation" }
func (c *WarningsCmd) Description() string { return "List warnings for a user." }
func (c *WarningsCmd) Usage() string       { return "[user]" }
func (c *WarningsCmd) Example() string     { return "@User" }
func (c *WarningsCmd) Permissions() int64  { return discordgo.PermissionModerateMembers }

func (c *WarningsCmd) Execute(ctx *bot.Context) error {
	targetUser, _, err := ctx.TargetUserAndMember()
	if err != nil || targetUser == nil {
		targetUser = ctx.Message.Author
	}

	cases, err := ctx.DB.GetUserModCases(ctx.Message.GuildID, targetUser.ID)
	if err != nil {
		return ctx.SendError("Failed to query moderation cases.")
	}

	var warnCases []database.ModCase
	for _, mc := range cases {
		if strings.EqualFold(mc.Action, "warn") {
			warnCases = append(warnCases, mc)
		}
	}

	if len(warnCases) == 0 {
		return ctx.SendSuccess("No warnings found for **%s**.", targetUser.String())
	}

	const pageSize = 5
	totalPages := (len(warnCases) + pageSize - 1) / pageSize
	var embeds []*discordgo.MessageEmbed

	for page := 0; page < totalPages; page++ {
		start := page * pageSize
		end := start + pageSize
		if end > len(warnCases) {
			end = len(warnCases)
		}

		var lines []string
		for _, wc := range warnCases[start:end] {
			lines = append(lines, fmt.Sprintf("**Case #%d** • <t:%d:R> by <@%s>\n> Reason: %s",
				wc.CaseID, wc.CreatedAt.Unix(), wc.ModID, wc.Reason))
		}

		embed := &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Warnings for %s", targetUser.String()),
			Description: strings.Join(lines, "\n\n"),
			Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: helpers.UserAvatar(targetUser)},
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Page %d of %d • Total Warnings: %d", page+1, totalPages, len(warnCases)),
			},
		}
		embeds = append(embeds, embed)
	}

	return ctx.SendPaginatedEmbeds(embeds)
}
