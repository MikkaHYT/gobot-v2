package listeners

import (
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

func OnGuildChannelCreate(db *database.DB) func(s *discordgo.Session, c *discordgo.ChannelCreate) {
	return func(s *discordgo.Session, c *discordgo.ChannelCreate) {
		if c.Channel == nil || c.GuildID == "" || db == nil {
			return
		}

		ch := c.Channel
		if ch.Type != discordgo.ChannelTypeGuildText && ch.Type != discordgo.ChannelTypeGuildVoice && ch.Type != discordgo.ChannelTypeGuildCategory && ch.Type != discordgo.ChannelTypeGuildNews {
			return
		}

		cfg, err := db.GetGuildConfig(c.GuildID)
		if err != nil || cfg == nil {
			return
		}

		roles, err := s.GuildRoles(c.GuildID)
		if err != nil || len(roles) == 0 {
			return
		}

		validRoleMap := make(map[string]bool)
		for _, r := range roles {
			validRoleMap[r.ID] = true
		}

		logger.Debugf("[CHANNEL] Auto-applying punishment role overwrites to newly created channel #%s (%s)", ch.Name, ch.ID)

		if cfg.ImageMuteRoleID != "" && validRoleMap[cfg.ImageMuteRoleID] {
			if err := s.ChannelPermissionSet(ch.ID, cfg.ImageMuteRoleID, discordgo.PermissionOverwriteTypeRole, 0, helpers.ImageMuteDenyFlags); err != nil {
				logger.Warnf("[CHANNEL] Failed to set ImageMute overwrite on #%s: %v", ch.Name, err)
			}
		}
		if cfg.ReactionMuteRoleID != "" && validRoleMap[cfg.ReactionMuteRoleID] {
			if err := s.ChannelPermissionSet(ch.ID, cfg.ReactionMuteRoleID, discordgo.PermissionOverwriteTypeRole, 0, helpers.ReactionMuteDenyFlags); err != nil {
				logger.Warnf("[CHANNEL] Failed to set ReactionMute overwrite on #%s: %v", ch.Name, err)
			}
		}
		if cfg.MuteRoleID != "" && validRoleMap[cfg.MuteRoleID] {
			if err := s.ChannelPermissionSet(ch.ID, cfg.MuteRoleID, discordgo.PermissionOverwriteTypeRole, 0, helpers.MuteDenyFlags); err != nil {
				logger.Warnf("[CHANNEL] Failed to set Mute overwrite on #%s: %v", ch.Name, err)
			}
		}
		if cfg.JailRoleID != "" && validRoleMap[cfg.JailRoleID] {
			if err := s.ChannelPermissionSet(ch.ID, cfg.JailRoleID, discordgo.PermissionOverwriteTypeRole, 0, helpers.JailDenyFlags); err != nil {
				logger.Warnf("[CHANNEL] Failed to set Jail overwrite on #%s: %v", ch.Name, err)
			}
		}
	}
}
