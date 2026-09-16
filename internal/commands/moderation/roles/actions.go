package roles

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/modlog"
	rolemgr "gobot/internal/roles"

	"github.com/bwmarrin/discordgo"
)

func GiveRoles(ctx *bot.Context, targetUser *discordgo.User, targetMember *discordgo.Member, targetRoles []*discordgo.Role) error {
	if targetUser == nil || len(targetRoles) == 0 {
		return fmt.Errorf("invalid user or role parameters")
	}

	roleMgr := rolemgr.DefaultRoleManager(ctx.Session, ctx.DB)
	for _, r := range targetRoles {
		if err := roleMgr.CheckAuthority(ctx.Context(), rolemgr.AuthRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			TargetRoleID: r.ID,
		}); err != nil {
			return err
		}
	}

	memberRoles := make(map[string]bool)
	if targetMember != nil {
		for _, rID := range targetMember.Roles {
			memberRoles[rID] = true
		}
	}

	var given []*discordgo.Role
	for _, r := range targetRoles {
		if memberRoles[r.ID] {
			continue
		}
		if err := roleMgr.Assign(ctx.Context(), rolemgr.AssignRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       r.ID,
			AssignedBy:   ctx.Message.Author.ID,
		}); err != nil {
			return err
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.MemberRoleUpdateEvent{
			GuildID:    ctx.Message.GuildID,
			TargetUser: targetUser,
			RoleID:     r.ID,
			IsAdd:      true,
			Moderator:  ctx.Message.Author,
		})
		given = append(given, r)
	}

	_ = ctx.ReactSuccess()

	var desc string
	if len(given) == 0 {
		roleWord := "role"
		if len(targetRoles) > 1 {
			roleWord = "roles"
		}
		desc = fmt.Sprintf("**%s** already has %s %s.", targetUser.String(), roleWord, formatRoleMentions(targetRoles))
	} else {
		roleWord := "role"
		if len(given) > 1 {
			roleWord = "roles"
		}
		desc = fmt.Sprintf("Gave %s %s to **%s**.", roleWord, formatRoleMentions(given), targetUser.String())
	}

	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: desc,
	})
	return err
}

func RemoveRoles(ctx *bot.Context, targetUser *discordgo.User, targetMember *discordgo.Member, targetRoles []*discordgo.Role) error {
	if targetUser == nil || len(targetRoles) == 0 {
		return fmt.Errorf("invalid user or role parameters")
	}

	roleMgr := rolemgr.DefaultRoleManager(ctx.Session, ctx.DB)
	for _, r := range targetRoles {
		if err := roleMgr.CheckAuthority(ctx.Context(), rolemgr.AuthRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			TargetRoleID: r.ID,
		}); err != nil {
			return err
		}
	}

	memberRoles := make(map[string]bool)
	if targetMember != nil {
		for _, rID := range targetMember.Roles {
			memberRoles[rID] = true
		}
	}

	var removed []*discordgo.Role
	for _, r := range targetRoles {
		if !memberRoles[r.ID] {
			continue
		}
		if err := roleMgr.Revoke(ctx.Context(), rolemgr.RevokeRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       r.ID,
		}); err != nil {
			return err
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.MemberRoleUpdateEvent{
			GuildID:    ctx.Message.GuildID,
			TargetUser: targetUser,
			RoleID:     r.ID,
			IsAdd:      false,
			Moderator:  ctx.Message.Author,
		})
		removed = append(removed, r)
	}

	_ = ctx.ReactSuccess()

	var desc string
	if len(removed) == 0 {
		roleWord := "role"
		if len(targetRoles) > 1 {
			roleWord = "roles"
		}
		desc = fmt.Sprintf("**%s** does not have %s %s.", targetUser.String(), roleWord, formatRoleMentions(targetRoles))
	} else {
		roleWord := "role"
		if len(removed) > 1 {
			roleWord = "roles"
		}
		desc = fmt.Sprintf("Removed %s %s from **%s**.", roleWord, formatRoleMentions(removed), targetUser.String())
	}

	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: desc,
	})
	return err
}

func TempRole(ctx *bot.Context, targetUser *discordgo.User, targetMember *discordgo.Member, targetRole *discordgo.Role, duration time.Duration, durStr string) error {
	if targetUser == nil || targetRole == nil {
		return fmt.Errorf("invalid user or role parameters")
	}
	if duration < 5*time.Second {
		return fmt.Errorf("temporary role duration must be at least 5 seconds")
	}
	if duration > 365*24*time.Hour {
		return fmt.Errorf("temporary role duration cannot exceed 365 days")
	}

	roleMgr := rolemgr.DefaultRoleManager(ctx.Session, ctx.DB)
	if err := roleMgr.CheckAuthority(ctx.Context(), rolemgr.AuthRequest{
		GuildID:      ctx.Message.GuildID,
		ActorUserID:  ctx.Message.Author.ID,
		TargetUserID: targetUser.ID,
		TargetRoleID: targetRole.ID,
	}); err != nil {
		return err
	}

	if err := roleMgr.Assign(ctx.Context(), rolemgr.AssignRequest{
		GuildID:      ctx.Message.GuildID,
		ActorUserID:  ctx.Message.Author.ID,
		TargetUserID: targetUser.ID,
		RoleID:       targetRole.ID,
		Duration:     duration,
		AssignedBy:   ctx.Message.Author.ID,
	}); err != nil {
		return err
	}

	_ = ctx.ReactSuccess()
	modlog.Log(ctx.Session, ctx.DB, &modlog.MemberRoleUpdateEvent{
		GuildID:    ctx.Message.GuildID,
		TargetUser: targetUser,
		RoleID:     targetRole.ID,
		IsAdd:      true,
		Moderator:  ctx.Message.Author,
	})

	expiresAt := time.Now().Add(duration)
	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Temporarily gave role <@&%s> to **%s** for **%s** (expires <t:%d:R>).", targetRole.ID, targetUser.String(), durStr, expiresAt.Unix()),
	})
	return err
}

func ToggleRoles(ctx *bot.Context, targetUser *discordgo.User, targetMember *discordgo.Member, targetRoles []*discordgo.Role) error {
	if targetUser == nil || targetMember == nil || len(targetRoles) == 0 {
		return fmt.Errorf("invalid user or role parameters")
	}

	roleMgr := rolemgr.DefaultRoleManager(ctx.Session, ctx.DB)
	for _, r := range targetRoles {
		if err := roleMgr.CheckAuthority(ctx.Context(), rolemgr.AuthRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			TargetRoleID: r.ID,
		}); err != nil {
			return err
		}
	}

	memberRoles := make(map[string]bool, len(targetMember.Roles))
	for _, rID := range targetMember.Roles {
		memberRoles[rID] = true
	}

	var toAdd []*discordgo.Role
	var toRemove []*discordgo.Role
	for _, r := range targetRoles {
		if memberRoles[r.ID] {
			toRemove = append(toRemove, r)
		} else {
			toAdd = append(toAdd, r)
		}
	}

	for _, r := range toAdd {
		if err := roleMgr.Assign(ctx.Context(), rolemgr.AssignRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       r.ID,
			AssignedBy:   ctx.Message.Author.ID,
		}); err != nil {
			return err
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.MemberRoleUpdateEvent{
			GuildID:    ctx.Message.GuildID,
			TargetUser: targetUser,
			RoleID:     r.ID,
			IsAdd:      true,
			Moderator:  ctx.Message.Author,
		})
	}

	for _, r := range toRemove {
		if err := roleMgr.Revoke(ctx.Context(), rolemgr.RevokeRequest{
			GuildID:      ctx.Message.GuildID,
			ActorUserID:  ctx.Message.Author.ID,
			TargetUserID: targetUser.ID,
			RoleID:       r.ID,
		}); err != nil {
			return err
		}
		modlog.Log(ctx.Session, ctx.DB, &modlog.MemberRoleUpdateEvent{
			GuildID:    ctx.Message.GuildID,
			TargetUser: targetUser,
			RoleID:     r.ID,
			IsAdd:      false,
			Moderator:  ctx.Message.Author,
		})
	}

	_ = ctx.ReactSuccess()

	var desc string
	if len(toAdd) > 0 && len(toRemove) > 0 {
		addWord := "role"
		if len(toAdd) > 1 {
			addWord = "roles"
		}
		remWord := "role"
		if len(toRemove) > 1 {
			remWord = "roles"
		}
		desc = fmt.Sprintf("Gave %s %s and removed %s %s for **%s**.",
			addWord, formatRoleMentions(toAdd),
			remWord, formatRoleMentions(toRemove),
			targetUser.String())
	} else if len(toAdd) > 0 {
		roleWord := "role"
		if len(toAdd) > 1 {
			roleWord = "roles"
		}
		desc = fmt.Sprintf("Gave %s %s to **%s**.", roleWord, formatRoleMentions(toAdd), targetUser.String())
	} else {
		roleWord := "role"
		if len(toRemove) > 1 {
			roleWord = "roles"
		}
		desc = fmt.Sprintf("Removed %s %s from **%s**.", roleWord, formatRoleMentions(toRemove), targetUser.String())
	}

	_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: desc,
	})
	return err
}

func formatRoleMentions(roles []*discordgo.Role) string {
	mentions := make([]string, len(roles))
	for i, r := range roles {
		mentions[i] = fmt.Sprintf("<@&%s>", r.ID)
	}
	return strings.Join(mentions, ", ")
}

func ParseRoleQueries(raw string) []string {
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		var queries []string
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				queries = append(queries, trimmed)
			}
		}
		return queries
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return []string{trimmed}
}

func resolveRoles(ctx *bot.Context, tokens []string) ([]*discordgo.Role, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no role specified")
	}

	raw := strings.Join(tokens, " ")
	queries := ParseRoleQueries(raw)

	if len(queries) > 1 {
		return resolveRoleQueries(ctx, queries)
	}

	if r, err := ctx.ResolveRole(raw); err == nil && r != nil {
		return []*discordgo.Role{r}, nil
	}

	if len(tokens) > 1 {
		var tokenRoles []*discordgo.Role
		allMatched := true
		for _, tok := range tokens {
			r, err := ctx.ResolveRole(tok)
			if err != nil || r == nil {
				allMatched = false
				break
			}
			tokenRoles = append(tokenRoles, r)
		}
		if allMatched && len(tokenRoles) > 0 {
			return deduplicateRoles(tokenRoles), nil
		}
	}

	return nil, fmt.Errorf("role not found: `%s`", raw)
}

func resolveRoleQueries(ctx *bot.Context, queries []string) ([]*discordgo.Role, error) {
	var resolved []*discordgo.Role
	for _, q := range queries {
		r, err := ctx.ResolveRole(q)
		if err != nil || r == nil {
			return nil, fmt.Errorf("role not found: `%s`", q)
		}
		resolved = append(resolved, r)
	}
	return deduplicateRoles(resolved), nil
}

func deduplicateRoles(roles []*discordgo.Role) []*discordgo.Role {
	seen := make(map[string]bool, len(roles))
	var unique []*discordgo.Role
	for _, r := range roles {
		if !seen[r.ID] {
			seen[r.ID] = true
			unique = append(unique, r)
		}
	}
	return unique
}

func (c *RoleCmd) handleToggle(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 2) {
		return nil
	}

	targetUser, targetMember, usedArg, err := ctx.TargetUserAndMemberArg(0)
	if err != nil || targetUser == nil || targetMember == nil {
		return fmt.Errorf("could not find member: `%s`", ctx.Args[0])
	}

	var roleTokens []string
	if usedArg {
		roleTokens = ctx.Args[1:]
	} else {
		roleTokens = ctx.Args
	}

	targetRoles, err := resolveRoles(ctx, roleTokens)
	if err != nil {
		return err
	}

	return ToggleRoles(ctx, targetUser, targetMember, targetRoles)
}

func (c *RoleCmd) handleExplicitAdd(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "role add <user> <role...>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	targetUser, targetMember, err := ctx.ResolveUserAndMember(subArgs[0])
	if err != nil || targetUser == nil {
		return fmt.Errorf("could not find member: `%s`", subArgs[0])
	}

	targetRoles, err := resolveRoles(ctx, subArgs[1:])
	if err != nil {
		return err
	}

	return GiveRoles(ctx, targetUser, targetMember, targetRoles)
}

func (c *RoleCmd) handleExplicitRemove(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(2, "role remove <user> <role...>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	targetUser, targetMember, err := ctx.ResolveUserAndMember(subArgs[0])
	if err != nil || targetUser == nil {
		return fmt.Errorf("could not find member: `%s`", subArgs[0])
	}

	targetRoles, err := resolveRoles(ctx, subArgs[1:])
	if err != nil {
		return err
	}

	return RemoveRoles(ctx, targetUser, targetMember, targetRoles)
}

func (c *RoleCmd) handleTemp(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(3, "role temp <user> <role> <duration>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	targetUser, targetMember, err := ctx.ResolveUserAndMember(subArgs[0])
	if err != nil || targetUser == nil {
		return fmt.Errorf("could not find member: `%s`", subArgs[0])
	}

	durStr := subArgs[len(subArgs)-1]
	duration, err := helpers.ParseDuration(durStr)
	if err != nil {
		return fmt.Errorf("invalid duration: `%s`. Examples: `30m`, `2h`, `7d`", durStr)
	}

	roleArg := strings.Join(subArgs[1:len(subArgs)-1], " ")
	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return fmt.Errorf("role not found: `%s`", roleArg)
	}

	return TempRole(ctx, targetUser, targetMember, targetRole, duration, durStr)
}
