package listeners

import (
	"context"

	"gobot/internal/database"
	"gobot/internal/logger"
	"gobot/internal/policy"
	"gobot/internal/roles"

	"github.com/bwmarrin/discordgo"
)

var DB *database.DB

func OnReactionRoleAdd(pol *policy.Module, db *database.DB) func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	return func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		if pol == nil || db == nil || r == nil || r.UserID == "" {
			return
		}
		if s.State != nil && s.State.User != nil && r.UserID == s.State.User.ID {
			return
		}
		if dec := pol.DecideUserAdmission(r.UserID); !dec.Allowed() {
			if dec.Unavailable() {
				logger.Errorf("[REACTION_ROLES] Global user policy lookup failed for %s; skipping role grant", r.UserID)
			}
			return
		}

		emojiKey := r.Emoji.APIName()
		roleID, err := db.GetReactionRoleID(r.GuildID, r.MessageID, emojiKey)
		if err != nil || roleID == "" {
			roleID, err = db.GetReactionRoleID(r.GuildID, r.MessageID, r.Emoji.Name)
		}

		if err == nil && roleID != "" {
			roleMgr := roles.DefaultRoleManager(s, db)
			if errAssign := roleMgr.Assign(context.Background(), roles.AssignRequest{
				GuildID:      r.GuildID,
				TargetUserID: r.UserID,
				RoleID:       roleID,
			}); errAssign != nil {
				logger.Warnf("[REACTION_ROLES] Failed to add role %s to user %s in guild %s: %v", roleID, r.UserID, r.GuildID, errAssign)
			}
		}
	}
}
func OnReactionRoleRemove(db *database.DB) func(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	return func(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
		if db == nil || r == nil || r.UserID == "" {
			return
		}
		if s.State != nil && s.State.User != nil && r.UserID == s.State.User.ID {
			return
		}

		emojiKey := r.Emoji.APIName()
		roleID, err := db.GetReactionRoleID(r.GuildID, r.MessageID, emojiKey)
		if err != nil || roleID == "" {
			roleID, err = db.GetReactionRoleID(r.GuildID, r.MessageID, r.Emoji.Name)
		}

		if err == nil && roleID != "" {
			roleMgr := roles.DefaultRoleManager(s, db)
			if errRevoke := roleMgr.Revoke(context.Background(), roles.RevokeRequest{
				GuildID:      r.GuildID,
				TargetUserID: r.UserID,
				RoleID:       roleID,
			}); errRevoke != nil {
				logger.Warnf("[REACTION_ROLES] Failed to remove role %s from user %s in guild %s: %v", roleID, r.UserID, r.GuildID, errRevoke)
			}
		}
	}
}
