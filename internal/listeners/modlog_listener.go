package listeners

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"
	"gobot/internal/modlog"

	"github.com/bwmarrin/discordgo"
)

type cachedMemberState struct {
	roles        []string
	nick         string
	timeoutUntil *time.Time
}

var (
	memberCacheMu sync.RWMutex
	memberCache   = make(map[string]cachedMemberState)
)

func getCachedMember(guildID, userID string) (cachedMemberState, bool) {
	memberCacheMu.RLock()
	defer memberCacheMu.RUnlock()
	state, ok := memberCache[guildID+":"+userID]
	return state, ok
}

func setCachedMember(guildID, userID string, roles []string, nick string, timeoutUntil *time.Time) {
	memberCacheMu.Lock()
	defer memberCacheMu.Unlock()
	rolesCopy := make([]string, len(roles))
	copy(rolesCopy, roles)
	var tCopy *time.Time
	if timeoutUntil != nil {
		t := *timeoutUntil
		tCopy = &t
	}
	memberCache[guildID+":"+userID] = cachedMemberState{
		roles:        rolesCopy,
		nick:         nick,
		timeoutUntil: tCopy,
	}
}

func deleteCachedMember(guildID, userID string) {
	memberCacheMu.Lock()
	defer memberCacheMu.Unlock()
	delete(memberCache, guildID+":"+userID)
}

func UpdateCachedNick(guildID, userID string, nick string) {
	memberCacheMu.Lock()
	defer memberCacheMu.Unlock()
	key := guildID + ":" + userID
	if state, ok := memberCache[key]; ok {
		state.nick = nick
		memberCache[key] = state
	} else {
		memberCache[key] = cachedMemberState{nick: nick}
	}
}

func isBotModerator(s *discordgo.Session, mod *discordgo.User) bool {
	if mod == nil || s == nil || s.State == nil || s.State.User == nil {
		return false
	}
	return mod.ID == s.State.User.ID
}

type auditLogCacheEntry struct {
	audit   *discordgo.GuildAuditLog
	fetched time.Time
}

var (
	auditLogCacheMu sync.Mutex
	auditLogCache   = make(map[string]auditLogCacheEntry)
)

func fetchRecentAuditLog(s *discordgo.Session, guildID string, action int, targetID string) (*discordgo.User, string) {
	cacheKey := fmt.Sprintf("%s:%d", guildID, action)

	auditLogCacheMu.Lock()
	cached, ok := auditLogCache[cacheKey]
	var audit *discordgo.GuildAuditLog
	var err error

	if ok && time.Since(cached.fetched) < 1500*time.Millisecond {
		audit = cached.audit
		auditLogCacheMu.Unlock()
	} else {
		auditLogCacheMu.Unlock()
		audit, err = s.GuildAuditLog(guildID, "", "", action, 5)
		if err == nil && audit != nil {
			auditLogCacheMu.Lock()
			auditLogCache[cacheKey] = auditLogCacheEntry{
				audit:   audit,
				fetched: time.Now(),
			}
			auditLogCacheMu.Unlock()
		}
	}

	if audit == nil {
		return nil, ""
	}

	now := time.Now()
	for _, entry := range audit.AuditLogEntries {
		if entry.TargetID == targetID {
			if entryTime, err := discordgo.SnowflakeTimestamp(entry.ID); err == nil {
				if now.Sub(entryTime) < 15*time.Second {
					for _, u := range audit.Users {
						if u.ID == entry.UserID {
							return u, entry.Reason
						}
					}
				}
			}
		}
	}
	return nil, ""
}

func OnGuildMemberUpdateForLog(db *database.DB) func(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
	return func(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
		if m.GuildID == "" || m.User == nil || db == nil {
			return
		}

		userCopy := *m.User
		rolesCopy := make([]string, len(m.Roles))
		copy(rolesCopy, m.Roles)
		nickCopy := m.Nick
		var timeoutCopy *time.Time
		if m.CommunicationDisabledUntil != nil {
			t := *m.CommunicationDisabledUntil
			timeoutCopy = &t
		}
		guildID := m.GuildID
		helpers.Spawn(func() {
			prevState, exists := getCachedMember(guildID, userCopy.ID)
			defer setCachedMember(guildID, userCopy.ID, rolesCopy, nickCopy, timeoutCopy)

			if !exists {
				return
			}

			oldRoles := make(map[string]bool, len(prevState.roles))
			for _, rID := range prevState.roles {
				oldRoles[rID] = true
			}

			newRoles := make(map[string]bool, len(rolesCopy))
			for _, rID := range rolesCopy {
				newRoles[rID] = true
			}

			var cachedMod *discordgo.User
			var modResolved bool

			getMod := func() *discordgo.User {
				if !modResolved {
					cachedMod, _ = fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionMemberRoleUpdate), userCopy.ID)
					modResolved = true
				}
				return cachedMod
			}

			for _, rID := range rolesCopy {
				if !oldRoles[rID] {
					mod := getMod()
					if !isBotModerator(s, mod) {
						logger.Debugf("[MODLOG] Role %s added to user %s by %v", rID, userCopy.String(), mod)
						modlog.Log(s, db, &modlog.MemberRoleUpdateEvent{
							GuildID:    guildID,
							TargetUser: &userCopy,
							RoleID:     rID,
							IsAdd:      true,
							Moderator:  mod,
						})
					}
				}
			}

			for _, rID := range prevState.roles {
				if !newRoles[rID] {
					mod := getMod()
					if !isBotModerator(s, mod) {
						logger.Debugf("[MODLOG] Role %s removed from user %s by %v", rID, userCopy.String(), mod)
						modlog.Log(s, db, &modlog.MemberRoleUpdateEvent{
							GuildID:    guildID,
							TargetUser: &userCopy,
							RoleID:     rID,
							IsAdd:      false,
							Moderator:  mod,
						})
					}
				}
			}

			if prevState.nick != nickCopy {
				logger.Debugf("[MODLOG] Nickname changed for user %s ('%s' -> '%s')", userCopy.String(), prevState.nick, nickCopy)
				modlog.Log(s, db, &modlog.NicknameChangeEvent{
					GuildID: guildID,
					User:    &userCopy,
					OldNick: prevState.nick,
					NewNick: nickCopy,
				})
			}

			now := time.Now()
			prevTimeoutActive := prevState.timeoutUntil != nil && prevState.timeoutUntil.After(now)
			currTimeoutActive := timeoutCopy != nil && timeoutCopy.After(now)

			if !prevTimeoutActive && currTimeoutActive {
				duration := timeoutCopy.Sub(now).Round(time.Second)
				mod, reason := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionMemberUpdate), userCopy.ID)
				if !isBotModerator(s, mod) {
					logger.Debugf("[MODLOG] Timeout issued for user %s by %v (%s)", userCopy.String(), mod, duration)
					modlog.Log(s, db, &modlog.PunishmentEvent{
						GuildID:    guildID,
						Action:     "Timeout",
						TargetUser: &userCopy,
						Moderator:  mod,
						Duration:   duration.String(),
						Reason:     reason,
					})
				}
			} else if prevTimeoutActive && !currTimeoutActive {
				mod, reason := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionMemberUpdate), userCopy.ID)
				if !isBotModerator(s, mod) {
					logger.Debugf("[MODLOG] Timeout removed for user %s by %v", userCopy.String(), mod)
					modlog.Log(s, db, &modlog.MemberAuditEvent{
						GuildID:   guildID,
						Action:    "Timeout Removed",
						User:      &userCopy,
						Moderator: mod,
						Reason:    reason,
					})
				}
			}
		})
	}
}

func OnGuildMemberAddForLog(db *database.DB) func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	return func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		if m.GuildID == "" || m.User == nil || db == nil {
			return
		}
		setCachedMember(m.GuildID, m.User.ID, m.Roles, m.Nick, m.CommunicationDisabledUntil)
	}
}

func OnGuildMemberRemoveForLog(db *database.DB) func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	return func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
		if m.GuildID == "" || m.User == nil || db == nil {
			return
		}

		userCopy := *m.User
		guildID := m.GuildID

		helpers.Spawn(func() {
			defer deleteCachedMember(guildID, userCopy.ID)

			mod, reason := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionMemberKick), userCopy.ID)
			if mod != nil {
				if isBotModerator(s, mod) {
					logger.Debugf("[MODLOG] User %s kicked by bot command; skipping duplicate listener log", userCopy.String())
					return
				}
				logger.Debugf("[MODLOG] User %s kicked from guild %s by %s (Reason: %s)", userCopy.String(), guildID, mod.String(), reason)
				modlog.Log(s, db, &modlog.MemberAuditEvent{
					GuildID:   guildID,
					Action:    "Member Kicked",
					User:      &userCopy,
					Moderator: mod,
					Reason:    reason,
				})
				return
			}

			modlog.Log(s, db, &modlog.MemberLeaveEvent{
				GuildID: guildID,
				User:    &userCopy,
			})
		})
	}
}

func OnGuildBanAddForLog(db *database.DB) func(s *discordgo.Session, b *discordgo.GuildBanAdd) {
	return func(s *discordgo.Session, b *discordgo.GuildBanAdd) {
		if b.GuildID == "" || b.User == nil || db == nil {
			return
		}
		userCopy := *b.User
		guildID := b.GuildID
		helpers.Spawn(func() {
			mod, reason := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionMemberBanAdd), userCopy.ID)
			if isBotModerator(s, mod) {
				logger.Debugf("[MODLOG] User %s banned by bot command; skipping duplicate listener log", userCopy.String())
				return
			}
			logger.Debugf("[MODLOG] User %s banned in guild %s by %v", userCopy.String(), guildID, mod)
			modlog.Log(s, db, &modlog.MemberAuditEvent{
				GuildID:   guildID,
				Action:    "Member Banned",
				User:      &userCopy,
				Moderator: mod,
				Reason:    reason,
			})
		})
	}
}

func OnGuildBanRemoveForLog(db *database.DB) func(s *discordgo.Session, b *discordgo.GuildBanRemove) {
	return func(s *discordgo.Session, b *discordgo.GuildBanRemove) {
		if b.GuildID == "" || b.User == nil || db == nil {
			return
		}
		userCopy := *b.User
		guildID := b.GuildID
		helpers.Spawn(func() {
			mod, reason := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionMemberBanRemove), userCopy.ID)
			if isBotModerator(s, mod) {
				logger.Debugf("[MODLOG] User %s unbanned by bot command; skipping duplicate listener log", userCopy.String())
				return
			}
			logger.Debugf("[MODLOG] User %s unbanned in guild %s by %v", userCopy.String(), guildID, mod)
			modlog.Log(s, db, &modlog.MemberAuditEvent{
				GuildID:   guildID,
				Action:    "Member Unbanned",
				User:      &userCopy,
				Moderator: mod,
				Reason:    reason,
			})
		})
	}
}

func OnVoiceStateUpdateForLog(db *database.DB) func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	return func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
		if v == nil || v.VoiceState == nil || v.GuildID == "" || v.UserID == "" || db == nil {
			return
		}

		joinedOrMoved := v.ChannelID != "" && (v.BeforeUpdate == nil || v.BeforeUpdate.ChannelID != v.ChannelID)
		leftOrMoved := v.BeforeUpdate != nil && v.BeforeUpdate.ChannelID != "" && v.BeforeUpdate.ChannelID != v.ChannelID
		if !joinedOrMoved && !leftOrMoved {
			return
		}

		user := helpers.GetVoiceStateUser(s, v)
		if user == nil {
			user = &discordgo.User{ID: v.UserID, Username: "Unknown User"}
		}

		if v.BeforeUpdate == nil && v.ChannelID != "" {
			modlog.Log(s, db, &modlog.VoiceEvent{
				GuildID:   v.GuildID,
				Action:    "Joined Voice Channel",
				User:      user,
				ChannelID: v.ChannelID,
			})
		} else if v.BeforeUpdate != nil && v.ChannelID == "" {
			modlog.Log(s, db, &modlog.VoiceEvent{
				GuildID:   v.GuildID,
				Action:    "Left Voice Channel",
				User:      user,
				ChannelID: v.BeforeUpdate.ChannelID,
			})
		} else if v.BeforeUpdate != nil && v.BeforeUpdate.ChannelID != v.ChannelID {
			details := fmt.Sprintf("Moved from <#%s> to <#%s>", v.BeforeUpdate.ChannelID, v.ChannelID)
			modlog.Log(s, db, &modlog.VoiceEvent{
				GuildID:   v.GuildID,
				Action:    details,
				User:      user,
				ChannelID: v.ChannelID,
			})
		}
	}
}

func OnGuildChannelCreateForLog(db *database.DB) func(s *discordgo.Session, c *discordgo.ChannelCreate) {
	return func(s *discordgo.Session, c *discordgo.ChannelCreate) {
		if c.Channel == nil || c.GuildID == "" || db == nil {
			return
		}

		guildID := c.GuildID
		channelID := c.ID
		channelName := c.Name

		helpers.Spawn(func() {
			mod, _ := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionChannelCreate), channelID)
			modlog.Log(s, db, &modlog.ChannelEvent{
				GuildID:     guildID,
				Action:      "Channel Created",
				ChannelName: channelName,
				ChannelID:   channelID,
				Moderator:   mod,
				Reason:      "New channel created",
			})
		})
	}
}

func OnGuildChannelDeleteForLog(db *database.DB) func(s *discordgo.Session, c *discordgo.ChannelDelete) {
	return func(s *discordgo.Session, c *discordgo.ChannelDelete) {
		if c.Channel == nil || c.GuildID == "" || db == nil {
			return
		}

		guildID := c.GuildID
		channelID := c.ID
		channelName := c.Name

		helpers.Spawn(func() {
			mod, _ := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionChannelDelete), channelID)
			modlog.Log(s, db, &modlog.ChannelEvent{
				GuildID:     guildID,
				Action:      "Channel Deleted",
				ChannelName: channelName,
				ChannelID:   channelID,
				Moderator:   mod,
				Reason:      "Channel deleted",
			})
		})
	}
}

func OnGuildRoleCreateForLog(db *database.DB) func(s *discordgo.Session, r *discordgo.GuildRoleCreate) {
	return func(s *discordgo.Session, r *discordgo.GuildRoleCreate) {
		if r.GuildID == "" || r.Role == nil || db == nil {
			return
		}

		guildID := r.GuildID
		roleCopy := *r.Role

		helpers.Spawn(func() {
			mod, _ := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionRoleCreate), roleCopy.ID)
			modlog.Log(s, db, &modlog.RoleEvent{
				GuildID:   guildID,
				Action:    "Role Created",
				Role:      &roleCopy,
				Moderator: mod,
			})
		})
	}
}

func OnGuildRoleDeleteForLog(db *database.DB) func(s *discordgo.Session, r *discordgo.GuildRoleDelete) {
	return func(s *discordgo.Session, r *discordgo.GuildRoleDelete) {
		if r.GuildID == "" || db == nil {
			return
		}

		guildID := r.GuildID
		roleID := r.RoleID

		helpers.Spawn(func() {
			mod, _ := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionRoleDelete), roleID)
			modlog.Log(s, db, &modlog.RoleEvent{
				GuildID:   guildID,
				Action:    "Role Deleted",
				Role:      &discordgo.Role{ID: roleID},
				Moderator: mod,
			})
		})
	}
}

func OnGuildRoleUpdateForLog(db *database.DB) func(s *discordgo.Session, r *discordgo.GuildRoleUpdate) {
	return func(s *discordgo.Session, r *discordgo.GuildRoleUpdate) {
		if r.GuildRole == nil || r.Role == nil || r.GuildID == "" || db == nil {
			return
		}

		guildID := r.GuildID
		roleCopy := *r.Role
		var changes []string

		if r.BeforeUpdate != nil {
			if r.BeforeUpdate.Name != r.Role.Name {
				changes = append(changes, fmt.Sprintf("Name: `%s` > `%s`", r.BeforeUpdate.Name, r.Role.Name))
			}
			if r.BeforeUpdate.Color != r.Role.Color {
				changes = append(changes, fmt.Sprintf("Color: `#%06X` > `#%06X`", r.BeforeUpdate.Color, r.Role.Color))
			}
			if r.BeforeUpdate.Hoist != r.Role.Hoist {
				changes = append(changes, fmt.Sprintf("Hoisted: `%v` > `%v`", r.BeforeUpdate.Hoist, r.Role.Hoist))
			}
			if r.BeforeUpdate.Mentionable != r.Role.Mentionable {
				changes = append(changes, fmt.Sprintf("Mentionable: `%v` > `%v`", r.BeforeUpdate.Mentionable, r.Role.Mentionable))
			}
			if r.BeforeUpdate.Permissions != r.Role.Permissions {
				var permDiffs []string
				adminGained := (r.Role.Permissions&discordgo.PermissionAdministrator != 0) && (r.BeforeUpdate.Permissions&discordgo.PermissionAdministrator == 0)
				adminLost := (r.Role.Permissions&discordgo.PermissionAdministrator == 0) && (r.BeforeUpdate.Permissions&discordgo.PermissionAdministrator != 0)
				if adminGained {
					permDiffs = append(permDiffs, "Granted **Administrator**")
				} else if adminLost {
					permDiffs = append(permDiffs, "Revoked **Administrator**")
				}
				if len(permDiffs) > 0 {
					changes = append(changes, strings.Join(permDiffs, ", "))
				} else {
					changes = append(changes, "Permissions modified")
				}
			}
		}

		helpers.Spawn(func() {
			mod, reason := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionRoleUpdate), roleCopy.ID)
			details := "Role settings updated"
			if len(changes) > 0 {
				details = strings.Join(changes, "\n> ")
			}
			if reason != "" {
				details += fmt.Sprintf("\n> Reason: %s", reason)
			}
			modlog.Log(s, db, &modlog.RoleEvent{
				GuildID:   guildID,
				Action:    "Role Updated",
				Role:      &roleCopy,
				Moderator: mod,
				Details:   details,
			})
		})
	}
}

func OnGuildEmojisUpdateForLog(db *database.DB) func(s *discordgo.Session, e *discordgo.GuildEmojisUpdate) {
	return func(s *discordgo.Session, e *discordgo.GuildEmojisUpdate) {
		if e == nil || e.GuildID == "" || db == nil {
			return
		}

		guildID := e.GuildID
		helpers.Spawn(func() {
			for _, action := range []int{
				int(discordgo.AuditLogActionEmojiCreate),
				int(discordgo.AuditLogActionEmojiDelete),
				int(discordgo.AuditLogActionEmojiUpdate),
			} {
				mod, reason := fetchRecentAuditLog(s, guildID, action, "")
				if mod != nil && !isBotModerator(s, mod) {
					actionLabel := "Emoji Created"
					if action == int(discordgo.AuditLogActionEmojiDelete) {
						actionLabel = "Emoji Deleted"
					} else if action == int(discordgo.AuditLogActionEmojiUpdate) {
						actionLabel = "Emoji Updated"
					}
					modlog.Log(s, db, &modlog.EmojiEvent{
						GuildID:   guildID,
						Action:    actionLabel,
						Moderator: mod,
						Details:   reason,
					})
					return
				}
			}
		})
	}
}

func OnGuildInviteCreateForLog(db *database.DB) func(s *discordgo.Session, i *discordgo.InviteCreate) {
	return func(s *discordgo.Session, i *discordgo.InviteCreate) {
		if i.GuildID == "" || db == nil {
			return
		}
		creator := i.Inviter
		details := fmt.Sprintf("Code: `%s` | Max Uses: %d | Max Age: %ds | Temp: %v", i.Code, i.MaxUses, i.MaxAge, i.Temporary)
		modlog.Log(s, db, &modlog.ChannelEvent{
			GuildID:   i.GuildID,
			Action:    "Invite Created",
			ChannelID: i.ChannelID,
			Moderator: creator,
			Reason:    details,
		})
	}
}

func OnGuildInviteDeleteForLog(db *database.DB) func(s *discordgo.Session, i *discordgo.InviteDelete) {
	return func(s *discordgo.Session, i *discordgo.InviteDelete) {
		if i.GuildID == "" || db == nil {
			return
		}

		guildID := i.GuildID
		code := i.Code
		channelID := i.ChannelID

		helpers.Spawn(func() {
			mod, _ := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionInviteDelete), code)
			details := fmt.Sprintf("Invite code `%s` was deleted from <#%s>", code, channelID)
			modlog.Log(s, db, &modlog.ChannelEvent{
				GuildID:   guildID,
				Action:    "Invite Deleted",
				ChannelID: channelID,
				Moderator: mod,
				Reason:    details,
			})
		})
	}
}

func OnGuildChannelUpdateForLog(db *database.DB) func(s *discordgo.Session, c *discordgo.ChannelUpdate) {
	return func(s *discordgo.Session, c *discordgo.ChannelUpdate) {
		if c.Channel == nil || c.GuildID == "" || db == nil {
			return
		}

		guildID := c.GuildID
		channelID := c.ID
		channelName := c.Name

		var changes []string
		if c.BeforeUpdate != nil {
			if c.BeforeUpdate.Name != c.Name {
				changes = append(changes, fmt.Sprintf("Name: `%s` > `%s`", c.BeforeUpdate.Name, c.Name))
			}
			if c.BeforeUpdate.Topic != c.Topic {
				changes = append(changes, "Topic updated")
			}
			if c.BeforeUpdate.RateLimitPerUser != c.RateLimitPerUser {
				changes = append(changes, fmt.Sprintf("Slowmode: `%ds` > `%ds`", c.BeforeUpdate.RateLimitPerUser, c.RateLimitPerUser))
			}
			if c.BeforeUpdate.NSFW != c.NSFW {
				changes = append(changes, fmt.Sprintf("NSFW: `%v` > `%v`", c.BeforeUpdate.NSFW, c.NSFW))
			}
		}

		helpers.Spawn(func() {
			mod, reason := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionChannelUpdate), channelID)
			if mod == nil {
				mod, reason = fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionChannelOverwriteUpdate), channelID)
			}

			details := "Channel settings updated"
			if len(changes) > 0 {
				details = strings.Join(changes, "\n> ")
			}
			if reason != "" {
				details += fmt.Sprintf("\n> Reason: %s", reason)
			}

			modlog.Log(s, db, &modlog.ChannelEvent{
				GuildID:     guildID,
				Action:      "Channel Updated",
				ChannelName: channelName,
				ChannelID:   channelID,
				Moderator:   mod,
				Reason:      details,
			})
		})
	}
}

func OnGuildUpdateForLog(db *database.DB) func(s *discordgo.Session, g *discordgo.GuildUpdate) {
	return func(s *discordgo.Session, g *discordgo.GuildUpdate) {
		if g.Guild == nil || g.ID == "" || db == nil {
			return
		}

		guildID := g.ID
		guildName := g.Name

		helpers.Spawn(func() {
			mod, reason := fetchRecentAuditLog(s, guildID, int(discordgo.AuditLogActionGuildUpdate), guildID)

			details := fmt.Sprintf("Server settings for **%s** were modified", guildName)
			if reason != "" {
				details += fmt.Sprintf("\n> Reason: %s", reason)
			}

			modlog.Log(s, db, &modlog.ChannelEvent{
				GuildID:     guildID,
				Action:      "Server Updated",
				ChannelName: guildName,
				Moderator:   mod,
				Reason:      details,
			})
		})
	}
}
