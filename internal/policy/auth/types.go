package auth

import (
	"context"
	"errors"

	"github.com/bwmarrin/discordgo"
)

var (
	ErrGuildRequired    = errors.New("guild id is required")
	ErrActorRequired    = errors.New("mod ID is required")
	ErrMemberNotFound   = errors.New("member not found")
	ErrRoleNotFound     = errors.New("role not found")
	ErrBotIDUnavailable = errors.New("bot identity unavailable")
)

type Request struct {
	GuildID      string
	ChannelID    string
	ActorID      string
	ActorMember  *discordgo.Member
	RequiredPerm int64
	TargetMember *discordgo.Member
	TargetRole   *discordgo.Role
	TargetUserID string
	TargetRoleID string
	CheckBot     bool
}

type Decision struct {
	Allowed bool
	Reason  string
	Err     error
}

func Allow() Decision {
	return Decision{Allowed: true}
}

func Deny(reason string) Decision {
	return Decision{Allowed: false, Reason: reason}
}

func Fail(err error, reason string) Decision {
	return Decision{Allowed: false, Reason: reason, Err: err}
}

type StateProvider interface {
	GetGuild(ctx context.Context, guildID string) (*discordgo.Guild, error)
	GetMember(ctx context.Context, guildID, userID string) (*discordgo.Member, error)
	GetRoles(ctx context.Context, guildID string) ([]*discordgo.Role, error)
	ComputePermissions(ctx context.Context, guildID, channelID, userID string) (int64, error)
	BotUserID() string
}
