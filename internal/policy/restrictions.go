package policy

import (
	"slices"
	"strings"
	"time"

	"gobot/internal/database"
)

type ScopeType string

const (
	ScopeGlobal ScopeType = "global"
	ScopeGuild  ScopeType = "guild"
)

type Scope struct {
	Type    ScopeType
	GuildID string
}

type GuildRestrictionKind string

const (
	RestrictionKindUser    GuildRestrictionKind = "user"
	RestrictionKindRole    GuildRestrictionKind = "role"
	RestrictionKindChannel GuildRestrictionKind = "channel"
)

type GlobalRestriction struct {
	TargetID   string
	TargetKind string
	Reason     string
	ActorID    string
	CreatedAt  time.Time
}

type GuildRestrictions struct {
	UserIDs    []string
	RoleIDs    []string
	ChannelIDs []string
}

var protectedCommands = map[string]bool{
	"help":            true,
	"enablecmd":       true,
	"disablecmd":      true,
	"disabledcmds":    true,
	"enablecommand":   true,
	"disablecommand":  true,
	"whitelist":       true,
	"wl":              true,
	"unblacklist":     true,
	"unignore":        true,
	"blacklist":       true,
	"bl":              true,
	"ignore":          true,
	"globalblacklist": true,
	"gblacklist":      true,
	"gbl":             true,
	"globalwhitelist": true,
	"gwhitelist":      true,
	"gwl":             true,
	"gunblacklist":    true,
}

func IsProtectedCommand(name string) bool {
	return protectedCommands[strings.ToLower(strings.TrimSpace(name))]
}

func (m *Module) AddGlobalRestriction(targetID, targetKind, reason, actorID string) error {
	if m == nil || m.db == nil {
		return ErrPolicyUnavailable
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return ErrTargetRequired
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	return m.db.AddGlobalBlacklist(targetID, targetKind, reason, actorID)
}

func (m *Module) RemoveGlobalRestriction(targetID string) error {
	if m == nil || m.db == nil {
		return ErrPolicyUnavailable
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return ErrTargetRequired
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	isBL, err := m.db.IsGlobalBlacklisted(targetID)
	if err != nil {
		return err
	}
	if !isBL {
		return ErrNotRestricted
	}
	return m.db.RemoveGlobalBlacklist(targetID)
}

func (m *Module) GetGlobalRestriction(targetID string) (*GlobalRestriction, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return nil, ErrTargetRequired
	}
	entries, err := m.db.GetGlobalBlacklist()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.TargetID == targetID {
			return &GlobalRestriction{
				TargetID:   e.TargetID,
				TargetKind: e.TargetType,
				Reason:     e.Reason,
				ActorID:    e.AddedBy,
				CreatedAt:  e.CreatedAt,
			}, nil
		}
	}
	return nil, ErrNotRestricted
}

func (m *Module) ListGlobalRestrictions(targetKind string) ([]GlobalRestriction, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	var entries []database.GlobalBlacklistEntry
	var err error
	if targetKind != "" {
		entries, err = m.db.GetGlobalBlacklistByType(targetKind)
	} else {
		entries, err = m.db.GetGlobalBlacklist()
	}
	if err != nil {
		return nil, err
	}
	res := make([]GlobalRestriction, len(entries))
	for i, e := range entries {
		res[i] = GlobalRestriction{
			TargetID:   e.TargetID,
			TargetKind: e.TargetType,
			Reason:     e.Reason,
			ActorID:    e.AddedBy,
			CreatedAt:  e.CreatedAt,
		}
	}
	return res, nil
}

func (m *Module) AddGuildRestriction(guildID string, kind GuildRestrictionKind, targetID string) error {
	if m == nil || m.db == nil {
		return ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return ErrGuildRequired
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return ErrTargetRequired
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	return m.db.AddGuildRestriction(guildID, kind, targetID)
}

func (m *Module) RemoveGuildRestriction(guildID string, kind GuildRestrictionKind, targetID string) error {
	if m == nil || m.db == nil {
		return ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return ErrGuildRequired
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return ErrTargetRequired
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	return m.db.RemoveGuildRestriction(guildID, kind, targetID)
}

func (m *Module) ListGuildRestrictions(guildID string) (*GuildRestrictions, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrGuildRequired
	}
	restr, err := m.db.GetGuildRestrictions(guildID)
	if err != nil {
		return nil, err
	}
	return &GuildRestrictions{
		UserIDs:    slices.Clone(restr.UserIDs),
		RoleIDs:    slices.Clone(restr.RoleIDs),
		ChannelIDs: slices.Clone(restr.ChannelIDs),
	}, nil
}

func (m *Module) ClearGuildRestrictions(guildID string) error {
	if m == nil || m.db == nil {
		return ErrPolicyUnavailable
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return ErrGuildRequired
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	return m.db.ClearGuildRestrictions(guildID)
}

func (m *Module) DisableCommand(scope Scope, commandName, actorID string) error {
	if m == nil || m.db == nil {
		return ErrPolicyUnavailable
	}
	var scopeTarget string
	switch scope.Type {
	case ScopeGlobal:
		scopeTarget = "global"
	case ScopeGuild:
		scopeTarget = strings.TrimSpace(scope.GuildID)
		if scopeTarget == "" {
			return ErrGuildRequired
		}
	default:
		return ErrInvalidScope
	}

	name := strings.ToLower(strings.TrimSpace(commandName))
	if name == "" {
		return ErrWordRequired
	}
	if IsProtectedCommand(name) {
		return ErrProtectedCommand
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	return m.db.DisableCommand(scopeTarget, name, actorID)
}

func (m *Module) EnableCommand(scope Scope, commandName string) error {
	if m == nil || m.db == nil {
		return ErrPolicyUnavailable
	}
	var scopeTarget string
	switch scope.Type {
	case ScopeGlobal:
		scopeTarget = "global"
	case ScopeGuild:
		scopeTarget = strings.TrimSpace(scope.GuildID)
		if scopeTarget == "" {
			return ErrGuildRequired
		}
	default:
		return ErrInvalidScope
	}

	name := strings.ToLower(strings.TrimSpace(commandName))
	if name == "" {
		return ErrWordRequired
	}
	m.mutationMu.Lock()
	defer m.mutationMu.Unlock()
	return m.db.EnableCommand(scopeTarget, name)
}

func (m *Module) ListDisabledCommands(scope Scope) ([]string, error) {
	if m == nil || m.db == nil {
		return nil, ErrPolicyUnavailable
	}
	var scopeTarget string
	switch scope.Type {
	case ScopeGlobal:
		scopeTarget = "global"
	case ScopeGuild:
		scopeTarget = strings.TrimSpace(scope.GuildID)
		if scopeTarget == "" {
			return nil, ErrGuildRequired
		}
	default:
		return nil, ErrInvalidScope
	}
	return m.db.GetDisabledCommands(scopeTarget)
}

func (m *Module) GetDisabledCounts(guildID string) (DisabledCommandCounts, error) {
	if m == nil || m.db == nil {
		return DisabledCommandCounts{}, ErrPolicyUnavailable
	}
	return m.db.GetDisabledCommandCounts(guildID)
}
