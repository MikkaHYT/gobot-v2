package moderation

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/listeners"
	"gobot/internal/modlog"
	"gobot/internal/roles"

	"github.com/bwmarrin/discordgo"
)

type CaseCmd struct{}

func (c *CaseCmd) Name() string        { return "case" }
func (c *CaseCmd) Aliases() []string   { return []string{} }
func (c *CaseCmd) Category() string    { return "Moderation" }
func (c *CaseCmd) Description() string { return "View information for a specific mod case ID." }
func (c *CaseCmd) Usage() string       { return "<caseID> | delete <caseID>" }
func (c *CaseCmd) Example() string     { return "14" }
func (c *CaseCmd) Permissions() int64  { return discordgo.PermissionManageMessages }

func (c *CaseCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}
	sub := strings.ToLower(ctx.Args[0])
	if sub == "delete" || sub == "del" || sub == "rm" || sub == "remove" {
		if !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
			return nil
		}
		if len(ctx.Args) < 2 {
			return ctx.SendError("Please specify a valid numeric case ID to delete.")
		}
		caseID, err := strconv.ParseInt(ctx.Args[1], 10, 64)
		if err != nil || caseID <= 0 {
			return ctx.SendError("Please specify a valid numeric case ID to delete.")
		}
		modCase, _ := ctx.DB.GetModCase(ctx.Message.GuildID, caseID)
		if err := ctx.DB.DeleteModCase(ctx.Message.GuildID, caseID); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to delete mod case: %v", err))
		}
		if modCase != nil {
			targetUser, _ := ctx.Session.User(modCase.UserID)
			if targetUser == nil {
				targetUser = &discordgo.User{ID: modCase.UserID}
			}
			modlog.Log(ctx.Session, ctx.DB, &modlog.MemberAuditEvent{
				GuildID:   ctx.Message.GuildID,
				Action:    fmt.Sprintf("Case #%d Deleted", caseID),
				User:      targetUser,
				Moderator: ctx.Message.Author,
				Reason:    fmt.Sprintf("Originally: %s | Responsible Mod: <@%s>", modCase.Action, modCase.ModID),
			})
		}
		return ctx.SendSuccess("Deleted mod case #%d.", caseID)
	}

	caseID, err := strconv.ParseInt(ctx.Args[0], 10, 64)
	if err != nil || caseID <= 0 {
		return ctx.SendError("Please specify a valid numeric case ID.")
	}

	modCase, err := ctx.DB.GetModCase(ctx.Message.GuildID, caseID)
	if err != nil || modCase == nil {
		return ctx.SendError(fmt.Sprintf("No mod case found for ID `#%d`.", caseID))
	}

	desc := fmt.Sprintf("> **Target** <@%s> (`%s`)\n> **Moderator** <@%s> (`%s`)\n> **Reason** %s", modCase.UserID, modCase.UserID, modCase.ModID, modCase.ModID, modCase.Reason)

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Case #%d | %s", modCase.CaseID, helpers.TitleCase(strings.ToLower(modCase.Action))),
		Description: desc,
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Case ID #%d • %s", modCase.CaseID, modCase.CreatedAt.Format("August 2, 2006 3:04 PM"))},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type CasesCmd struct{}

func (c *CasesCmd) Name() string        { return "cases" }
func (c *CasesCmd) Aliases() []string   { return []string{"modlogs", "history"} }
func (c *CasesCmd) Category() string    { return "Moderation" }
func (c *CasesCmd) Description() string { return "List mod history and cases for a user." }
func (c *CasesCmd) Usage() string       { return "[@user|userID] | clear" }
func (c *CasesCmd) Example() string     { return "@user" }
func (c *CasesCmd) Permissions() int64  { return discordgo.PermissionManageMessages }

func (c *CasesCmd) Execute(ctx *bot.Context) error {
	if len(ctx.Args) > 0 {
		sub := strings.ToLower(ctx.Args[0])
		if sub == "clear" || sub == "wipe" || sub == "reset" {
			if !ctx.RequirePermissions(discordgo.PermissionAdministrator) {
				return nil
			}
			confirmed, err := ctx.PromptConfirmation("Permanently delete all moderation cases for this server?")
			if err != nil || !confirmed {
				return err
			}
			deleted, err := ctx.DB.DeleteAllModCases(ctx.Message.GuildID)
			if err != nil {
				return ctx.SendError(fmt.Sprintf("Failed to delete moderation cases: %v", err))
			}
			modlog.Log(ctx.Session, ctx.DB, &modlog.ConfigChangeEvent{
				GuildID:   ctx.Message.GuildID,
				Setting:   "Moderation Cases Cleared",
				Moderator: ctx.Message.Author,
				Details:   fmt.Sprintf("Permanently deleted %d cases", deleted),
			})
			return ctx.SendSuccess("Deleted **%d** moderation cases for this server.", deleted)
		}
	}

	targetUser, _, _ := ctx.TargetUserAndMember()
	if targetUser == nil {
		targetUser = ctx.Message.Author
	}

	cases, err := ctx.DB.GetUserModCases(ctx.Message.GuildID, targetUser.ID)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to query moderation history: %v", err))
	}
	if len(cases) == 0 {
		embed := &discordgo.MessageEmbed{
			Title:       "Mod History",
			Description: fmt.Sprintf("> **User** %s (`%s`)\n\nNo mod cases recorded.", targetUser.String(), targetUser.ID),
			Color:       helpers.ColorDefault,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	var sb strings.Builder
	for _, mc := range cases {
		sb.WriteString(fmt.Sprintf("> **Case #%d** - `%s` by <@%s> - %s\n", mc.CaseID, mc.Action, mc.ModID, mc.Reason))
	}

	seperator := "~~------------------------------------------~~"

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Moderation History for %s", targetUser.Username),
		Description: fmt.Sprintf("> **Target** %s (`%s`)\n> **Total Cases** %d\n\n-# %s\n\n**Cases**\n%s", targetUser.String(), targetUser.ID, len(cases), seperator, sb.String()),
		Thumbnail:   &discordgo.MessageEmbedThumbnail{URL: helpers.UserAvatar(targetUser)},
		Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Requested by %s", ctx.Message.Author.Username)},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type ReasonCmd struct{}

func (c *ReasonCmd) Name() string        { return "reason" }
func (c *ReasonCmd) Aliases() []string   { return []string{} }
func (c *ReasonCmd) Category() string    { return "Moderation" }
func (c *ReasonCmd) Description() string { return "Update the reason for a specific case ID." }
func (c *ReasonCmd) Usage() string       { return "<caseID> <new reason>" }
func (c *ReasonCmd) Example() string     { return "14 Spammed invite links" }
func (c *ReasonCmd) Permissions() int64 {
	return discordgo.PermissionModerateMembers | discordgo.PermissionKickMembers | discordgo.PermissionBanMembers
}

func (c *ReasonCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	caseID, err := strconv.ParseInt(ctx.Args[0], 10, 64)
	if err != nil || caseID <= 0 {
		return ctx.SendError("Please specify a valid numeric case ID.")
	}

	newReason := strings.TrimSpace(strings.Join(ctx.Args[1:], " "))
	if newReason == "" {
		return ctx.SendError("Please provide a non-empty reason for the case.")
	}
	if utf8.RuneCountInString(newReason) > 500 {
		return ctx.SendError("Case reason cannot exceed 500 characters.")
	}

	modCase, errGet := ctx.DB.GetModCase(ctx.Message.GuildID, caseID)
	if errGet != nil || modCase == nil {
		return ctx.SendError(fmt.Sprintf("Case `#%d` was not found in this server.", caseID))
	}

	if err := ctx.DB.UpdateModCaseReason(ctx.Message.GuildID, caseID, newReason); err != nil {
		return ctx.SendError(fmt.Sprintf("Could not update case `#%d`: %v", caseID, err))
	}

	targetUser, _ := ctx.Session.User(modCase.UserID)
	if targetUser == nil {
		targetUser = &discordgo.User{ID: modCase.UserID}
	}
	modlog.Log(ctx.Session, ctx.DB, &modlog.MemberAuditEvent{
		GuildID:   ctx.Message.GuildID,
		Action:    fmt.Sprintf("Case #%d Reason Updated", caseID),
		User:      targetUser,
		Moderator: ctx.Message.Author,
		Reason:    fmt.Sprintf("Previous: %s\nNew: %s", modCase.Reason, newReason),
	})

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Updated reason for Case `#%d` to `%s`.", caseID, newReason),
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type ForcenickCmd struct{}

func (c *ForcenickCmd) Name() string        { return "forcenick" }
func (c *ForcenickCmd) Aliases() []string   { return []string{"fnick", "setnick"} }
func (c *ForcenickCmd) Category() string    { return "Moderation" }
func (c *ForcenickCmd) Description() string { return "Forcibly change a member's nickname." }
func (c *ForcenickCmd) Usage() string       { return "<@user> <new nickname> | list" }
func (c *ForcenickCmd) Example() string     { return "@user Moderated Name" }
func (c *ForcenickCmd) Permissions() int64  { return discordgo.PermissionManageNicknames }

func (c *ForcenickCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	if len(ctx.Args) > 0 {
		sub := strings.ToLower(ctx.Args[0])
		if sub == "list" || sub == "show" || sub == "all" {
			lockedNicks, err := ctx.DB.GetAllLockedNicknames(ctx.Message.GuildID)
			if err != nil {
				return ctx.SendError(fmt.Sprintf("Failed to query locked nicknames: %v", err))
			}
			if len(lockedNicks) == 0 {
				embed := &discordgo.MessageEmbed{
					Title:       "Locked Nicknames",
					Description: "No members currently have locked nicknames.",
					Color:       helpers.ColorDefault,
				}
				_, err := ctx.ReplyEmbed(embed)
				return err
			}

			userIDs := make([]string, 0, len(lockedNicks))
			for uid := range lockedNicks {
				userIDs = append(userIDs, uid)
			}
			sort.Strings(userIDs)

			var lines []string
			for _, uid := range userIDs {
				nick := lockedNicks[uid]
				lines = append(lines, fmt.Sprintf("> <@%s> (`%s`): \"%s\"", uid, uid, nick))
			}

			const pageSize = 15
			totalPages := (len(lines) + pageSize - 1) / pageSize
			var embeds []*discordgo.MessageEmbed
			for page := range totalPages {
				start := page * pageSize
				end := start + pageSize
				if end > len(lines) {
					end = len(lines)
				}
				embeds = append(embeds, &discordgo.MessageEmbed{
					Title:       fmt.Sprintf("Locked Nicknames (%d)", len(lines)),
					Description: strings.Join(lines[start:end], "\n"),
					Color:       helpers.ColorDefault,
					Footer:      &discordgo.MessageEmbedFooter{Text: fmt.Sprintf("Page %d of %d", page+1, totalPages)},
				})
			}
			return ctx.SendPaginatedEmbeds(embeds)
		}
	}
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	if targetMember != nil {
		if ok, err := helpers.CanMemberManageTarget(ctx.Session, ctx.Message.GuildID, ctx.Message.Member, targetMember); !ok {
			return ctx.SendError(err.Error())
		}
		if ok, err := helpers.CanBotManageTarget(ctx.Session, ctx.Message.GuildID, targetMember); !ok {
			return ctx.SendError(err.Error())
		}
	}

	var nick string
	if targetViaArg {
		if len(ctx.Args) > 1 {
			nick = strings.Join(ctx.Args[1:], " ")
		}
	} else if len(ctx.Args) > 0 {
		nick = strings.Join(ctx.Args, " ")
	}

	sub := strings.ToLower(strings.TrimSpace(nick))
	if nick == "" || sub == "reset" || sub == "clear" || sub == "remove" || sub == "unlock" {
		if err := ctx.DB.UnlockUserNickname(ctx.Message.GuildID, targetUser.ID); err != nil {
			bot.Warnf("[FORCENICK] Failed to unlock nickname record for %s: %v", targetUser.ID, err)
		}
		listeners.UpdateCachedNick(ctx.Message.GuildID, targetUser.ID, "")
		if err := ctx.Session.GuildMemberNickname(ctx.Message.GuildID, targetUser.ID, ""); err != nil {
			return ctx.SendError(fmt.Sprintf("Failed to reset nickname: %v", err))
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.MemberAuditEvent{
			GuildID:   ctx.Message.GuildID,
			Action:    "Nickname Reset",
			User:      targetUser,
			Moderator: ctx.Message.Author,
			Reason:    "Removed nickname lock and reset nickname",
		})
		return ctx.SendSuccess("Removed nickname lock and reset nickname for **%s**.", targetUser.Username)
	}

	listeners.UpdateCachedNick(ctx.Message.GuildID, targetUser.ID, nick)
	if err := ctx.Session.GuildMemberNickname(ctx.Message.GuildID, targetUser.ID, nick); err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to update nickname: %v", err))
	}

	if err := ctx.DB.LockUserNickname(ctx.Message.GuildID, targetUser.ID, nick, ctx.Message.Author.ID); err != nil {
		bot.Warnf("[FORCENICK] Failed to persist nickname lock for %s: %v", targetUser.ID, err)
	}

	modlog.Log(ctx.Session, ctx.DB, &modlog.MemberAuditEvent{
		GuildID:   ctx.Message.GuildID,
		Action:    "Nickname Locked",
		User:      targetUser,
		Moderator: ctx.Message.Author,
		Reason:    fmt.Sprintf("Locked nickname to `%s`", nick),
	})

	return ctx.SendSuccess("Locked nickname for **%s** to `%s`.", targetUser.Username, nick)
}

type ImuteCmd struct{}

func (c *ImuteCmd) Name() string        { return "imute" }
func (c *ImuteCmd) Aliases() []string   { return []string{"imagemute"} }
func (c *ImuteCmd) Category() string    { return "Moderation" }
func (c *ImuteCmd) Description() string { return "Image/embed mute a member." }
func (c *ImuteCmd) Usage() string       { return "<@user> [reason]" }
func (c *ImuteCmd) Example() string     { return "@user Posting explicit media" }
func (c *ImuteCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *ImuteCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	roleID, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingImageMuteRoleID)
	if roleID == "" {
		return ctx.SendError(fmt.Sprintf("Image mute role is not set. Use `%simagemuterole <@role>` first.", ctx.Prefix))
	}
	if err := checkRoleAuthority(ctx, roleID); err != nil {
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
		caseType:    "Imute",
		modlogLabel: "Image Muted",
		reason:      reason,
		dm: &punishmentDM{
			Title:   "Image Muted",
			Color:   dmColorPunishment,
			Dispute: true,
		},
	}
	action := func() error {
		roleMgr := roles.DefaultRoleManager(ctx.Session, ctx.DB)
		return roleMgr.Assign(ctx.Context(), roles.AssignRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       roleID,
			AssignedBy:   ctx.Message.Author.ID,
			Overwrite:    roles.OverwriteImageMute,
		})
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Applied image mute to **%s**.", targetUser.Username))
}

type IunmuteCmd struct{}

func (c *IunmuteCmd) Name() string        { return "iunmute" }
func (c *IunmuteCmd) Aliases() []string   { return []string{"imageunmute"} }
func (c *IunmuteCmd) Category() string    { return "Moderation" }
func (c *IunmuteCmd) Description() string { return "Remove image/embed mute from a member." }
func (c *IunmuteCmd) Usage() string       { return "<@user>" }
func (c *IunmuteCmd) Example() string     { return "@user" }
func (c *IunmuteCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *IunmuteCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	roleID, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingImageMuteRoleID)
	if roleID == "" {
		return ctx.SendError("Image mute role is not configured.")
	}
	if err := checkRoleAuthority(ctx, roleID); err != nil {
		return ctx.SendError(err.Error())
	}

	p := punishment{
		caseType:    "Iunmute",
		modlogLabel: "Image Unmuted",
		reason:      "Image mute removed",
		dm: &punishmentDM{
			Title: "Image Unmuted",
			Color: dmColorReversal,
		},
	}
	action := func() error {
		roleMgr := roles.DefaultRoleManager(ctx.Session, ctx.DB)
		return roleMgr.Revoke(ctx.Context(), roles.RevokeRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       roleID,
		})
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Removed image mute from **%s**.", targetUser.Username))
}

type RmuteCmd struct{}

func (c *RmuteCmd) Name() string        { return "rmute" }
func (c *RmuteCmd) Aliases() []string   { return []string{"reactionmute"} }
func (c *RmuteCmd) Category() string    { return "Moderation" }
func (c *RmuteCmd) Description() string { return "Reaction mute a member." }
func (c *RmuteCmd) Usage() string       { return "<@user> [reason]" }
func (c *RmuteCmd) Example() string     { return "@user Reaction spam" }
func (c *RmuteCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *RmuteCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	roleID, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingReactionMuteRoleID)
	if roleID == "" {
		return ctx.SendError(fmt.Sprintf("Reaction mute role is not set. Use `%sreactionmuterole <@role>` first.", ctx.Prefix))
	}
	if err := checkRoleAuthority(ctx, roleID); err != nil {
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
		caseType:    "Rmute",
		modlogLabel: "Reaction Muted",
		reason:      reason,
		dm: &punishmentDM{
			Title:   "Reaction Muted",
			Color:   dmColorPunishment,
			Dispute: true,
		},
	}
	action := func() error {
		roleMgr := roles.DefaultRoleManager(ctx.Session, ctx.DB)
		return roleMgr.Assign(ctx.Context(), roles.AssignRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       roleID,
			AssignedBy:   ctx.Message.Author.ID,
			Overwrite:    roles.OverwriteReactionMute,
		})
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Applied reaction mute to **%s**.", targetUser.Username))
}

type RunmuteCmd struct{}

func (c *RunmuteCmd) Name() string        { return "runmute" }
func (c *RunmuteCmd) Aliases() []string   { return []string{"reactionunmute"} }
func (c *RunmuteCmd) Category() string    { return "Moderation" }
func (c *RunmuteCmd) Description() string { return "Remove reaction mute from a member." }
func (c *RunmuteCmd) Usage() string       { return "<@user>" }
func (c *RunmuteCmd) Example() string     { return "@user" }
func (c *RunmuteCmd) Permissions() int64  { return discordgo.PermissionManageRoles }

func (c *RunmuteCmd) Execute(ctx *bot.Context) error {
	ctx.Args = helpers.SplitQuoted(strings.Join(ctx.Args, " "))
	targetUser, targetMember, targetViaArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || (!targetViaArg && ctx.Message.ReferencedMessage == nil && len(ctx.Message.Mentions) == 0) {
		_, err := ctx.SendUsage(c)
		return err
	}

	roleID, _ := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingReactionMuteRoleID)
	if roleID == "" {
		return ctx.SendError("Reaction mute role is not configured.")
	}
	if err := checkRoleAuthority(ctx, roleID); err != nil {
		return ctx.SendError(err.Error())
	}

	p := punishment{
		caseType:    "Runmute",
		modlogLabel: "Reaction Unmuted",
		reason:      "Reaction mute removed",
		dm: &punishmentDM{
			Title: "Reaction Unmuted",
			Color: dmColorReversal,
		},
	}
	action := func() error {
		roleMgr := roles.DefaultRoleManager(ctx.Session, ctx.DB)
		return roleMgr.Revoke(ctx.Context(), roles.RevokeRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       roleID,
		})
	}
	return executePunishment(ctx, p, targetUser, targetMember, action,
		fmt.Sprintf("Removed reaction mute from **%s**.", targetUser.Username))
}
