package radio

import (
	"context"
	"strings"
	"time"

	"gobot/internal/database"
)

var ValidRadioModes = map[string]string{
	"all":                       "all",
	"auto":                      "all",
	"any":                       "all",
	"off":                       "all",
	"reset":                     "all",
	"clear":                     "all",
	"unreleased":                "unreleased",
	"leak":                      "unreleased",
	"leaks":                     "unreleased",
	"released":                  "released",
	"session":                   "sessions",
	"sessions":                  "sessions",
	"recording_session":         "sessions",
	"gbgr":                      "GB&GR",
	"gb&gr":                     "GB&GR",
	"goodbye":                   "GB&GR",
	"goodbye & good riddance":   "GB&GR",
	"goodbye and good riddance": "GB&GR",
	"drfl":                      "DRFL",
	"death race":                "DRFL",
	"death race for love":       "DRFL",
	"jw3":                       "JW3",
	"juice wrld 3":              "JW3",
	"tpne":                      "JW3",
	"the party never ends":      "JW3",
	"outsiders":                 "JW3",
	"the outsiders":             "JW3",
	"out":                       "JW3",
	"wod":                       "WOD",
	"wrld on drugs":             "WOD",
	"world on drugs":            "WOD",
	"post":                      "POST",
	"posthumous":                "POST",
	"pre":                       "PRE-GBGR",
	"pre-gbgr":                  "PRE-GBGR",
	"pre gbgr":                  "PRE-GBGR",
}

func NormalizeRadioMode(input string) (string, bool) {
	clean := strings.ToLower(strings.TrimSpace(input))
	if canonical, ok := ValidRadioModes[clean]; ok {
		return canonical, true
	}
	return "", false
}

const (
	RestrictionAll          = database.RadioRestrictionModeAll
	RestrictionVoiceChannel = database.RadioRestrictionModeVoiceChannel
	RestrictionModerators   = database.RadioRestrictionModeModerators
)

type RadioSettings struct {
	RestrictionMode      string
	AutojoinChannelID    string
	PlayerChannelID      string
	RestrictionModeSet   bool
	AutojoinChannelIDSet bool
	PlayerChannelIDSet   bool
}

type SettingsStore interface {
	LoadRadioSettings(ctx context.Context, guildID string) (RadioSettings, error)
	SaveRadioSettings(ctx context.Context, guildID string, settings RadioSettings) error
}

func NormalizeRestrictionMode(mode string) string {
	return database.NormalizeRadioRestrictionMode(mode)
}

func isValidRestrictionMode(mode string) bool {
	switch NormalizeRestrictionMode(mode) {
	case RestrictionAll, RestrictionVoiceChannel, RestrictionModerators:
		return true
	default:
		return false
	}
}

type databaseSettingsStore struct {
	db *database.DB
}

func (s databaseSettingsStore) LoadRadioSettings(ctx context.Context, guildID string) (RadioSettings, error) {
	mode, err := s.db.GetGuildSettingString(guildID, database.SettingRadioRestrictionMode)
	if err != nil {
		return RadioSettings{}, err
	}
	autojoin, err := s.db.GetGuildSettingString(guildID, database.SettingRadioAutojoinChannelID)
	if err != nil {
		return RadioSettings{}, err
	}
	playerCh, err := s.db.GetGuildSettingString(guildID, database.SettingRadioPlayerChannelID)
	if err != nil {
		return RadioSettings{}, err
	}
	return RadioSettings{
		RestrictionMode:   NormalizeRestrictionMode(mode),
		AutojoinChannelID: autojoin,
		PlayerChannelID:   playerCh,
	}, nil
}

func (s databaseSettingsStore) SaveRadioSettings(ctx context.Context, guildID string, settings RadioSettings) error {
	updates := make(map[database.GuildSetting]any, 3)
	if settings.RestrictionModeSet {
		updates[database.SettingRadioRestrictionMode] = NormalizeRestrictionMode(settings.RestrictionMode)
	}
	if settings.AutojoinChannelIDSet {
		updates[database.SettingRadioAutojoinChannelID] = settings.AutojoinChannelID
	}
	if settings.PlayerChannelIDSet {
		updates[database.SettingRadioPlayerChannelID] = settings.PlayerChannelID
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.UpdateGuildSettings(guildID, updates)
}

func NewDatabaseSettingsStore(db *database.DB) SettingsStore {
	if db == nil {
		return nil
	}
	return databaseSettingsStore{db: db}
}

type settingsLoadState struct {
	loaded   bool
	loading  bool
	waitCh   chan struct{}
	err      error
	loadedAt time.Time
	lastWarn time.Time
}

func (m *Module) loadSettings(ctx context.Context, s *session) {
	_ = m.loadSettingsWithForce(ctx, s, false)
}

func (m *Module) loadSettingsWithForce(ctx context.Context, s *session, force bool) error {
	if m.settings == nil {
		return nil
	}
	if ctx == nil {
		ctx = m.ctx
	}
	for {
		s.settingsMu.Lock()
		if s.settings.loaded && !force {
			s.settingsMu.Unlock()
			return nil
		}
		if s.settings.loading {
			ch := s.settings.waitCh
			s.settingsMu.Unlock()
			select {
			case <-ch:
				if !force {
					s.settingsMu.Lock()
					err := s.settings.err
					s.settingsMu.Unlock()
					return err
				}
				s.settingsMu.Lock()
				loaded := s.settings.loaded
				err := s.settings.err
				s.settingsMu.Unlock()
				if loaded {
					return nil
				}
				if err != nil {
					continue
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		s.settings.loading = true
		s.settings.waitCh = make(chan struct{})
		ch := s.settings.waitCh
		s.settings.err = nil
		s.settingsMu.Unlock()

		settings, err := m.settings.LoadRadioSettings(ctx, s.guildID)
		s.settingsMu.Lock()
		if err != nil {
			s.settings.err = err
			s.settings.loading = false
			close(ch)
			s.settings.waitCh = nil
			shouldWarn := m.now().Sub(s.settings.lastWarn) >= time.Minute
			if shouldWarn {
				s.settings.lastWarn = m.now()
			}
			s.settingsMu.Unlock()
			if shouldWarn && m.warn != nil {
				m.warn(err)
			}
			return err
		}
		s.mu.Lock()
		s.restrictionMode = NormalizeRestrictionMode(settings.RestrictionMode)
		s.autojoinChannelID = settings.AutojoinChannelID
		s.playerChannelID = settings.PlayerChannelID
		s.mu.Unlock()
		s.settings.loaded = true
		s.settings.loadedAt = m.now()
		s.settings.err = nil
		s.settings.loading = false
		close(ch)
		s.settings.waitCh = nil
		s.settingsMu.Unlock()
		return nil
	}
}

func (m *Module) lockSettingsForUpdate(ctx context.Context, s *session) error {
	for {
		s.settingsMu.Lock()
		if !s.settings.loading {
			return nil
		}
		waitCh := s.settings.waitCh
		s.settingsMu.Unlock()
		select {
		case <-waitCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (m *Module) UpdateSettings(ctx context.Context, request SettingsUpdate) (Snapshot, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	s, err := m.getSession(ctx, request.GuildID)
	if err != nil {
		return Snapshot{}, err
	}
	if err := m.lockSettingsForUpdate(ctx, s); err != nil {
		return Snapshot{}, err
	}
	defer s.settingsMu.Unlock()
	s.mu.Lock()
	settings := RadioSettings{
		RestrictionMode:   s.restrictionMode,
		AutojoinChannelID: s.autojoinChannelID,
		PlayerChannelID:   s.playerChannelID,
	}
	if request.RestrictionModeSet {
		settings.RestrictionMode = NormalizeRestrictionMode(request.RestrictionMode)
		settings.RestrictionModeSet = true
		if !isValidRestrictionMode(settings.RestrictionMode) {
			s.mu.Unlock()
			return Snapshot{}, ErrInvalidRequest
		}
	}
	if request.AutojoinChannelIDSet {
		settings.AutojoinChannelID = request.AutojoinChannelID
		settings.AutojoinChannelIDSet = true
	}
	if request.PlayerChannelIDSet {
		settings.PlayerChannelID = request.PlayerChannelID
		settings.PlayerChannelIDSet = true
	}
	s.mu.Unlock()
	if m.settings != nil {
		if err := m.settings.SaveRadioSettings(ctx, request.GuildID, settings); err != nil {
			return Snapshot{}, err
		}
	}
	s.mu.Lock()
	s.lastActivity = m.now()
	s.restrictionMode = settings.RestrictionMode
	s.autojoinChannelID = settings.AutojoinChannelID
	s.playerChannelID = settings.PlayerChannelID
	if request.Is247Set {
		s.is247 = request.Is247
	}
	if request.ModeSet {
		s.mode = request.Mode
	}
	s.settings.loaded = true
	s.settings.loadedAt = m.now()
	s.settings.err = nil
	if s.settings.loading && s.settings.waitCh != nil {
		close(s.settings.waitCh)
		s.settings.waitCh = nil
		s.settings.loading = false
	}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()
	return snapshot, nil
}

func (m *Module) RefreshSettings(ctx context.Context, guildID string) error {
	if ctx == nil {
		ctx = m.ctx
	}
	if m.settings == nil {
		return nil
	}
	s, err := m.getSession(ctx, guildID)
	if err != nil {
		return err
	}
	return m.loadSettingsWithForce(ctx, s, true)
}
