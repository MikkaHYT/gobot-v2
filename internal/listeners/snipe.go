package listeners

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"gobot/config"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/modlog"

	"github.com/bwmarrin/discordgo"
)

const (
	maxTrackedMsgsPerChannel = 200
	numShards                = 64
)

type SnipeEntry struct {
	AuthorName      string
	AuthorAvatar    string
	AuthorID        string
	Content         string
	AttachmentURLs  []string
	Timestamp       time.Time
	OriginalContent string
	EditedContent   string
}

type ReactionEntry struct {
	AuthorName   string
	AuthorAvatar string
	AuthorID     string
	EmojiName    string
	EmojiURL     string
	MessageID    string
	ChannelID    string
	Timestamp    time.Time
}

type TrackedMessage struct {
	ID           string
	AuthorID     string
	AuthorName   string
	AuthorAvatar string
	IsBot        bool
	Content      string
	Attachments  []string
}

type channelShard struct {
	mu         sync.RWMutex
	recentMsgs map[string][]TrackedMessage
	deleted    map[string][]SnipeEntry
	edited     map[string][]SnipeEntry
	reactions  map[string][]ReactionEntry
	lastActive map[string]time.Time
}

func newChannelShard() *channelShard {
	return &channelShard{
		recentMsgs: make(map[string][]TrackedMessage),
		deleted:    make(map[string][]SnipeEntry),
		edited:     make(map[string][]SnipeEntry),
		reactions:  make(map[string][]ReactionEntry),
		lastActive: make(map[string]time.Time),
	}
}

type SnipeCache struct {
	shards          [numShards]*channelShard
	guildLimitsMu   sync.RWMutex
	guildLimits     map[string]int
	expirationHours int
	maxSnipeLimit   int
	mu              sync.Mutex
	stopChan        chan struct{}
	running         bool
	wg              sync.WaitGroup
}

var GlobalSnipeCache = NewSnipeCache()

func NewSnipeCache() *SnipeCache {
	d := config.DefaultConfig()
	c := &SnipeCache{
		guildLimits:     make(map[string]int),
		expirationHours: d.SnipeExpirationHours,
		maxSnipeLimit:   d.MaxSnipeLimit,
	}
	for i := range c.shards {
		c.shards[i] = newChannelShard()
	}
	return c
}

func safeMemberAvatarURL(member *discordgo.Member, author *discordgo.User, guildID string) string {
	if member != nil && member.Avatar != "" && author != nil {
		gID := member.GuildID
		if gID == "" {
			gID = guildID
		}
		if gID != "" {
			ext := "png"
			if len(member.Avatar) >= 2 && member.Avatar[:2] == "a_" {
				ext = "gif"
			}
			return fmt.Sprintf("https://cdn.discordapp.com/guilds/%s/users/%s/avatars/%s.%s?size=256", gID, author.ID, member.Avatar, ext)
		}
	}
	if author != nil {
		return helpers.UserAvatar(author)
	}
	return ""
}

func (s *SnipeCache) SetExpirationHours(hours int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hours > 0 {
		s.expirationHours = hours
	}
}

func (s *SnipeCache) SetMaxLimit(maxLimit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxLimit > 0 {
		s.maxSnipeLimit = maxLimit
	}
}

func (s *SnipeCache) getShard(channelID string) *channelShard {
	var h uint32 = 2166136261
	for i := 0; i < len(channelID); i++ {
		h ^= uint32(channelID[i])
		h *= 16777619
	}
	return s.shards[h%numShards]
}

func (s *SnipeCache) Start(ctx context.Context) {
	s.StartCleanupRoutine(ctx, 15*time.Minute)
}

func (s *SnipeCache) StartCleanupRoutine(ctx context.Context, interval time.Duration) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopChan = make(chan struct{})
	stopCh := s.stopChan
	expHours := s.expirationHours
	if expHours <= 0 {
		expHours = 24
	}
	s.mu.Unlock()

	expThreshold := time.Duration(expHours) * time.Hour

	s.wg.Add(1)
	helpers.Spawn(func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				for _, shard := range s.shards {
					shard.mu.Lock()
					for channelID, lastTime := range shard.lastActive {
						if now.Sub(lastTime) > expThreshold {
							delete(shard.recentMsgs, channelID)
							delete(shard.deleted, channelID)
							delete(shard.edited, channelID)
							delete(shard.reactions, channelID)
							delete(shard.lastActive, channelID)
						}
					}
					shard.mu.Unlock()
				}
			}
		}
	})
}

func (s *SnipeCache) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopChan)
	s.mu.Unlock()

	s.wg.Wait()
}

func (s *SnipeCache) TrackMessage(m *discordgo.Message) {
	if m == nil || m.ChannelID == "" || m.Author == nil || m.Author.Bot || (m.Content == "" && len(m.Attachments) == 0) {
		return
	}

	var attachments []string
	for _, att := range m.Attachments {
		if att.URL != "" {
			attachments = append(attachments, att.URL)
		}
	}

	avatarURL := safeMemberAvatarURL(m.Member, m.Author, m.GuildID)

	tm := TrackedMessage{
		ID:           m.ID,
		AuthorID:     m.Author.ID,
		AuthorName:   m.Author.Username,
		AuthorAvatar: avatarURL,
		IsBot:        m.Author.Bot,
		Content:      m.Content,
		Attachments:  attachments,
	}

	shard := s.getShard(m.ChannelID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.lastActive[m.ChannelID] = time.Now()
	msgs := shard.recentMsgs[m.ChannelID]

	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].ID == m.ID {
			msgs[i] = tm
			return
		}
	}

	if len(msgs) >= maxTrackedMsgsPerChannel {
		copy(msgs, msgs[1:])
		msgs[len(msgs)-1] = tm
	} else {
		msgs = append(msgs, tm)
	}

	shard.recentMsgs[m.ChannelID] = msgs
}

func (s *SnipeCache) UpdateTracked(channelID, msgID, newContent string, attachments []string) {
	if channelID == "" || msgID == "" {
		return
	}

	shard := s.getShard(channelID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.lastActive[channelID] = time.Now()
	msgs := shard.recentMsgs[channelID]
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].ID == msgID {
			if newContent != "" {
				msgs[i].Content = newContent
			}
			if len(attachments) > 0 {
				msgs[i].Attachments = attachments
			}
			return
		}
	}
}

func (s *SnipeCache) GetTrackedMessage(channelID, msgID string) (TrackedMessage, bool) {
	if channelID == "" || msgID == "" {
		return TrackedMessage{}, false
	}

	shard := s.getShard(channelID)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	msgs := shard.recentMsgs[channelID]
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].ID == msgID {
			return msgs[i], true
		}
	}
	return TrackedMessage{}, false
}

func (s *SnipeCache) RemoveTrackedMessage(channelID, msgID string) {
	if channelID == "" || msgID == "" {
		return
	}

	shard := s.getShard(channelID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	msgs := shard.recentMsgs[channelID]
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].ID == msgID {
			copy(msgs[i:], msgs[i+1:])
			msgs[len(msgs)-1] = TrackedMessage{}
			shard.recentMsgs[channelID] = msgs[:len(msgs)-1]
			return
		}
	}
}

func (s *SnipeCache) RemoveTrackedMessages(channelID string, msgIDs []string) {
	if channelID == "" || len(msgIDs) == 0 {
		return
	}
	idMap := make(map[string]struct{}, len(msgIDs))
	for _, id := range msgIDs {
		idMap[id] = struct{}{}
	}

	shard := s.getShard(channelID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	msgs := shard.recentMsgs[channelID]
	filtered := msgs[:0]
	for _, msg := range msgs {
		if _, exists := idMap[msg.ID]; exists {
			continue
		}
		filtered = append(filtered, msg)
	}

	for i := len(filtered); i < len(msgs); i++ {
		msgs[i] = TrackedMessage{}
	}
	shard.recentMsgs[channelID] = filtered
}

func (s *SnipeCache) SetLimit(guildID string, limit int) {
	if guildID == "" {
		return
	}

	s.guildLimitsMu.Lock()
	defer s.guildLimitsMu.Unlock()

	if limit < 1 {
		limit = 15
	}
	maxLimit := s.maxLimit()
	if limit > maxLimit {
		limit = maxLimit
	}
	s.guildLimits[guildID] = limit
}

func (s *SnipeCache) maxLimit() int {
	s.mu.Lock()
	maxLimit := s.maxSnipeLimit
	s.mu.Unlock()
	if maxLimit <= 0 {
		return config.DefaultConfig().MaxSnipeLimit
	}
	return maxLimit
}

func (s *SnipeCache) GetLimit(guildID string) int {
	if guildID == "" {
		return 25
	}

	s.guildLimitsMu.RLock()
	if lim, ok := s.guildLimits[guildID]; ok && lim > 0 {
		s.guildLimitsMu.RUnlock()
		return lim
	}
	s.guildLimitsMu.RUnlock()

	limit := 25
	if DB != nil {
		if dbLim, err := DB.GetGuildSettingInt(guildID, database.Setting
