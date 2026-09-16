package roles

import (
	"context"
	"fmt"

	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type DiscordRoleAdapter struct {
	s *discordgo.Session
}

func NewDiscordRoleAdapter(s *discordgo.Session) RoleClient {
	return &DiscordRoleAdapter{s: s}
}

func (a *DiscordRoleAdapter) AddRole(ctx context.Context, guildID, userID, roleID string) error {
	if a.s == nil {
		return fmt.Errorf("session is nil")
	}
	return a.s.GuildMemberRoleAdd(guildID, userID, roleID, discordgo.WithContext(ctx))
}

func (a *DiscordRoleAdapter) RemoveRole(ctx context.Context, guildID, userID, roleID string) error {
	if a.s == nil {
		return fmt.Errorf("session is nil")
	}
	return a.s.GuildMemberRoleRemove(guildID, userID, roleID, discordgo.WithContext(ctx))
}

func (a *DiscordRoleAdapter) FetchRoles(ctx context.Context, guildID string) (map[string]RoleInfo, error) {
	if a.s == nil {
		return nil, fmt.Errorf("session is nil")
	}
	roles, err := a.s.GuildRoles(guildID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]RoleInfo, len(roles))
	for _, r := range roles {
		out[r.ID] = RoleInfo{
			ID:          r.ID,
			Name:        r.Name,
			Position:    r.Position,
			Permissions: r.Permissions,
		}
	}
	return out, nil
}

func (a *DiscordRoleAdapter) FetchMember(ctx context.Context, guildID, userID string) (MemberInfo, error) {
	if a.s == nil {
		return MemberInfo{}, fmt.Errorf("session is nil")
	}
	m, err := helpers.GetGuildMember(a.s, guildID, userID)
	if err != nil || m == nil {
		return MemberInfo{}, fmt.Errorf("failed to fetch member %s: %w", userID, err)
	}
	uID := ""
	if m.User != nil {
		uID = m.User.ID
	}
	return MemberInfo{
		UserID:  uID,
		RoleIDs: m.Roles,
	}, nil
}

func (a *DiscordRoleAdapter) FetchOwnerID(ctx context.Context, guildID string) (string, error) {
	if a.s == nil {
		return "", fmt.Errorf("session is nil")
	}
	g, err := a.s.Guild(guildID)
	if err != nil {
		return "", err
	}
	return g.OwnerID, nil
}

func (a *DiscordRoleAdapter) EnsureChannelOverwrite(ctx context.Context, guildID, roleID string, overwrite ChannelOverwrite, channelID string) error {
	if a.s == nil {
		return nil
	}
	switch overwrite {
	case OverwriteMute:
		return helpers.EnsureRoleOverwrites(a.s, guildID, roleID, helpers.MuteDenyFlags)
	case OverwriteImageMute:
		return helpers.EnsureRoleOverwrites(a.s, guildID, roleID, helpers.ImageMuteDenyFlags)
	case OverwriteReactionMute:
		return helpers.EnsureRoleOverwrites(a.s, guildID, roleID, helpers.ReactionMuteDenyFlags)
	case OverwriteJail:
		if err := helpers.EnsureRoleOverwrites(a.s, guildID, roleID, helpers.JailDenyFlags); err != nil {
			return err
		}
		if channelID != "" {
			return helpers.EnsureJailChannelPermissions(a.s, guildID, roleID, channelID)
		}
	}
	return nil
}

func (a *DiscordRoleAdapter) BotUserID() string {
	if a.s != nil && a.s.State != nil && a.s.State.User != nil {
		return a.s.State.User.ID
	}
	return ""
}
