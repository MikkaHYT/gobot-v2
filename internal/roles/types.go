package roles

import (
	"context"
	"errors"
	"time"

	"gobot/internal/database"
)

var (
	ErrHierarchyActorTarget = errors.New("cannot moderate a member with an equal or higher role")
	ErrHierarchyActorRole   = errors.New("cannot modify or assign a role equal to or higher than your highest role")
	ErrHierarchyBotTarget   = errors.New("bot highest role is not positioned high enough to moderate this member")
	ErrHierarchyBotRole     = errors.New("bot highest role is not positioned high enough to manage this role")
	ErrSelfModeration       = errors.New("you cannot perform this action on yourself")
	ErrOwnerModeration      = errors.New("you cannot moderate the server owner")
	ErrInvalidDuration      = errors.New("invalid role duration")
	ErrRoleNotFound         = errors.New("role not found")
	ErrMemberNotFound       = errors.New("member not found")
	ErrAlreadyJailed        = database.ErrAlreadyJailed
	ErrNotJailed            = errors.New("member is not currently jailed")
)

type ChannelOverwrite int

const (
	OverwriteNone ChannelOverwrite = iota
	OverwriteMute
	OverwriteImageMute
	OverwriteReactionMute
	OverwriteJail
)

type RoleInfo struct {
	ID          string
	Name        string
	Position    int
	Permissions int64
}

type MemberInfo struct {
	UserID  string
	RoleIDs []string
}

type AuthRequest struct {
	GuildID      string
	ActorUserID  string
	TargetUserID string
	TargetRoleID string
}

type AssignRequest struct {
	GuildID      string
	ActorUserID  string
	TargetUserID string
	RoleID       string
	Duration     time.Duration
	AssignedBy   string
	Overwrite    ChannelOverwrite
}

type RevokeRequest struct {
	GuildID      string
	ActorUserID  string
	TargetUserID string
	RoleID       string
}

type JailRequest struct {
	GuildID        string
	ActorUserID    string
	TargetUserID   string
	JailRoleID     string
	JailChannelID  string
	Reason         string
	PermanentRoles []string
}

type UnjailRequest struct {
	GuildID      string
	ActorUserID  string
	TargetUserID string
	JailRoleID   string
	Force        bool
}

type RejoinRequest struct {
	GuildID        string
	UserID         string
	IsJailed       bool
	JailRoleID     string
	AutoroleIDs    []string
	SavedRoleIDs   []string
	AdminPermFlags int64
	MuteRoleID     string
}

type UnjailResult struct {
	Restored []string
	Missed   []string
}

type RoleClient interface {
	AddRole(ctx context.Context, guildID, userID, roleID string) error
	RemoveRole(ctx context.Context, guildID, userID, roleID string) error
	FetchRoles(ctx context.Context, guildID string) (map[string]RoleInfo, error)
	FetchMember(ctx context.Context, guildID, userID string) (MemberInfo, error)
	FetchOwnerID(ctx context.Context, guildID string) (string, error)
	EnsureChannelOverwrite(ctx context.Context, guildID, roleID string, overwrite ChannelOverwrite, channelID string) error
	BotUserID() string
}

type Store interface {
	SaveTempRole(guildID, userID, roleID string, expiresAt time.Time, assignedBy string) error
	RemoveTempRole(guildID, userID, roleID string) error
	DeleteExpiredTempRole(guildID, userID, roleID string, maxExpiresAt time.Time) (bool, error)
	GetActiveTempRolesForUser(guildID, userID string) ([]database.TempRoleEntry, error)
	GetUserTempRoles(guildID, userID string) ([]database.TempRoleEntry, error)
	GetExpiredTempRoles() ([]database.TempRoleEntry, error)
	SaveJailedUser(guildID, userID string, roles []string, jailedBy, reason string) error
	GetJailedUserInfo(guildID, userID string) (*database.JailedUser, error)
	RemoveJailedUser(guildID, userID string) error
}

type RoleManager interface {
	CheckAuthority(ctx context.Context, req AuthRequest) error
	Assign(ctx context.Context, req AssignRequest) error
	Revoke(ctx context.Context, req RevokeRequest) error
	Jail(ctx context.Context, req JailRequest) ([]string, error)
	Unjail(ctx context.Context, req UnjailRequest) (UnjailResult, error)
	RestoreOnRejoin(ctx context.Context, req RejoinRequest) ([]string, error)
	SweepExpired(ctx context.Context) (int, error)
}
