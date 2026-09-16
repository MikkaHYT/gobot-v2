package listeners

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"
	"gobot/internal/roles"
)

type userSpamState struct {
	mu              sync.Mutex
	lastXPEarned    time.Time
	lastMessageHash uint64
	repeatCount     int
}

var spamTracker sync.Map

func CleanupSpamTracker() {
	spamTracker.Range(func(key, value any) bool {
		state, ok := value.(*userSpamState)
		if !ok || state == nil {
			spamTracker.Delete(key)
			return true
		}

		state.mu.Lock()
		isStale := time.Since(state.lastXPEarned) > 15*time.Minute
		state.mu.Unlock()

		if isStale {
			spamTracker.Delete(key)
		}
		return true
	})
}

func fnvHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = io.WriteString(h, s)
	return h.Sum64()
}

type levelRoleCacheEntry struct {
	roles   []database.LevelRoleReward
	fetched time.Time
}

var (
	guildLevelRolesMu    sync.RWMutex
	guildLevelRolesCache = make(map[string]levelRoleCacheEntry)
)

func InvalidateGuildLevelRolesCache(guildID string) {
	guildLevelRolesMu.Lock()
	defer guildLevelRolesMu.Unlock()
	delete(guildLevelRolesCache, guildID)
}

func getGuildLevelRoles(db *database.DB, guildID string) ([]database.LevelRoleReward, error) {
	guildLevelRolesMu.RLock()
	cached, ok := guildLevelRolesCache[guildID]
	guildLevelRolesMu.RUnlock()

	if ok && time.Since(cached.fetched) < 30*time.Second {
		return cached.roles, nil
	}

	roles, err := db.GetLevelRoles(guildID)
	if err != nil {
		return nil, err
	}

	guildLevelRolesMu.Lock()
	guildLevelRolesCache[guildID] = levelRoleCacheEntry{
		roles:   roles,
		fetched: time.Now(),
	}
	guildLevelRolesMu.Unlock()

	return roles, nil
}

func SyncUserLevelRoles(s *discordgo.Session, db *database.DB, guildID, userID string) error {
	if db == nil || guildID == "" || userID == "" {
		return nil
	}

	levelRoles, err := getGuildLevelRoles(db, guildID)
	if err != nil || len(levelRoles) == 0 {
		return err
	}

	ul, err := db.GetUserXP(guildID, userID)
	if err != nil {
		return fmt.Errorf("failed to fetch user level: %w", err)
	}

	member, err := helpers.GetGuildMember(s, guildID, userID)
	if err != nil || member == nil {
		return fmt.Errorf("failed to fetch guild member %s: %w", userID, err)
	}

	stackStr, errStack := db.GetGuildSettingString(guildID, database.SettingLevelRolesStack)
	if errStack != nil {
		logger.Warnf("[LEVELING] Failed to query level role stacking for guild %s: %v", guildID, errStack)
	}
	shouldStack := stackStr != "false"

	levelRoleMap := make(map[string]bool, len(levelRoles))
	for _, lr := range levelRoles {
		levelRoleMap[lr.RoleID] = true
	}

	targetLevelRoles := make(map[string]bool)
	if shouldStack {
		for _, lr := range levelRoles {
			if ul.Level >= lr.Level {
				targetLevelRoles[lr.RoleID] = true
			}
		}
	} else {
		var highestRoleID string
		highestLevel := -1
		for _, lr := range levelRoles {
			if ul.Level >= lr.Level && lr.Level > highestLevel {
				highestLevel = lr.Level
				highestRoleID = lr.RoleID
			}
		}
		if highestRoleID != "" {
			targetLevelRoles[highestRoleID] = true
		}
	}

	currentLevelRoles := make(map[string]bool)
	for _, rID := range member.Roles {
		if levelRoleMap[rID] {
			currentLevelRoles[rID] = true
		}
	}

	var rolesToAdd []string
	var rolesToRemove []string

	for rID := range targetLevelRoles {
		if !currentLevelRoles[rID] {
			rolesToAdd = append(rolesToAdd, rID)
		}
	}
	for rID := range currentLevelRoles {
		if !targetLevelRoles[rID] {
			rolesToRemove = append(rolesToRemove, rID)
		}
	}

	roleMgr := roles.DefaultRoleManager(s, db)
	for _, rID := range rolesToAdd {
		if err := roleMgr.Assign(context.Background(), roles.AssignRequest{
			GuildID:      guildID,
			TargetUserID: userID,
			RoleID:       rID,
		}); err != nil {
			logger.Warnf("[LEVELING] Failed to add level role %s to %s via roleMgr: %v", rID, userID, err)
		}
	}
	for _, rID := range rolesToRemove {
		if err := roleMgr.Revoke(context.Background(), roles.RevokeRequest{
			GuildID:      guildID,
			TargetUserID: userID,
			RoleID:       rID,
		}); err != nil {
			logger.Warnf("[LEVELING] Failed to remove level role %s from %s via roleMgr: %v", rID, userID, err)
		}
	}

	return nil
}

func CalculateXPFromLength(content string) int {
	length := utf8.RuneCountInString(content)

	if length <= 5 {
		return 5
	}
	if length >= 100 {
		return 35
	}

	return 5 + ((length-5)*30)/95
}

func OnLevelingMessageCreate(db *database.DB) func(s *discordgo.Session, m *discordgo.MessageCreate) {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if db == nil || m.GuildID == "" || m.Author == nil || m.Author.Bot || m.WebhookID != "" {
			return
		}

		trimmed := strings.TrimSpace(m.Content)
		if utf8.RuneCountInString(trimmed) < 4 {
			return
		}

		cfg, _ := db.GetGuildConfig(m.GuildID)

		if cfg != nil {
			for _, chID := range cfg.IgnoredLevelChannelIDs {
				if chID == m.ChannelID {
					return
				}
			}
		}

		now := time.Now()
		key := m.GuildID + ":" + m.Author.ID
		msgHash := fnvHash(strings.ToLower(trimmed))

		val, _ := spamTracker.LoadOrStore(key, &userSpamState{})
		state, ok := val.(*userSpamState)
		if !ok {
			return
		}

		state.mu.Lock()
		if msgHash == state.lastMessageHash {
			state.repeatCount++
		} else {
			state.lastMessageHash = msgHash
			state.repeatCount = 0
		}

		if state.repeatCount >= 2 {
			state.mu.Unlock()
			return
		}

		if now.Sub(state.lastXPEarned) < 60*time.Second {
			state.mu.Unlock()
			return
		}
		previousLastXP := state.lastXPEarned
		state.lastXPEarned = now
		state.mu.Unlock()

		rawXP := CalculateXPFromLength(trimmed)
		xpEarned := rawXP

		if cfg != nil && cfg.XPMultiplier > 0 {
			calculated := int(float64(rawXP) * cfg.XPMultiplier)
			if calculated < 1 {
				calculated = 1
			}
			xpEarned = calculated
		}

		newLevel, leveledUp, err := db.AddUserXP(m.GuildID, m.Author.ID, int64(xpEarned))
		if err != nil {
			state.mu.Lock()
			state.lastXPEarned = previousLastXP
			state.mu.Unlock()
			logger.Errorf("[LEVELING] Failed to add XP for user %s: %v", m.Author.ID, err)
			return
		}

		if leveledUp {
			enabled, _ := db.GetGuildSettingBool(m.GuildID, database.SettingLevelUpMessagesEnabled)
			if enabled {
				embed := &discordgo.MessageEmbed{
					Description: fmt.Sprintf("Congratulations %s, you reached **Level %d**!", m.Author.Mention(), newLevel),
					Color:       helpers.ColorDefault,
				}
				if _, err := s.ChannelMessageSendEmbed(m.ChannelID, embed); err != nil {
					logger.Warnf("[LEVELING] Failed to send level up message in channel %s: %v", m.ChannelID, err)
				}
			}

			if err := SyncUserLevelRoles(s, db, m.GuildID, m.Author.ID); err != nil {
				logger.Warnf("[LEVELING] Role sync failed for user %s: %v", m.Author.ID, err)
			}
		}
	}
}
