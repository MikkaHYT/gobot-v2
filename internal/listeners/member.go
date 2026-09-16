package listeners

import (
	"context"

	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"
	"gobot/internal/modlog"
	"gobot/internal/roles"

	"github.com/bwmarrin/discordgo"
)

const adminPermFlags = discordgo.PermissionAdministrator |
	discordgo.PermissionManageGuild |
	discordgo.PermissionManageRoles |
	discordgo.PermissionKickMembers |
	discordgo.PermissionBanMembers |
	discordgo.PermissionManageMessages |
	discordgo.PermissionManageChannels |
	discordgo.PermissionManageWebhooks |
	discordgo.PermissionManageGuildExpressions

func fetchGuildRolesMap(s *discordgo.Session, guildID string) map[string]*discordgo.Role {
	rolesMap := make(map[string]*discordgo.Role)
	if guild, err := helpers.GetGuild(s, guildID); err == nil && guild != nil && len(guild.Roles) > 0 {
		for _, r := range guild.Roles {
			rolesMap[r.ID] = r
		}
	} else if gRoles, err := s.GuildRoles(guildID); err == nil {
		for _, r := range gRoles {
			rolesMap[r.ID] = r
		}
	}
	return rolesMap
}

func OnGuildMemberAdd(db *database.DB) func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
	return func(s *discordgo.Session, m *discordgo.GuildMemberAdd) {
		if m == nil || m.User == nil || m.GuildID == "" {
			return
		}

		cfg, _ := db.GetGuildConfig(m.GuildID)

		var isJailed bool
		if jailed, errJ := db.IsUserJailed(m.GuildID, m.User.ID); errJ == nil {
			isJailed = jailed
		} else {
			logger.Errorf("[JAIL] Failed to query jail status on member join for %s: %v", m.User.String(), errJ)
		}

		roleMgr := roles.DefaultRoleManager(s, db)
		var autoroleIDs []string
		var jailRoleID string
		var muteRoleID string
		if cfg != nil {
			autoroleIDs = cfg.AutoroleIDs
			jailRoleID = cfg.JailRoleID
			muteRoleID = cfg.MuteRoleID
		}
		var savedRoleIDs []string
		if saved, errS := db.GetSavedUserRoles(m.GuildID, m.User.ID); errS == nil {
			savedRoleIDs = saved
		}

		restoredRoles, _ := roleMgr.RestoreOnRejoin(context.Background(), roles.RejoinRequest{
			GuildID:        m.GuildID,
			UserID:         m.User.ID,
			IsJailed:       isJailed,
			JailRoleID:     jailRoleID,
			AutoroleIDs:    autoroleIDs,
			SavedRoleIDs:   savedRoleIDs,
			AdminPermFlags: adminPermFlags,
			MuteRoleID:     muteRoleID,
		})

		setCachedMember(m.GuildID, m.User.ID, m.Roles, m.Nick, m.CommunicationDisabledUntil)

		modlog.Log(s, db, &modlog.MemberJoinEvent{
			GuildID:       m.GuildID,
			User:          m.User,
			RestoredRoles: restoredRoles,
		})

		if !m.User.Bot {
			go dispatchWelcome(s, db, m.Member, m.GuildID)
		}
	}
}

func OnGuildMemberRemove(db *database.DB) func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	return func(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
		if m == nil || m.User == nil || m.GuildID == "" {
			return
		}

		if !m.User.Bot {
			go dispatchGoodbye(s, db, m.User, m.Member, m.GuildID)
		}

		cfg, _ := db.GetGuildConfig(m.GuildID)

		var rolesToSave []string
		if m.Member != nil && len(m.Member.Roles) > 0 {
			rolesToSave = m.Member.Roles
		} else if cached, ok := getCachedMember(m.GuildID, m.User.ID); ok && len(cached.roles) > 0 {
			rolesToSave = cached.roles
		}

		rolesMap := fetchGuildRolesMap(s, m.GuildID)

		var cleanRoles []string
		for _, roleID := range rolesToSave {
			if roleID == "" || roleID == m.GuildID {
				continue
			}

			if cfg != nil && (roleID == cfg.MuteRoleID || roleID == cfg.JailRoleID) {
				continue
			}

			if r, exists := rolesMap[roleID]; exists {
				if r.Permissions&adminPermFlags != 0 {
					logger.Debugf("[ROLES] Skipping admin role %s (%s) on save for %s", roleID, r.Name, m.User.String())
					continue
				}
			}

			cleanRoles = append(cleanRoles, roleID)
		}

		if len(cleanRoles) > 0 {
			if err := db.SaveUserRoles(m.GuildID, m.User.ID, cleanRoles); err != nil {
				logger.Warnf("[ROLES] Failed to save roles for %s: %v", m.User.String(), err)
			} else {
				logger.Debugf("[ROLES] Saved %d roles for user %s on leave", len(cleanRoles), m.User.String())
			}
		}
	}
}

func dispatchWelcome(s *discordgo.Session, db *database.DB, m *discordgo.Member, guildID string) {
	if s == nil || db == nil || m == nil || m.User == nil || m.User.Bot || guildID == "" {
		return
	}

	cfg, err := db.GetGuildConfig(guildID)
	if err != nil || cfg == nil {
		return
	}

	if cfg.WelcomeChannelID == "" && !cfg.WelcomeDMEnabled {
		return
	}

	guild, _ := helpers.GetGuild(s, guildID)

	if cfg.WelcomeChannelID != "" {
		if cfg.WelcomeIsEmbed && cfg.WelcomeEmbedJSON != "" {
			if parsed, _, err := LoadEmbedFromJSON(cfg.WelcomeEmbedJSON); err == nil && parsed != nil {
				rendered := helpers.FormatAnnouncementEmbed(parsed, m, guild)
				if _, errSend := s.ChannelMessageSendEmbed(cfg.WelcomeChannelID, rendered); errSend != nil {
					logger.Debugf("[WELCOME] Failed to send welcome embed to %s: %v", cfg.WelcomeChannelID, errSend)
				}
			} else if err != nil {
				logger.Debugf("[WELCOME] Failed to parse welcome embed for %s: %v", guildID, err)
			}
		} else {
			text := cfg.WelcomeMessage
			if text == "" {
				text = "Welcome {mention} to {server}!"
			}
			rendered := helpers.FormatAnnouncementVariables(text, m, guild)
			if _, errSend := s.ChannelMessageSend(cfg.WelcomeChannelID, rendered); errSend != nil {
				logger.Debugf("[WELCOME] Failed to send welcome message to %s: %v", cfg.WelcomeChannelID, errSend)
			}
		}
	}

	if cfg.WelcomeDMEnabled {
		dmChannel, err := s.UserChannelCreate(m.User.ID)
		if err != nil {
			logger.Debugf("[WELCOME] Failed to open DM with %s: %v", m.User.String(), err)
			return
		}

		if cfg.WelcomeDMIsEmbed && cfg.WelcomeDMEmbedJSON != "" {
			if parsed, _, err := LoadEmbedFromJSON(cfg.WelcomeDMEmbedJSON); err == nil && parsed != nil {
				rendered := helpers.FormatAnnouncementEmbed(parsed, m, guild)
				if _, errSend := s.ChannelMessageSendEmbed(dmChannel.ID, rendered); errSend != nil {
					logger.Debugf("[WELCOME] Failed to send welcome DM embed to %s: %v", m.User.String(), errSend)
				}
			} else if err != nil {
				logger.Debugf("[WELCOME] Failed to parse welcome DM embed for %s: %v", guildID, err)
			}
		} else {
			text := cfg.WelcomeDMMessage
			if text == "" {
				text = cfg.WelcomeMessage
			}
			if text == "" {
				text = "Welcome to {server}, {user.name}!"
			}
			rendered := helpers.FormatAnnouncementVariables(text, m, guild)
			if _, errSend := s.ChannelMessageSend(dmChannel.ID, rendered); errSend != nil {
				logger.Debugf("[WELCOME] Failed to send welcome DM to %s: %v", m.User.String(), errSend)
			}
		}
	}
}

func dispatchGoodbye(s *discordgo.Session, db *database.DB, user *discordgo.User, member *discordgo.Member, guildID string) {
	if s == nil || db == nil || user == nil || user.Bot || guildID == "" {
		return
	}

	cfg, err := db.GetGuildConfig(guildID)
	if err != nil || cfg == nil || cfg.GoodbyeChannelID == "" {
		return
	}

	if member == nil {
		member = &discordgo.Member{
			User:    user,
			GuildID: guildID,
		}
	}

	guild, _ := helpers.GetGuild(s, guildID)

	if cfg.GoodbyeIsEmbed && cfg.GoodbyeEmbedJSON != "" {
		if parsed, _, err := LoadEmbedFromJSON(cfg.GoodbyeEmbedJSON); err == nil && parsed != nil {
			rendered := helpers.FormatAnnouncementEmbed(parsed, member, guild)
			if _, errSend := s.ChannelMessageSendEmbed(cfg.GoodbyeChannelID, rendered); errSend != nil {
				logger.Debugf("[GOODBYE] Failed to send goodbye embed to %s: %v", cfg.GoodbyeChannelID, errSend)
			}
		} else if err != nil {
			logger.Debugf("[GOODBYE] Failed to parse goodbye embed for %s: %v", guildID, err)
		}
	} else {
		text := cfg.GoodbyeMessage
		if text == "" {
			text = "{user} has left {server}."
		}
		rendered := helpers.FormatAnnouncementVariables(text, member, guild)
		if _, errSend := s.ChannelMessageSend(cfg.GoodbyeChannelID, rendered); errSend != nil {
			logger.Debugf("[GOODBYE] Failed to send goodbye message to %s: %v", cfg.GoodbyeChannelID, errSend)
		}
	}
}

func OnGuildMemberUpdateNickname(db *database.DB) func(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
	return func(s *discordgo.Session, m *discordgo.GuildMemberUpdate) {
		if m == nil || m.User == nil || m.User.Bot || m.GuildID == "" || db == nil {
			return
		}

		lockedNick, err := db.GetLockedNickname(m.GuildID, m.User.ID)
		if err != nil || lockedNick == "" {
			return
		}

		if m.Nick != lockedNick {
			if err := s.GuildMemberNickname(m.GuildID, m.User.ID, lockedNick); err != nil {
				logger.Warnf("[FORCENICK] Failed to revert locked nickname for %s in %s: %v", m.User.String(), m.GuildID, err)
			}
		}
	}
}

func ProcessExpiredTempRoles(ctx context.Context, s *discordgo.Session, db *database.DB) {
	if db == nil || s == nil {
		return
	}

	roleMgr := roles.DefaultRoleManager(s, db)
	if count, err := roleMgr.SweepExpired(ctx); err != nil {
		logger.Warnf("[TEMPROLES] Error during expired temp role sweep: %v", err)
	} else if count > 0 {
		logger.Infof("[TEMPROLES] Swept %d expired temporary role(s)", count)
	}
}
