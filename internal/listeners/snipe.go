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
		if dbLim, err := DB.GetGuildSettingInt(guildID, database.SettingSnipeLimit); err == nil && dbLim > 0 {
			limit = dbLim
		}
	}
	if limit > s.maxLimit() {
		limit = s.maxLimit()
	}

	s.guildLimitsMu.Lock()
	s.guildLimits[guildID] = limit
	s.guildLimitsMu.Unlock()

	return limit
}

func (s *SnipeCache) AddDeleted(guildID, channelID string, entry SnipeEntry) {
	if channelID == "" {
		return
	}
	limit := s.GetLimit(guildID)
	shard := s.getShard(channelID)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.lastActive[channelID] = time.Now()
	list := append(shard.deleted[channelID], entry)
	if len(list) > limit {
		evictCount := len(list) - limit
		for i := 0; i < evictCount; i++ {
			list[i] = SnipeEntry{}
		}
		list = list[evictCount:]
	}
	shard.deleted[channelID] = list
}

func (s *SnipeCache) AddDeletedBatch(guildID, channelID string, entries []SnipeEntry) {
	if channelID == "" || len(entries) == 0 {
		return
	}
	limit := s.GetLimit(guildID)
	shard := s.getShard(channelID)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.lastActive[channelID] = time.Now()
	list := append(shard.deleted[channelID], entries...)
	if len(list) > limit {
		evictCount := len(list) - limit
		for i := 0; i < evictCount; i++ {
			list[i] = SnipeEntry{}
		}
		list = list[evictCount:]
	}
	shard.deleted[channelID] = list
}

func (s *SnipeCache) GetDeleted(channelID string, index int) (SnipeEntry, int, bool) {
	if channelID == "" {
		return SnipeEntry{}, 0, false
	}

	shard := s.getShard(channelID)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	list := shard.deleted[channelID]
	total := len(list)
	if total == 0 || index < 1 || index > total {
		return SnipeEntry{}, total, false
	}

	return list[total-index], total, true
}

func (s *SnipeCache) AddEdited(guildID, channelID string, entry SnipeEntry) {
	if channelID == "" {
		return
	}
	limit := s.GetLimit(guildID)
	shard := s.getShard(channelID)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.lastActive[channelID] = time.Now()
	list := append(shard.edited[channelID], entry)
	if len(list) > limit {
		evictCount := len(list) - limit
		for i := 0; i < evictCount; i++ {
			list[i] = SnipeEntry{}
		}
		list = list[evictCount:]
	}
	shard.edited[channelID] = list
}

func (s *SnipeCache) GetEdited(channelID string, index int) (SnipeEntry, int, bool) {
	if channelID == "" {
		return SnipeEntry{}, 0, false
	}

	shard := s.getShard(channelID)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	list := shard.edited[channelID]
	total := len(list)
	if total == 0 || index < 1 || index > total {
		return SnipeEntry{}, total, false
	}

	return list[total-index], total, true
}

func (s *SnipeCache) AddReaction(guildID, channelID string, entry ReactionEntry) {
	if channelID == "" {
		return
	}
	limit := s.GetLimit(guildID)
	shard := s.getShard(channelID)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	shard.lastActive[channelID] = time.Now()
	list := append(shard.reactions[channelID], entry)
	if len(list) > limit {
		evictCount := len(list) - limit
		for i := 0; i < evictCount; i++ {
			list[i] = ReactionEntry{}
		}
		list = list[evictCount:]
	}
	shard.reactions[channelID] = list
}

func (s *SnipeCache) GetReaction(channelID string, index int) (ReactionEntry, int, bool) {
	if channelID == "" {
		return ReactionEntry{}, 0, false
	}

	shard := s.getShard(channelID)
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	list := shard.reactions[channelID]
	total := len(list)
	if total == 0 || index < 1 || index > total {
		return ReactionEntry{}, total, false
	}

	return list[total-index], total, true
}

func (s *SnipeCache) Clear(channelID string) {
	if channelID == "" {
		return
	}

	shard := s.getShard(channelID)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	delete(shard.recentMsgs, channelID)
	delete(shard.deleted, channelID)
	delete(shard.edited, channelID)
	delete(shard.reactions, channelID)
	delete(shard.lastActive, channelID)
}

func OnMessageReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	if r == nil || r.UserID == "" {
		return
	}

	if s != nil && s.State != nil && s.State.User != nil && r.UserID == s.State.User.ID {
		return
	}

	var username string
	var avatarURL string
	if member, err := helpers.GetGuildMember(s, r.GuildID, r.UserID); err == nil && member != nil && member.User != nil {
		username = member.User.Username
		avatarURL = safeMemberAvatarURL(member, member.User, r.GuildID)
	} else if user, errU := s.User(r.UserID); errU == nil && user != nil {
		username = user.Username
		avatarURL = helpers.UserAvatar(user)
	} else {
		username = "User"
	}

	emojiURL := ""
	if r.Emoji.ID != "" {
		ext := "png"
		if r.Emoji.Animated {
			ext = "gif"
		}
		emojiURL = fmt.Sprintf("https://cdn.discordapp.com/emojis/%s.%s", r.Emoji.ID, ext)
	}

	entry := ReactionEntry{
		AuthorName:   username,
		AuthorAvatar: avatarURL,
		AuthorID:     r.UserID,
		EmojiName:    r.Emoji.Name,
		EmojiURL:     emojiURL,
		MessageID:    r.MessageID,
		ChannelID:    r.ChannelID,
		Timestamp:    time.Now(),
	}

	GlobalSnipeCache.AddReaction(r.GuildID, r.ChannelID, entry)
}

func OnMessageCreateForSnipe(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m != nil && m.Message != nil {
		GlobalSnipeCache.TrackMessage(m.Message)
	}
}

func OnMessageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
	if m == nil || m.ID == "" {
		return
	}

	var authorID, authorName, authorAvatar string
	var isBot bool
	var content string
	var attachments []string
	var authorUser *discordgo.User

	if m.BeforeDelete != nil && m.BeforeDelete.Author != nil {
		authorUser = m.BeforeDelete.Author
		authorID = m.BeforeDelete.Author.ID
		authorName = m.BeforeDelete.Author.Username
		authorAvatar = safeMemberAvatarURL(m.BeforeDelete.Member, m.BeforeDelete.Author, m.GuildID)
		isBot = m.BeforeDelete.Author.Bot
		content = m.BeforeDelete.Content
		for _, att := range m.BeforeDelete.Attachments {
			if att.URL != "" {
				attachments = append(attachments, att.URL)
			}
		}
	} else if tracked, ok := GlobalSnipeCache.GetTrackedMessage(m.ChannelID, m.ID); ok {
		authorID = tracked.AuthorID
		authorName = tracked.AuthorName
		authorAvatar = tracked.AuthorAvatar
		isBot = tracked.IsBot
		content = tracked.Content
		attachments = tracked.Attachments
	}

	GlobalSnipeCache.RemoveTrackedMessage(m.ChannelID, m.ID)

	if authorID == "" || isBot || (content == "" && len(attachments) == 0) {
		return
	}

	entry := SnipeEntry{
		AuthorName:     authorName,
		AuthorAvatar:   authorAvatar,
		AuthorID:       authorID,
		Content:        content,
		AttachmentURLs: attachments,
		Timestamp:      time.Now(),
	}

	if IsAutoModDeleted(m.ID) {
		return
	}

	GlobalSnipeCache.AddDeleted(m.GuildID, m.ChannelID, entry)

	if DB != nil && m.GuildID != "" {
		createdTS, _ := discordgo.SnowflakeTimestamp(m.ID)
		if authorUser == nil {
			authorUser = &discordgo.User{
				ID:       authorID,
				Username: authorName,
			}
		}
		modlog.Log(s, DB, &modlog.MessageDeleteEvent{
			GuildID:      m.GuildID,
			Author:       authorUser,
			AuthorAvatar: authorAvatar,
			ChannelID:    m.ChannelID,
			MessageID:    m.ID,
			Content:      content,
			Attachments:  attachments,
			SentAt:       createdTS,
		})
	}
}

func OnMessageDeleteBulk(s *discordgo.Session, m *discordgo.MessageDeleteBulk) {
	if m == nil || m.GuildID == "" || len(m.Messages) == 0 {
		return
	}

	msgIDs := make([]string, len(m.Messages))
	copy(msgIDs, m.Messages)
	sort.Slice(msgIDs, func(i, j int) bool {
		tsI, errI := discordgo.SnowflakeTimestamp(msgIDs[i])
		tsJ, errJ := discordgo.SnowflakeTimestamp(msgIDs[j])
		if errI == nil && errJ == nil {
			return tsI.Before(tsJ)
		}
		return msgIDs[i] < msgIDs[j]
	})

	var entries []SnipeEntry
	for _, msgID := range msgIDs {
		if IsAutoModDeleted(msgID) {
			continue
		}
		tracked, ok := GlobalSnipeCache.GetTrackedMessage(m.ChannelID, msgID)
		if !ok || tracked.IsBot || (tracked.Content == "" && len(tracked.Attachments) == 0) {
			continue
		}

		entries = append(entries, SnipeEntry{
			AuthorName:     tracked.AuthorName,
			AuthorAvatar:   tracked.AuthorAvatar,
			AuthorID:       tracked.AuthorID,
			Content:        tracked.Content,
			AttachmentURLs: tracked.Attachments,
			Timestamp:      time.Now(),
		})
	}

	GlobalSnipeCache.RemoveTrackedMessages(m.ChannelID, msgIDs)

	if len(entries) > 0 {
		GlobalSnipeCache.AddDeletedBatch(m.GuildID, m.ChannelID, entries)
	}

	if DB != nil && len(entries) > 0 {
		mod, _ := fetchRecentAuditLog(s, m.GuildID, int(discordgo.AuditLogActionMessageBulkDelete), m.ChannelID)
		details := fmt.Sprintf("Bulk deleted %d message(s) in <#%s>", len(m.Messages), m.ChannelID)
		modlog.Log(s, DB, &modlog.ChannelEvent{
			GuildID:   m.GuildID,
			Action:    "Bulk Message Delete",
			ChannelID: m.ChannelID,
			Moderator: mod,
			Reason:    details,
		})
	}
}

func OnMessageUpdate(s *discordgo.Session, m *discordgo.MessageUpdate) {
	if m == nil || m.ID == "" {
		return
	}

	var authorID, authorName, authorAvatar string
	var isBot bool
	var oldContent string
	var authorUser *discordgo.User

	if m.BeforeUpdate != nil && m.BeforeUpdate.Author != nil {
		authorUser = m.BeforeUpdate.Author
		authorID = m.BeforeUpdate.Author.ID
		authorName = m.BeforeUpdate.Author.Username
		authorAvatar = safeMemberAvatarURL(m.BeforeUpdate.Member, m.BeforeUpdate.Author, m.GuildID)
		isBot = m.BeforeUpdate.Author.Bot
		oldContent = m.BeforeUpdate.Content
	} else if tracked, ok := GlobalSnipeCache.GetTrackedMessage(m.ChannelID, m.ID); ok {
		authorID = tracked.AuthorID
		authorName = tracked.AuthorName
		authorAvatar = tracked.AuthorAvatar
		isBot = tracked.IsBot
		oldContent = tracked.Content
	}

	if authorID == "" && s != nil {
		if msg, err := s.ChannelMessage(m.ChannelID, m.ID); err == nil && msg != nil && msg.Author != nil {
			authorUser = msg.Author
			authorID = msg.Author.ID
			authorName = msg.Author.Username
			authorAvatar = safeMemberAvatarURL(msg.Member, msg.Author, m.GuildID)
			isBot = msg.Author.Bot
		}
	}

	var attachments []string
	for _, att := range m.Attachments {
		if att.URL != "" {
			attachments = append(attachments, att.URL)
		}
	}

	if m.Content != "" || len(attachments) > 0 {
		GlobalSnipeCache.UpdateTracked(m.ChannelID, m.ID, m.Content, attachments)
	}

	if authorID == "" || isBot || oldContent == m.Content || (oldContent == "" &&
