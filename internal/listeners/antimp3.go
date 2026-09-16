package listeners

import (
	"fmt"
	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/modlog"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func isAudioAttachment(att *discordgo.MessageAttachment, strict bool) bool {
	if att == nil {
		return false
	}

	if att.ContentType != "" && strings.HasPrefix(strings.ToLower(att.ContentType), "audio/") {
		return true
	}

	cleanName := strings.ToLower(strings.TrimSpace(att.Filename))
	if helpers.IsAudioExtension(cleanName) {
		return true
	}

	if strict {
		parts := strings.Split(cleanName, ".")
		if len(parts) > 2 {
			for _, part := range parts[1:] {
				if helpers.SupportedAudioExtensions["."+part] {
					return true
				}
			}
		}
	}
	return false
}

func isRadioPlayCommand(content, serverPrefix string) bool {
	clean := strings.TrimSpace(strings.ToLower(content))
	if clean == "" {
		return false
	}

	if serverPrefix != "" {
		lowerPrefix := strings.ToLower(serverPrefix)
		if strings.HasPrefix(clean, lowerPrefix) {
			withoutPrefix := strings.TrimSpace(clean[len(lowerPrefix):])
			fields := strings.Fields(withoutPrefix)
			if len(fields) > 0 {
				switch fields[0] {
				case "play", "p", "playfile", "pf", "playlist", "pl", "playnext", "pn", "radio", "r":
					return true
				}
			}
		}
	}

	clean = strings.TrimLeft(clean, "?,!.;$/-~+ ")

	fields := strings.Fields(clean)
	if len(fields) == 0 {
		return false
	}

	cmd := fields[0]

	switch cmd {
	case "play", "p", "playfile", "pf", "playlist", "pl", "playnext", "pn", "radio", "r":
		return true
	}

	return false
}

func OnMessageCreateForAntiMP3(db *database.DB) func(s *discordgo.Session, m *discordgo.MessageCreate) {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m == nil || m.Author == nil || m.Author.Bot || m.GuildID == "" || db == nil {
			return
		}

		if len(m.Attachments) == 0 {
			return
		}

		mode, err := db.GetGuildSettingString(m.GuildID, database.SettingAntiMP3Mode)
		if err != nil || mode == "" || mode == "disable" {
			return
		}

		serverPrefix, _ := db.GetGuildSettingString(m.GuildID, database.SettingPrefix)
		if serverPrefix == "" {
			serverPrefix = ","
		}

		if isRadioPlayCommand(m.Content, serverPrefix) {
			return
		}

		if mode == "normal" {
			perms, errP := s.UserChannelPermissions(m.Author.ID, m.ChannelID)
			if errP == nil {
				if perms&discordgo.PermissionAdministrator != 0 || perms&discordgo.PermissionManageMessages != 0 || perms&discordgo.PermissionManageGuild != 0 {
					return
				}
			}
			if s.State != nil {
				guild, errG := s.State.Guild(m.GuildID)
				isOwner := errG == nil && guild != nil && guild.OwnerID == m.Author.ID
				if isOwner {
					return
				}
			}
		}

		strict := mode == "strict"
		hasAudio := false
		var matchedFilename string

		for _, att := range m.Attachments {
			if isAudioAttachment(att, strict) {
				hasAudio = true
				matchedFilename = att.Filename
				break
			}
		}

		if !hasAudio {
			return
		}

		_ = s.ChannelMessageDelete(m.ChannelID, m.ID)

		modlog.Log(s, db, &modlog.MemberAuditEvent{
			GuildID:   m.GuildID,
			Action:    "Audio Upload Deleted (Anti-MP3)",
			User:      m.Author,
			Moderator: nil,
			Reason:    fmt.Sprintf("Deleted attachment: %s", matchedFilename),
		})
	}
}
