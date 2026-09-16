package voice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

func isDiscordNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) {
		if restErr.Response != nil && restErr.Response.StatusCode == 404 {
			return true
		}
		if restErr.Message != nil && restErr.Message.Code == 10003 {
			return true
		}
	}
	return false
}

func OnVoiceMasterUpdate(db *database.DB) func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	return func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
		if v == nil || v.VoiceState == nil || v.GuildID == "" || v.UserID == "" {
			return
		}

		joinedOrMoved := v.ChannelID != "" && (v.BeforeUpdate == nil || v.BeforeUpdate.ChannelID != v.ChannelID)
		leftOrMoved := v.BeforeUpdate != nil && v.BeforeUpdate.ChannelID != "" && v.BeforeUpdate.ChannelID != v.ChannelID
		if !joinedOrMoved && !leftOrMoved {
			return
		}

		user := helpers.GetVoiceStateUser(s, v)
		if joinedOrMoved && user != nil && !user.Bot {
			handleChannelJoin(s, db, v, user)
		}

		if leftOrMoved {
			handleChannelLeave(s, v.GuildID, v.BeforeUpdate.ChannelID)
		}
	}
}

func handleChannelJoin(s *discordgo.Session, db *database.DB, v *discordgo.VoiceStateUpdate, user *discordgo.User) {
	if db == nil || user == nil {
		return
	}

	triggerID, err := db.GetGuildSettingString(v.GuildID, database.SettingVoiceMasterTriggerChannelID)
	if err != nil || triggerID == "" || v.ChannelID != triggerID {
		return
	}

	categoryID, _ := db.GetGuildSettingString(v.GuildID, database.SettingVoiceMasterCategoryID)

	if GlobalManager.IsOnCooldown(v.GuildID, v.UserID) {
		bot.Debugf("[VOICEMASTER] User %s tried creating room on cooldown in guild %s", v.UserID, v.GuildID)
		return
	}

	if !GlobalManager.TryAcquireCreationLock(v.GuildID, v.UserID) {
		if existingChanID, exists := GlobalManager.GetUserOwnedChannel(v.GuildID, v.UserID); exists {
			if errMove := s.GuildMemberMove(v.GuildID, v.UserID, &existingChanID); errMove != nil {
				bot.Warnf("[VOICEMASTER] Failed to move user %s to existing channel %s: %v", v.UserID, existingChanID, errMove)
			}
		}
		return
	}
	defer GlobalManager.ReleaseCreationLock(v.GuildID, v.UserID)

	roomName := fmt.Sprintf("%s's Room", user.Username)
	userLimit := 0

	if settings, found := GlobalManager.GetUserSettings(v.GuildID, v.UserID); found && settings != nil {
		if settings.ChannelName != "" {
			roomName = settings.ChannelName
		}
		if settings.ChannelLimit > 0 {
			userLimit = settings.ChannelLimit
		}
	}

	newChan, err := s.GuildChannelCreateComplex(v.GuildID, discordgo.GuildChannelCreateData{
		Name:      roomName,
		Type:      discordgo.ChannelTypeGuildVoice,
		UserLimit: userLimit,
		ParentID:  categoryID,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{
				ID:    v.UserID,
				Type:  discordgo.PermissionOverwriteTypeMember,
				Allow: discordgo.PermissionManageChannels | discordgo.PermissionVoiceMoveMembers | discordgo.PermissionVoiceMuteMembers | discordgo.PermissionVoiceDeafenMembers,
			},
		},
	})
	if err != nil {
		bot.Errorf("[VOICEMASTER] Failed to create dynamic voice channel in guild %s: %v", v.GuildID, err)
		return
	}

	GlobalManager.TrackChannel(newChan.ID, v.UserID, v.GuildID)

	if errDB := db.SaveTempVoiceChannel(newChan.ID, v.GuildID, v.UserID); errDB != nil {
		bot.Errorf("[VOICEMASTER] Failed to persist temp voice channel %s to database: %v", newChan.ID, errDB)
	}

	GlobalManager.SetCooldown(v.GuildID, v.UserID, 5*time.Second)

	err = s.GuildMemberMove(v.GuildID, v.UserID, &newChan.ID)
	if err != nil {
		bot.Errorf("[VOICEMASTER] Failed to move user %s into newly created room %s: %v", v.UserID, newChan.ID, err)
		_, _ = s.ChannelDelete(newChan.ID)
		GlobalManager.RemoveChannel(newChan.ID)
		_ = db.DeleteTempVoiceChannel(newChan.ID)
		return
	}

	bot.Infof("[VOICEMASTER] User %s created temporary room '%s' (%s) in guild %s", v.UserID, roomName, newChan.ID, v.GuildID)
}

func InitVoiceMaster(ctx context.Context, s *discordgo.Session, db *database.DB) {
	if s == nil || db == nil {
		return
	}

	records, err := db.GetAllTempVoiceChannels()
	if err != nil || len(records) == 0 {
		return
	}

	for _, rec := range records {
		GlobalManager.TrackChannel(rec.ChannelID, rec.OwnerID, rec.GuildID)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	for _, rec := range records {
		if ctx.Err() != nil {
			return
		}
		_, err := s.Channel(rec.ChannelID)
		if err != nil {
			if isDiscordNotFoundError(err) {
				if errDel := db.DeleteTempVoiceChannel(rec.ChannelID); errDel != nil {
					bot.Warnf("[VOICEMASTER] Failed to delete orphaned record for channel %s: %v", rec.ChannelID, errDel)
				}
				GlobalManager.RemoveChannel(rec.ChannelID)
			} else {
				bot.Warnf("[VOICEMASTER] Transient error verifying channel %s during startup: %v (retaining tracking)", rec.ChannelID, err)
				GlobalManager.TrackChannel(rec.ChannelID, rec.OwnerID, rec.GuildID)
			}
			continue
		}

		GlobalManager.TrackChannel(rec.ChannelID, rec.OwnerID, rec.GuildID)
	}
}

func handleChannelLeave(s *discordgo.Session, guildID, oldChannelID string) {
	if !GlobalManager.IsActiveChannel(oldChannelID) {
		return
	}

	if s.State == nil {
		return
	}
	guild, err := s.State.Guild(guildID)
	if err != nil || guild == nil {
		return
	}

	humanMemberCount := 0
	botUserID := ""
	if s.State.User != nil {
		botUserID = s.State.User.ID
	}
	for _, vs := range guild.VoiceStates {
		if vs.ChannelID == oldChannelID {
			if vs.Member != nil && vs.Member.User != nil && vs.Member.User.Bot {
				continue
			}
			if botUserID != "" && vs.UserID == botUserID {
				continue
			}
			humanMemberCount++
		}
	}

	if humanMemberCount == 0 {
		_, err := s.ChannelDelete(oldChannelID)
		if err != nil {
			bot.Debugf("[VOICEMASTER] Failed to delete empty channel %s: %v", oldChannelID, err)
		}
		GlobalManager.RemoveChannel(oldChannelID)
		if GlobalDB != nil {
			if errDel := GlobalDB.DeleteTempVoiceChannel(oldChannelID); errDel != nil {
				bot.Warnf("[VOICEMASTER] Failed to delete temp voice channel record %s: %v", oldChannelID, errDel)
			}
		}
		bot.Infof("[VOICEMASTER] Deleted empty room %s in guild %s", oldChannelID, guildID)
	}
}
