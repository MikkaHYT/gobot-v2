package auth

import (
	"context"

	"github.com/bwmarrin/discordgo"
)

type DiscordStateAdapter struct {
	session *discordgo.Session
}

func NewDiscordStateAdapter(s *discordgo.Session) *DiscordStateAdapter {
	return &DiscordStateAdapter{session: s}
}

func (a *DiscordStateAdapter) GetGuild(ctx context.Context, guildID string) (*discordgo.Guild, error) {
	if a.session == nil || guildID == "" {
		return nil, ErrGuildRequired
	}
	if a.session.State != nil {
		if g, err := a.session.State.Guild(guildID); err == nil && g != nil {
			return g, nil
		}
	}
	return a.session.Guild(guildID)
}

func (a *DiscordStateAdapter) GetMember(ctx context.Context, guildID, userID string) (*discordgo.Member, error) {
	if a.session == nil || guildID == "" || userID == "" {
		return nil, ErrMemberNotFound
	}
	if a.session.State != nil {
		if m, err := a.session.State.Member(guildID, userID); err == nil && m != nil {
			return m, nil
		}
	}
	return a.session.GuildMember(guildID, userID)
}

func (a *DiscordStateAdapter) GetRoles(ctx context.Context, guildID string) ([]*discordgo.Role, error) {
	if a.session == nil || guildID == "" {
		return nil, ErrGuildRequired
	}
	if a.session.State != nil {
		if g, err := a.session.State.Guild(guildID); err == nil && g != nil && len(g.Roles) > 0 {
			return g.Roles, nil
		}
	}
	return a.session.GuildRoles(guildID)
}

func (a *DiscordStateAdapter) ComputePermissions(ctx context.Context, guildID, channelID, userID string) (int64, error) {
	if a.session == nil || userID == "" {
		return 0, nil
	}

	if channelID != "" && a.session.State != nil {
		perms, err := a.session.UserChannelPermissions(userID, channelID)
		if err == nil {
			return perms, nil
		}
	}

	guild, err := a.GetGuild(ctx, guildID)
	if err != nil || guild == nil {
		return 0, err
	}
	if guild.OwnerID != "" && userID == guild.OwnerID {
		return discordgo.PermissionAll, nil
	}

	member, err := a.GetMember(ctx, guildID, userID)
	if err != nil || member == nil {
		return 0, err
	}

	roles, err := a.GetRoles(ctx, guildID)
	if err != nil {
		return 0, err
	}

	return computeGuildPermissions(guild, member, roles), nil
}

func computeGuildPermissions(guild *discordgo.Guild, member *discordgo.Member, roles []*discordgo.Role) int64 {
	var perms int64
	rolesMap := make(map[string]*discordgo.Role, len(roles))
	for _, r := range roles {
		if r != nil {
			rolesMap[r.ID] = r
		}
	}

	if everyoneRole, ok := rolesMap[guild.ID]; ok && everyoneRole != nil {
		perms |= everyoneRole.Permissions
	}

	for _, roleID := range member.Roles {
		if r, ok := rolesMap[roleID]; ok && r != nil {
			perms |= r.Permissions
		}
	}

	if perms&discordgo.PermissionAdministrator != 0 {
		return discordgo.PermissionAll
	}
	return perms
}

func (a *DiscordStateAdapter) BotUserID() string {
	if a.session == nil {
		return ""
	}
	if a.session.State != nil && a.session.State.User != nil {
		return a.session.State.User.ID
	}
	if u, err := a.session.User("@me"); err == nil && u != nil {
		return u.ID
	}
	return ""
}
