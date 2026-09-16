package helpers

import (
	"context"
	"errors"
	"fmt"

	"gobot/internal/policy/auth"

	"github.com/bwmarrin/discordgo"
)

const DangerousPermissions = discordgo.PermissionAdministrator |
	discordgo.PermissionManageGuild |
	discordgo.PermissionManageRoles |
	discordgo.PermissionBanMembers |
	discordgo.PermissionKickMembers |
	discordgo.PermissionManageChannels |
	discordgo.PermissionManageWebhooks |
	discordgo.PermissionManageGuildExpressions

func IsRoleDangerous(role *discordgo.Role) bool {
	if role == nil {
		return false
	}
	return role.Permissions&int64(DangerousPermissions) != 0
}


func CanMemberManageTarget(s *discordgo.Session, guildID string, modMember, targetMember *discordgo.Member) (bool, error) {
	if modMember == nil || targetMember == nil || guildID == "" {
		return false, fmt.Errorf("executor, target member, and guild are required")
	}
	modID := ""
	if modMember.User != nil {
		modID = modMember.User.ID
	}
	authorizer := auth.NewAuthorizer(auth.NewDiscordStateAdapter(s), nil)
	d := authorizer.Evaluate(context.Background(), auth.Request{
		GuildID:      guildID,
		ActorID:      modID,
		ActorMember:  modMember,
		TargetMember: targetMember,
		CheckBot:     false,
	})
	if !d.Allowed {
		if d.Reason != "" {
			return false, errors.New(d.Reason)
		}
		if d.Err != nil {
			return false, d.Err
		}
		return false, errors.New("you cannot moderate this member")
	}
	return true, nil
}

func CanMemberManageRole(s *discordgo.Session, guildID string, modMember *discordgo.Member, targetRole *discordgo.Role) (bool, error) {
	if modMember == nil || targetRole == nil || guildID == "" {
		return false, fmt.Errorf("executor, target role, and guild are required")
	}
	modID := ""
	if modMember.User != nil {
		modID = modMember.User.ID
	}
	authorizer := auth.NewAuthorizer(auth.NewDiscordStateAdapter(s), nil)
	d := authorizer.Evaluate(context.Background(), auth.Request{
		GuildID:     guildID,
		ActorID:     modID,
		ActorMember: modMember,
		TargetRole:  targetRole,
		CheckBot:    false,
	})
	if !d.Allowed {
		if d.Reason != "" {
			return false, errors.New(d.Reason)
		}
		if d.Err != nil {
			return false, d.Err
		}
		return false, errors.New("you cannot modify or assign this role")
	}
	return true, nil
}

func CanBotManageTarget(s *discordgo.Session, guildID string, targetMember *discordgo.Member) (bool, error) {
	if s == nil || targetMember == nil || guildID == "" {
		return false, fmt.Errorf("session, guild, and target member are required")
	}
	adapter := auth.NewDiscordStateAdapter(s)
	botID := adapter.BotUserID()
	if botID == "" {
		return false, fmt.Errorf("unable to verify bot identity for hierarchy check")
	}
	authorizer := auth.NewAuthorizer(adapter, []string{botID})
	d := authorizer.Evaluate(context.Background(), auth.Request{
		GuildID:      guildID,
		ActorID:      botID,
		TargetMember: targetMember,
		CheckBot:     true,
	})
	if !d.Allowed {
		if d.Reason != "" {
			return false, errors.New(d.Reason)
		}
		if d.Err != nil {
			return false, d.Err
		}
		return false, errors.New("bot cannot moderate this member")
	}
	return true, nil
}

func CanBotManageRole(s *discordgo.Session, guildID string, targetRole *discordgo.Role) (bool, error) {
	if s == nil || targetRole == nil || guildID == "" {
		return false, fmt.Errorf("session, guild, and target role are required")
	}
	adapter := auth.NewDiscordStateAdapter(s)
	botID := adapter.BotUserID()
	if botID == "" {
		return false, fmt.Errorf("unable to verify bot identity for hierarchy check")
	}
	authorizer := auth.NewAuthorizer(adapter, []string{botID})
	d := authorizer.Evaluate(context.Background(), auth.Request{
		GuildID:    guildID,
		ActorID:    botID,
		TargetRole: targetRole,
		CheckBot:   true,
	})
	if !d.Allowed {
		if d.Reason != "" {
			return false, errors.New(d.Reason)
		}
		if d.Err != nil {
			return false, d.Err
		}
		return false, errors.New("bot cannot manage this role")
	}
	return true, nil
}
