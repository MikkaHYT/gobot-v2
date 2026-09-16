package voice

import (
	"fmt"
	"sync"
	"time"

	"gobot/internal/database"
)

var GlobalDB *database.DB

type ActiveChannel struct {
	ChannelID string
	OwnerID   string
	GuildID   string
}

type UserSettings struct {
	GuildID      string
	UserID       string
	ChannelName  string
	ChannelLimit int
}

func compositeKey(guildID, userID string) string {
	return guildID + ":" + userID
}

type Manager struct {
	mu             sync.RWMutex
	activeChannels map[string]*ActiveChannel
	userChannels   map[string]string        
	userSettings   map[string]*UserSettings 
	cooldowns      map[string]time.Time     
	creationLocks  map[string]bool          
}

var GlobalManager = NewManager()

func NewManager() *Manager {
	return &Manager{
		activeChannels: make(map[string]*ActiveChannel),
		userChannels:   make(map[string]string),
		userSettings:   make(map[string]*UserSettings),
		cooldowns:      make(map[string]time.Time),
		creationLocks:  make(map[string]bool),
	}
}

func (m *Manager) TryAcquireCreationLock(guildID, userID string) bool {
	key := compositeKey(guildID, userID)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.creationLocks[key] {
		return false
	}
	if _, exists := m.userChannels[key]; exists {
		return false
	}
	m.creationLocks[key] = true
	return true
}

func (m *Manager) ReleaseCreationLock(guildID, userID string) {
	key := compositeKey(guildID, userID)
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.creationLocks, key)
}

func (m *Manager) TrackChannel(channelID, ownerID, guildID string) {
	key := compositeKey(guildID, ownerID)
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := &ActiveChannel{
		ChannelID: channelID,
		OwnerID:   ownerID,
		GuildID:   guildID,
	}
	m.activeChannels[channelID] = ch
	m.userChannels[key] = channelID
}

func (m *Manager) RemoveChannel(channelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ch, exists := m.activeChannels[channelID]; exists {
		key := compositeKey(ch.GuildID, ch.OwnerID)
		delete(m.userChannels, key)
		delete(m.activeChannels, channelID)
	}
}

func (m *Manager) GetUserOwnedChannel(guildID, userID string) (string, bool) {
	key := compositeKey(guildID, userID)
	m.mu.RLock()
	defer m.mu.RUnlock()

	chID, exists := m.userChannels[key]
	return chID, exists
}

func (m *Manager) IsActiveChannel(channelID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.activeChannels[channelID]
	return exists
}

func (m *Manager) GetChannelOwner(channelID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if ch, exists := m.activeChannels[channelID]; exists {
		return ch.OwnerID, true
	}
	return "", false
}

func (m *Manager) TransferOwnership(channelID, newOwnerID string) error {
	m.mu.Lock()
	ch, exists := m.activeChannels[channelID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("active voice channel not found")
	}

	oldOwnerID := ch.OwnerID
	oldKey := compositeKey(ch.GuildID, oldOwnerID)
	newKey := compositeKey(ch.GuildID, newOwnerID)
	delete(m.userChannels, oldKey)
	ch.OwnerID = newOwnerID
	m.userChannels[newKey] = channelID
	m.mu.Unlock()

	if GlobalDB != nil {
		if err := GlobalDB.UpdateTempVoiceChannelOwner(channelID, newOwnerID); err != nil {
			m.mu.Lock()
			if curr, ok := m.activeChannels[channelID]; ok && curr.OwnerID == newOwnerID {
				delete(m.userChannels, newKey)
				curr.OwnerID = oldOwnerID
				m.userChannels[oldKey] = channelID
			}
			m.mu.Unlock()
			return fmt.Errorf("failed to persist ownership update to database: %w", err)
		}
	}
	return nil
}

func (m *Manager) IsOnCooldown(guildID, userID string) bool {
	key := compositeKey(guildID, userID)
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	expiry, exists := m.cooldowns[key]
	if !exists {
		return false
	}
	if now.After(expiry) {
		delete(m.cooldowns, key)
		return false
	}
	return true
}

func (m *Manager) SetCooldown(guildID, userID string, duration time.Duration) {
	key := compositeKey(guildID, userID)
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.cooldowns) > 100 {
		for k, exp := range m.cooldowns {
			if now.After(exp) {
				delete(m.cooldowns, k)
			}
		}
	}
	m.cooldowns[key] = now.Add(duration)
}

func (m *Manager) GetUserSettings(guildID, userID string) (*UserSettings, bool) {
	key := compositeKey(guildID, userID)
	m.mu.RLock()
	defer m.mu.RUnlock()

	settings, found := m.userSettings[key]
	if !found || settings == nil {
		return nil, false
	}
	cp := *settings
	return &cp, true
}
func (m *Manager) SetUserSettings(guildID, userID, name string, limit int) {
	key := compositeKey(guildID, userID)
	m.mu.Lock()
	defer m.mu.Unlock()

	current, exists := m.userSettings[key]
	if !exists || current == nil {
		initialLimit := 0
		if limit >= 0 {
			initialLimit = limit
		}
		m.userSettings[key] = &UserSettings{
			GuildID:      guildID,
			UserID:       userID,
			ChannelName:  name,
			ChannelLimit: initialLimit,
		}
		return
	}

	if name != "" {
		current.ChannelName = name
	}
	if limit >= 0 {
		current.ChannelLimit = limit
	}
}
