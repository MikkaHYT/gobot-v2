package policy

import (
	"errors"

	"gobot/internal/database"
)

type prodDB struct {
	db *database.DB
}

func newProdDB(db *database.DB) dbDriver {
	if db == nil {
		return nil
	}
	return &prodDB{db: db}
}

func (p *prodDB) IsGlobalBlacklisted(targetID string) (bool, error) {
	return p.db.IsGlobalBlacklisted(targetID)
}

func (p *prodDB) AddGlobalBlacklist(targetID, targetType, reason, actorID string) error {
	return p.db.AddGlobalBlacklist(targetID, targetType, reason, actorID)
}

func (p *prodDB) RemoveGlobalBlacklist(targetID string) error {
	return p.db.RemoveGlobalBlacklist(targetID)
}

func (p *prodDB) GetGlobalBlacklist() ([]database.GlobalBlacklistEntry, error) {
	return p.db.GetAllGlobalBlacklists()
}

func (p *prodDB) GetGlobalBlacklistByType(targetType string) ([]database.GlobalBlacklistEntry, error) {
	return p.db.GetGlobalBlacklistsByType(targetType)
}

func (p *prodDB) GetGuildRestrictions(guildID string) (*dbGuildRestrictions, error) {
	cfg, err := p.db.GetGuildConfig(guildID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return &dbGuildRestrictions{
				UserIDs:    make([]string, 0),
				RoleIDs:    make([]string, 0),
				ChannelIDs: make([]string, 0),
			}, nil
		}
		return nil, err
	}
	return &dbGuildRestrictions{
		UserIDs:    cfg.BlacklistedUserIDs,
		RoleIDs:    cfg.BlacklistedRoleIDs,
		ChannelIDs: cfg.BlacklistedChannelIDs,
	}, nil
}

func (p *prodDB) AddGuildRestriction(guildID string, kind GuildRestrictionKind, targetID string) error {
	var key database.GuildSetting
	switch kind {
	case RestrictionKindUser:
		key = database.SettingBlacklistedUserIDs
	case RestrictionKindRole:
		key = database.SettingBlacklistedRoleIDs
	case RestrictionKindChannel:
		key = database.SettingBlacklistedChannelIDs
	default:
		return errors.New("unknown restriction kind")
	}

	slice, err := p.db.GetGuildSettingSlice(guildID, key)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	for _, id := range slice {
		if id == targetID {
			return ErrAlreadyRestricted
		}
	}
	slice = append(slice, targetID)
	return p.db.UpdateGuildSetting(guildID, key, slice)
}

func (p *prodDB) RemoveGuildRestriction(guildID string, kind GuildRestrictionKind, targetID string) error {
	var key database.GuildSetting
	switch kind {
	case RestrictionKindUser:
		key = database.SettingBlacklistedUserIDs
	case RestrictionKindRole:
		key = database.SettingBlacklistedRoleIDs
	case RestrictionKindChannel:
		key = database.SettingBlacklistedChannelIDs
	default:
		return errors.New("unknown restriction kind")
	}

	slice, err := p.db.GetGuildSettingSlice(guildID, key)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	found := false
	newSlice := make([]string, 0, len(slice))
	for _, id := range slice {
		if id == targetID {
			found = true
		} else {
			newSlice = append(newSlice, id)
		}
	}
	if !found {
		return ErrNotRestricted
	}
	return p.db.UpdateGuildSetting(guildID, key, newSlice)
}

func (p *prodDB) ClearGuildRestrictions(guildID string) error {
	updates := map[database.GuildSetting]any{
		database.SettingBlacklistedUserIDs:    []string{},
		database.SettingBlacklistedRoleIDs:    []string{},
		database.SettingBlacklistedChannelIDs: []string{},
	}
	return p.db.UpdateGuildSettings(guildID, updates)
}

func (p *prodDB) IsCommandDisabled(guildID, commandName string) (bool, error) {
	return p.db.IsCommandDisabled(guildID, commandName)
}

func (p *prodDB) DisableCommand(guildID, commandName, actorID string) error {
	return p.db.DisableCommand(guildID, commandName, actorID)
}

func (p *prodDB) EnableCommand(guildID, commandName string) error {
	return p.db.EnableCommand(guildID, commandName)
}

func (p *prodDB) GetDisabledCommands(guildID string) ([]string, error) {
	return p.db.GetDisabledCommands(guildID)
}

func (p *prodDB) GetDisabledCommandCounts(guildID string) (DisabledCommandCounts, error) {
	guildCount, globalCount, err := p.db.GetDisabledCommandCounts(guildID)
	if err != nil {
		return DisabledCommandCounts{}, err
	}
	return DisabledCommandCounts{Global: globalCount, Guild: guildCount}, nil
}

func (p *prodDB) GetFilterConfig(guildID string) (*dbFilterConfig, error) {
	cfg, err := p.db.GetGuildConfig(guildID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return &dbFilterConfig{
				InviteEnabled:          false,
				InvitePunishment:       "delete",
				InviteWhitelistedRoles: make([]string, 0),
				WordWhitelistedRoles:   make([]string, 0),
				WordPunishment:         "delete",
			}, nil
		}
		return nil, err
	}
	return &dbFilterConfig{
		InviteEnabled:          cfg.InviteFilterEnabled,
		InvitePunishment:       cfg.InviteFilterPunishment,
		InviteWhitelistedRoles: cfg.InviteFilterWhitelistedRoleIDs,
		WordWhitelistedRoles:   cfg.WordFilterWhitelistedRoleIDs,
		WordPunishment:         cfg.WordFilterPunishment,
	}, nil
}

func (p *prodDB) SetInviteFilter(guildID string, enabled bool, punishment string) error {
	updates := map[database.GuildSetting]any{
		database.SettingInviteFilterEnabled: enabled,
	}
	if punishment != "" {
		updates[database.SettingInviteFilterPunishment] = punishment
	}
	return p.db.UpdateGuildSettings(guildID, updates)
}

func (p *prodDB) AddInviteFilterWhitelistedRole(guildID, roleID string) error {
	roles, err := p.db.GetGuildSettingSlice(guildID, database.SettingInviteFilterWhitelistedRoles)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	for _, r := range roles {
		if r == roleID {
			return ErrAlreadyExempt
		}
	}
	roles = append(roles, roleID)
	return p.db.UpdateGuildSetting(guildID, database.SettingInviteFilterWhitelistedRoles, roles)
}

func (p *prodDB) RemoveInviteFilterWhitelistedRole(guildID, roleID string) error {
	roles, err := p.db.GetGuildSettingSlice(guildID, database.SettingInviteFilterWhitelistedRoles)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	found := false
	newRoles := make([]string, 0, len(roles))
	for _, r := range roles {
		if r == roleID {
			found = true
		} else {
			newRoles = append(newRoles, r)
		}
	}
	if !found {
		return ErrNotExempt
	}
	return p.db.UpdateGuildSetting(guildID, database.SettingInviteFilterWhitelistedRoles, newRoles)
}

func (p *prodDB) AddWordFilterWhitelistedRole(guildID, roleID string) error {
	roles, err := p.db.GetGuildSettingSlice(guildID, database.SettingWordFilterWhitelistedRoles)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	for _, r := range roles {
		if r == roleID {
			return ErrAlreadyExempt
		}
	}
	roles = append(roles, roleID)
	return p.db.UpdateGuildSetting(guildID, database.SettingWordFilterWhitelistedRoles, roles)
}

func (p *prodDB) RemoveWordFilterWhitelistedRole(guildID, roleID string) error {
	roles, err := p.db.GetGuildSettingSlice(guildID, database.SettingWordFilterWhitelistedRoles)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}
	found := false
	newRoles := make([]string, 0, len(roles))
	for _, r := range roles {
		if r == roleID {
			found = true
		} else {
			newRoles = append(newRoles, r)
		}
	}
	if !found {
		return ErrNotExempt
	}
	return p.db.UpdateGuildSetting(guildID, database.SettingWordFilterWhitelistedRoles, newRoles)
}

func (p *prodDB) GetBlacklistedWords(guildID string) ([]string, error) {
	return p.db.GetBlacklistedWords(guildID)
}

func (p *prodDB) AddBlacklistedWord(guildID, word string) error {
	return p.db.AddBlacklistedWord(guildID, word)
}

func (p *prodDB) RemoveBlacklistedWord(guildID, word string) error {
	return p.db.RemoveBlacklistedWord(guildID, word)
}

func (p *prodDB) ClearBlacklistedWords(guildID string) error {
	return p.db.ClearBlacklistedWords(guildID)
}

func (p *prodDB) GetBlacklistedRegexes(guildID string) ([]string, error) {
	return p.db.GetBlacklistedRegexes(guildID)
}

func (p *prodDB) AddBlacklistedRegex(guildID, pattern string) error {
	return p.db.AddBlacklistedRegex(guildID, pattern)
}

func (p *prodDB) RemoveBlacklistedRegex(guildID, pattern string) error {
	return p.db.RemoveBlacklistedRegex(guildID, pattern)
}

func (p *prodDB) ClearBlacklistedRegexes(guildID string) error {
	return p.db.ClearBlacklistedRegexes(guildID)
}

func (p *prodDB) AddBlacklistedWords(guildID string, words []string) (int, error) {
	return p.db.AddBlacklistedWords(guildID, words)
}

func (p *prodDB) GetAllowedInvites(guildID string) ([]string, error) {
	return p.db.GetAllowedInvites(guildID)
}

func (p *prodDB) AddAllowedInvite(guildID, invite string) error {
	return p.db.AddAllowedInvite(guildID, invite)
}

func (p *prodDB) RemoveAllowedInvite(guildID, invite string) error {
	return p.db.RemoveAllowedInvite(guildID, invite)
}

func (p *prodDB) ClearAllowedInvites(guildID string) error {
	return p.db.ClearAllowedInvites(guildID)
}

