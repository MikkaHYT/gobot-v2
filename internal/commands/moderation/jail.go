package moderation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/roles"

	"github.com/bwmarrin/discordgo"
)

type JailCmd struct{}

func (c *JailCmd) Name() string      { return "jail" }
func (c *JailCmd) Aliases() []string { return []string{"imprison", "jailed"} }
func (c *JailCmd) Category() string  { return "Moderation" }
func (c *JailCmd) Description() string {
	return "Jail a member"
}
func (c *JailCmd) Usage() string      { return "<@user|userID> [reason] | list" }
func (c *JailCmd) Example() string    { return "@Cloudyy Breaking rule #1" }
func (c *JailCmd) Permissions() int64 { return discordgo.PermissionManageRoles }

func (c *JailCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	if (len(ctx.Args) > 0 && (strings.EqualFold(ctx.Args[0], "list") || strings.EqualFold(ctx.Args[0], "show") || strings.EqualFold(ctx.Args[0], "all"))) ||
		(len(ctx.Args) == 0 && strings.EqualFold(ctx.InvokedCommand, "jailed")) {
		jailedUsers, err := ctx.DB.GetAllJailedUsers(ctx.Message.GuildID)
		if err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to query jailed members: %v", err))
		}
		if len(jailedUsers) == 0 {
			embed := &discordgo.MessageEmbed{
				Title:       "Jailed Members",
				Description: "No members are currently jailed in this server.",
				Color:       helpers.ColorDefault,
			}
			_, err := ctx.ReplyEmbed(embed)
			return err
		}

		var lines []string
		for _, j := range jailedUsers {
			dateStr := j.CreatedAt.Format("Jan 02, 2006")
			reason := j.Reason
			if reason == "" {
				reason = "No reason provided"
			}
			jailedBy := j.JailedBy
			if jailedBy == "" {
				jailedBy = "Unknown"
			}
			lines = append(lines, fmt.Sprintf("> <@%s> (`%s`)\n> **Jailed by:** <@%s> • **Date:** %s\n> **Reason:** %s", j.UserID, j.UserID, jailedBy, dateStr, reason))
		}

		const pageSize = 10
		totalPages := (len(lines) + pageSize - 1) / pageSize
		var embeds []*discordgo.MessageEmbed
		for page := range totalPages {
			start := page * pageSize
			end := start + pageSize
			if end > len(lines) {
				end = len(lines)
			}
			embeds = append(embeds, &discordgo.MessageEmbed{
				Title:       fmt.Sprintf("Jailed Members (%d)", len(lines)),
				Description: strings.Join(lines[start:end], "\n\n"),
				Color:       helpers.ColorDefault,
				Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Page %d of %d", page+1, totalPages)},
			})
		}
		return ctx.SendPaginatedEmbeds(embeds)
	}
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}
	if targetMember == nil {
		if m, errM := ctx.GetMember(targetUser.ID); errM == nil && m != nil {
			targetMember = m
		}
	}
	if targetMember == nil {
		return ctx.SendError("The specified user is not a member of this server.")
	}

	jailRoleID, err := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingJailRoleID)
	if err != nil || jailRoleID == "" {
		return ctx.SendError(fmt.Sprintf("No jail role set. Configure it with `%sjailrole <@role>` first.", ctx.Prefix))
	}
	if err := checkRoleAuthority(ctx, jailRoleID); err != nil {
		return ctx.SendError(err.Error())
	}

	existingJail, errJail := ctx.DB.GetJailedUserInfo(ctx.Message.GuildID, targetUser.ID)
	if errJail == nil && existingJail != nil {
		return ctx.SendError(fmt.Sprintf("**%s** is already jailed.", targetUser.String()))
	} else if errJail != nil && !errors.Is(errJail, database.ErrNotFound) {
		return fmt.Errorf("failed checking jail status: %w", errJail)
	}
	reason := "No reason provided"
	if targetViaArg {
		if len(ctx.Args) > 1 {
			reason = strings.Join(ctx.Args[1:], " ")
		}
	} else if len(ctx.Args) > 0 {
		reason = strings.Join(ctx.Args, " ")
	}

	jailChanID, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingJailChannelID)

	p := punishment{
		caseType:    "Jail",
		modlogLabel: "Jailed",
		reason:      reason,
		embedTitle:  "Member Jailed",
	}
	action := func() error {
		roleMgr := roles.DefaultRoleManager(ctx.Session, ctx.DB)
		_, err := roleMgr.Jail(ctx.Context(), roles.JailRequest{
			GuildID:       ctx.Message.GuildID,
			ActorUserID:   ctx.Message.Author.ID,
			TargetUserID:  targetUser.ID,
			JailRoleID:    jailRoleID,
			JailChannelID: jailChanID,
			Reason:        reason,
		})
		if errors.Is(err, database.ErrAlreadyJailed) {
			return ctx.SendError(fmt.Sprintf("**%s** is already jailed.", targetUser.String()))
		}
		return err
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("**%s** has been jailed.\n**Reason:** %s", targetUser.String(), reason))
}

type UnjailCmd struct{}

func (c *UnjailCmd) Name() string      { return "unjail" }
func (c *UnjailCmd) Aliases() []string { return []string{"free"} }
func (c *UnjailCmd) Category() string  { return "Moderation" }
func (c *UnjailCmd) Description() string {
	return "Release a member from jail and restore their original roles."
}
func (c *UnjailCmd) Usage() string      { return "<@user|userID>" }
func (c *UnjailCmd) Example() string    { return "@Cloudyy" }
func (c *UnjailCmd) Permissions() int64 { return discordgo.PermissionManageRoles }

func (c *UnjailCmd) Execute(ctx *bot.Context) error {
	if ctx.DB == nil {
		return fmt.Errorf("database connection unavailable")
	}

	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	jailRoleID, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingJailRoleID)

	roleMgr := roles.DefaultRoleManager(ctx.Session, ctx.DB)
	res, errRestore := roleMgr.Unjail(ctx.Context(), roles.UnjailRequest{
		GuildID:      ctx.Message.GuildID,
		ActorUserID:  ctx.Message.Author.ID,
		TargetUserID: targetUser.ID,
		JailRoleID:   jailRoleID,
		Force:        false,
	})
	if errRestore != nil {
		if errors.Is(errRestore, roles.ErrNotJailed) {
			return ctx.SendError(fmt.Sprintf("**%s** is not currently jailed.", targetUser.String()))
		}
		return ctx.SendError(fmt.Sprintf("Failed to prepare restoration for %s: %v", targetUser.String(), errRestore))
	}

	if len(res.Missed) > 0 {
		msg, errSend := ctx.ReplyEmbed(&discordgo.MessageEmbed{Description: bot.CleanErrorMessage(fmt.Sprintf(
			"**%s** could not be fully released. Not restored: %s.\nReply **FORCE** to this message within 2 minutes to complete anyway.",
			targetUser.String(), strings.Join(res.Missed, ", "))), Color: helpers.ColorDarkRed})
		if errSend == nil && msg != nil {
			registerUnjailForce(msg.ID, unjailForceRequest{
				guildID:     ctx.Message.GuildID,
				channelID:   ctx.Message.ChannelID,
				targetID:    targetUser.ID,
				moderatorID: ctx.Message.Author.ID,
				expiresAt:   time.Now().Add(2 * time.Minute),
			})
		}
		return nil
	}

	bot.Infof("[JAIL] Unjailed %s in guild %s (%d roles restored)", targetUser.ID, ctx.Message.GuildID, len(res.Restored))

	p := punishment{
		caseType:    "Unjail",
		modlogLabel: "Unjailed",
		reason:      "Released from jail",
		embedTitle:  "Member Unjailed",
	}
	action := func() error {
		return nil
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("**%s** has been released from jail and roles have been restored.", targetUser.String()))
}

type unjailForceRequest struct {
	guildID     string
	channelID   string
	targetID    string
	moderatorID string
	expiresAt   time.Time
}

var pendingUnjailForces sync.Map

func registerUnjailForce(messageID string, req unjailForceRequest) {
	pendingUnjailForces.Store(messageID, req)
	time.AfterFunc(2*time.Minute, func() {
		pendingUnjailForces.Delete(messageID)
	})
}

func ConsumeUnjailForce(s *discordgo.Session, m *discordgo.MessageCreate, db *database.DB) bool {
	if m == nil || m.ReferencedMessage == nil || db == nil || m.Author == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(m.Content), "force") {
		return false
	}
	v, ok := pendingUnjailForces.Load(m.ReferencedMessage.ID)
	if !ok {
		return false
	}
	req, ok := v.(unjailForceRequest)
	if !ok || m.Author.ID != req.moderatorID || m.ChannelID != req.channelID || time.Now().After(req.expiresAt) {
		return false
	}

	perms, errP := s.UserChannelPermissions(req.moderatorID, req.channelID)
	if errP != nil || perms&(discordgo.PermissionAdministrator|discordgo.PermissionManageGuild) == 0 {
		_, _ = s.ChannelMessageSend(req.channelID, fmt.Sprintf("<@%s> You no longer have permission to complete this unjail.", req.moderatorID))
		return true
	}
	pendingUnjailForces.Delete(m.ReferencedMessage.ID)

	targetUser, errU := s.User(req.targetID)
	if errU != nil || targetUser == nil {
		targetUser = &discordgo.User{ID: req.targetID, Username: "Unknown User"}
	}

	jailRoleID, _ := db.GetGuildSettingString(req.guildID, database.SettingJailRoleID)

	roleMgr := roles.DefaultRoleManager(s, db)
	unjailCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, errR := roleMgr.Unjail(unjailCtx, roles.UnjailRequest{
		GuildID:      req.guildID,
		ActorUserID:  req.moderatorID,
		TargetUserID: req.targetID,
		JailRoleID:   jailRoleID,
		Force:        true,
	})
	if errR != nil {
		bot.Errorf("[JAIL] Forced unjail for %s failed: %v", req.targetID, errR)
		registerUnjailForce(m.ReferencedMessage.ID, req)
		_, _ = s.ChannelMessageSend(req.channelID, fmt.Sprintf("Forced unjail for **%s** failed: %v. Reply **FORCE** again once role visibility is restored.", targetUser.String(), errR))
		return true
	}
	if len(res.Missed) > 0 {
		registerUnjailForce(m.ReferencedMessage.ID, req)
		bot.Warnf("[JAIL] Forced unjail for %s left %d role(s) unrestored; record retained", req.targetID, len(res.Missed))
		_, _ = s.ChannelMessageSend(req.channelID, fmt.Sprintf("Could not restore: %s.\nUpdate server permissions and reply FORCE to retry (expires <t:%d:R>).", strings.Join(res.Missed, ", "), req.expiresAt.Unix()))
		return true
	}

	summary := fmt.Sprintf("Forced unjail complete for **%s** (%d roles restored).", targetUser.String(), len(res.Restored))
	_, _ = s.ChannelMessageSend(req.channelID, summary)
	bot.Infof("[JAIL] Forced unjail %s in guild %s (%d roles restored, moderator %s)", req.targetID, req.guildID, len(res.Restored), req.moderatorID)
	return true
}
