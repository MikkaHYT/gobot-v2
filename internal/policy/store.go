package policy

import (
	"gobot/internal/database"
)

type dbGuildRestrictions struct {
	UserIDs    []string
	RoleIDs    []string
	ChannelIDs []string
}

type dbFilterConfig struct {
	InviteEnabled          bool
	InvitePunishment       string
	InviteWhitelistedRoles []string
	WordWhitelistedRoles   []string
	WordPunishment         string
}

type DisabledCommandCounts struct {
	Global int
	Guild  int
}

type dbDriver interface {
	IsGlobalBlacklisted(targetID string) (bool, error)
	AddGlobalBlacklist(targetID, targetType, reason, actorID string) error
	RemoveGlobalBlacklist(targetID string) error
	GetGlobalBlacklist() ([]database.GlobalBlacklistEntry, error)
	GetGlobalBlacklistByType(targetType string) ([]database.GlobalBlacklistEntry, error)

	GetGuildRestrictions(guildID string) (*dbGuildRestrictions, error)
	AddGuildRestriction(guildID string, kind GuildRestrictionKind, targetID string) error
	RemoveGuildRestriction(guildID string, kind GuildRestrictionKind, targetID string) error
	ClearGuildRestrictions(guildID string) error

	IsCommandDisabled(guildID, commandName string) (bool, error)
	DisableCommand(guildID, commandName, actorID string) error
	EnableCommand(guildID, commandName string) error
	GetDisabledCommands(guildID string) ([]string, error)
	GetDisabledCommandCounts(guildID string) (DisabledCommandCounts, error)

	GetFilterConfig(guildID string) (*dbFilterConfig, error)
	SetInviteFilter(guildID string, enabled bool, punishment string) error
	AddInviteFilterWhitelistedRole(guildID, roleID string) error
	RemoveInviteFilterWhitelistedRole(guildID, roleID string) error
	AddWordFilterWhitelistedRole(guildID, roleID string) error
	RemoveWordFilterWhitelistedRole(guildID, roleID string) error

	GetBlacklistedWords(guildID string) ([]string, error)
	AddBlacklistedWord(guildID, word string) error
	AddBlacklistedWords(guildID string, words []string) (int, error)
	RemoveBlacklistedWord(guildID, word string) error
	ClearBlacklistedWords(guildID string) error
	GetBlacklistedRegexes(guildID string) ([]string, error)
	AddBlacklistedRegex(guildID, pattern string) error
	RemoveBlacklistedRegex(guildID, pattern string) error
	ClearBlacklistedRegexes(guildID string) error

	GetAllowedInvites(guildID string) ([]string, error)
	AddAllowedInvite(guildID, invite string) error
	RemoveAllowedInvite(guildID, invite string) error
	ClearAllowedInvites(guildID string) error
}
