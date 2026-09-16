package listeners

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

func OnAFKMessageCreate(db *database.DB, defaultPrefix ...string) func(s *discordgo.Session, m *discordgo.MessageCreate) {
	prefix := ","
	if len(defaultPrefix) > 0 && defaultPrefix[0] != "" {
		prefix = defaultPrefix[0]
	}
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil || m.Author.Bot || m.GuildID == "" || db == nil {
			return
		}
		effectivePrefix := prefix
		guildPrefix, _ := db.GetGuildSettingString(m.GuildID, database.SettingPrefix)
		if guildPrefix != "" {
			effectivePrefix = guildPrefix
		}
		isAfkCmd := isAFKCommand(m.Content, effectivePrefix)

		if !isAfkCmd {
			if afk, err := db.GetUserAFKInfo(m.GuildID, m.Author.ID); err == nil && afk != nil {
				if time.Since(afk.SinceTimestamp) >= helpers.DurationFeedbackShort {
					if errRem := db.RemoveUserAFK(m.GuildID, m.Author.ID); errRem != nil {
						logger.Warnf("[AFK] Failed to remove AFK record for user %s in guild %s: %v", m.Author.ID, m.GuildID, errRem)
					} else {
						durationStr := helpers.FormatDuration(time.Since(afk.SinceTimestamp))
						embed := &discordgo.MessageEmbed{
							Description: fmt.Sprintf("Welcome back %s, you were AFK for **%s**.", m.Author.Mention(), durationStr),
							Color:       helpers.ColorDefault,
						}
						_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
					}
				}
			}
		}

		if len(m.Mentions) > 0 {
			seen := make(map[string]bool)
			var afkNotifications []string

			for _, mentionedUser := range m.Mentions {
				if mentionedUser == nil || mentionedUser.ID == m.Author.ID || seen[mentionedUser.ID] {
					continue
				}
				seen[mentionedUser.ID] = true

				if afk, err := db.GetUserAFKInfo(m.GuildID, mentionedUser.ID); err == nil && afk != nil {
					durationStr := helpers.FormatDuration(time.Since(afk.SinceTimestamp))
					afkNotifications = append(afkNotifications, fmt.Sprintf("**%s** is AFK: **%s** (%s ago)", mentionedUser.Username, afk.Reason, durationStr))
				}
			}

			if len(afkNotifications) > 0 {
				embed := &discordgo.MessageEmbed{
					Description: strings.Join(afkNotifications, "\n"),
					Color:       helpers.ColorDefault,
				}
				_, _ = s.ChannelMessageSendEmbed(m.ChannelID, embed)
			}
		}
	}
}

func isAFKCommand(content, prefix string) bool {
	clean := strings.TrimSpace(content)
	if prefix == "" || !strings.HasPrefix(clean, prefix) {
		return false
	}
	parts := strings.Fields(strings.TrimSpace(strings.TrimPrefix(clean, prefix)))
	return len(parts) > 0 && (strings.EqualFold(parts[0], "afk") || strings.EqualFold(parts[0], "away"))
}
