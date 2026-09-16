package modlog

import (
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

func sendModLogCV2WithFiles(s *discordgo.Session, db *database.DB, guildID string, components []map[string]interface{}, files []*discordgo.File) {
	if s == nil || db == nil || guildID == "" || len(components) == 0 {
		return
	}

	modLogChannelID, err := db.GetGuildSettingString(guildID, database.SettingModLogChannelID)
	if err != nil || modLogChannelID == "" || modLogChannelID == "auto" {
		return
	}

	if err = helpers.SendCV2MessageWithFiles(s, modLogChannelID, components, files); err != nil {
		logger.Warnf("[MODLOG] Failed to send log to channel %s: %v", modLogChannelID, err)
	}
}

func Log(s *discordgo.Session, db *database.DB, event Event) {
	if s == nil || db == nil || event == nil {
		return
	}
	guildID := event.Guild()
	if guildID == "" {
		return
	}
	components, files := event.Build(s)
	if len(components) == 0 {
		return
	}
	sendModLogCV2WithFiles(s, db, guildID, components, files)
}
