package listeners

import (
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"gobot/internal/database"
	"gobot/internal/helpers"
	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

type messageLocker struct {
	shards [256]sync.Mutex
}

var locker messageLocker

func (l *messageLocker) getShard(messageID string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(messageID))
	return &l.shards[h.Sum32()%256]
}

func (l *messageLocker) Lock(messageID string) {
	l.getShard(messageID).Lock()
}

func (l *messageLocker) Unlock(messageID string) {
	l.getShard(messageID).Unlock()
}

func isStarEmoji(name string) bool {
	return name == "⭐"
}

func OnStarboardReactionAdd(db *database.DB) func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	return func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		if db == nil || r.GuildID == "" || !isStarEmoji(r.Emoji.Name) {
			return
		}

		starChanID, err := db.GetGuildSettingString(r.GuildID, database.SettingStarboardChannelID)
		if err != nil || starChanID == "" || starChanID == "auto" || r.ChannelID == starChanID {
			return
		}

		threshold, err := db.GetGuildSettingInt(r.GuildID, database.SettingStarboardThreshold)
		if err != nil || threshold <= 0 {
			threshold = 3
		}

		selfReactDisabled, _ := db.GetGuildSettingBool(r.GuildID, database.SettingSelfReactDisabled)

		locker.Lock(r.MessageID)
		defer locker.Unlock(r.MessageID)

		msg, err := s.State.Message(r.ChannelID, r.MessageID)
		if err != nil || msg == nil {
			msg, err = s.ChannelMessage(r.ChannelID, r.MessageID)
			if err != nil || msg == nil {
				return
			}
		}

		if selfReactDisabled && msg.Author != nil && msg.Author.ID == r.UserID {
			return
		}

		starCount := 0
		for _, rx := range msg.Reactions {
			if isStarEmoji(rx.Emoji.Name) {
				starCount = rx.Count
				break
			}
		}

		if selfReactDisabled && msg.Author != nil && starCount > 0 {
			starCount = countValidStarReactors(s, r.ChannelID, r.MessageID, msg.Author.ID, threshold)
		}

		if starCount < threshold {
			return
		}

		entry, errEntry := db.GetStarboardEntry(r.GuildID, r.MessageID)
		if errEntry != nil && !errors.Is(errEntry, database.ErrNotFound) {
			logger.Warnf("[STARBOARD] Failed to query existing starboard entry for message %s: %v", r.MessageID, errEntry)
			return
		}
		jumpURL := helpers.MessageURL(r.GuildID, r.ChannelID, r.MessageID)

		authorName := "Unknown User"
		authorAvatar := ""
		if msg.Author != nil {
			authorName = msg.Author.Username
			authorAvatar = helpers.UserAvatar(msg.Author)
		}

		description := msg.Content
		if description != "" {
			description += "\n\n"
		}
		description += fmt.Sprintf("[Jump to Message](%s)", jumpURL)

		embed := &discordgo.MessageEmbed{
			Description: description,
			Author: &discordgo.MessageEmbedAuthor{
				Name:    authorName,
				IconURL: authorAvatar,
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("ID: %s", msg.ID),
			},
			Timestamp: msg.Timestamp.Format(time.RFC3339),
			Color:     helpers.ColorStarboard,
		}

		for _, att := range msg.Attachments {
			if helpers.IsMediaURL(att.URL) {
				embed.Image = &discordgo.MessageEmbedImage{URL: att.URL}
				break
			}
		}

		if embed.Image == nil {
			for _, emb := range msg.Embeds {
				if emb.Image != nil && emb.Image.URL != "" {
					embed.Image = &discordgo.MessageEmbedImage{URL: emb.Image.URL}
					break
				}
			}
			if embed.Image == nil {
				for _, emb := range msg.Embeds {
					if emb.Thumbnail != nil && emb.Thumbnail.URL != "" {
						embed.Image = &discordgo.MessageEmbedImage{URL: emb.Thumbnail.URL}
						break
					}
				}
			}
		}

		contentHeader := fmt.Sprintf("⭐ **%d** | <#%s>", starCount, r.ChannelID)

		if entry != nil && entry.StarboardMessageID != "" {
			if _, errEdit := s.ChannelMessageEditComplex(&discordgo.MessageEdit{
				Channel: starChanID,
				ID:      entry.StarboardMessageID,
				Content: &contentHeader,
				Embeds:  &[]*discordgo.MessageEmbed{embed},
			}); errEdit != nil {
				logger.Warnf("[STARBOARD] Failed to update starboard message %s: %v", entry.StarboardMessageID, errEdit)
			}
			if errDB := db.SaveStarboardEntry(r.GuildID, r.MessageID, entry.StarboardMessageID, starCount); errDB != nil {
				logger.Warnf("[STARBOARD] Failed to save starboard entry: %v", errDB)
			}
		} else {
			starMsg, errSend := s.ChannelMessageSendComplex(starChanID, &discordgo.MessageSend{
				Content: contentHeader,
				Embeds:  []*discordgo.MessageEmbed{embed},
			})
			if errSend != nil {
				logger.Warnf("[STARBOARD] Failed to send starboard message to %s: %v", starChanID, errSend)
			} else if starMsg != nil {
				if errDB := db.SaveStarboardEntry(r.GuildID, r.MessageID, starMsg.ID, starCount); errDB != nil {
					logger.Warnf("[STARBOARD] Failed to save starboard entry: %v", errDB)
				}
			}
		}
	}
}

func OnStarboardReactionRemove(db *database.DB) func(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	return func(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
		if db == nil || r.GuildID == "" || !isStarEmoji(r.Emoji.Name) {
			return
		}

		starChanID, err := db.GetGuildSettingString(r.GuildID, database.SettingStarboardChannelID)
		if err != nil || starChanID == "" || starChanID == "auto" || r.ChannelID == starChanID {
			return
		}

		threshold, err := db.GetGuildSettingInt(r.GuildID, database.SettingStarboardThreshold)
		if err != nil || threshold <= 0 {
			threshold = 3
		}

		locker.Lock(r.MessageID)
		defer locker.Unlock(r.MessageID)

		entry, err := db.GetStarboardEntry(r.GuildID, r.MessageID)
		if err != nil || entry == nil || entry.StarboardMessageID == "" {
			return
		}

		msg, err := s.State.Message(r.ChannelID, r.MessageID)
		if err != nil || msg == nil {
			msg, _ = s.ChannelMessage(r.ChannelID, r.MessageID)
		}

		starCount := 0
		if msg != nil {
			for _, rx := range msg.Reactions {
				if isStarEmoji(rx.Emoji.Name) {
					starCount = rx.Count
					break
				}
			}
		}

		if starCount < threshold {
			if errDel := s.ChannelMessageDelete(starChanID, entry.StarboardMessageID); errDel != nil {
				logger.Debugf("[STARBOARD] Failed to delete starboard message %s: %v", entry.StarboardMessageID, errDel)
			}
			if errDB := db.DeleteStarboardEntry(r.GuildID, r.MessageID); errDB != nil {
				logger.Warnf("[STARBOARD] Failed to delete starboard DB entry: %v", errDB)
			}
		} else {
			contentHeader := fmt.Sprintf("⭐ **%d** | <#%s>", starCount, r.ChannelID)
			if _, errEdit := s.ChannelMessageEdit(starChanID, entry.StarboardMessageID, contentHeader); errEdit != nil {
				logger.Warnf("[STARBOARD] Failed to update starboard message %s: %v", entry.StarboardMessageID, errEdit)
			}
			if errDB := db.SaveStarboardEntry(r.GuildID, r.MessageID, entry.StarboardMessageID, starCount); errDB != nil {
				logger.Warnf("[STARBOARD] Failed to update starboard DB entry: %v", errDB)
			}
		}
	}
}

func countValidStarReactors(s *discordgo.Session, channelID, messageID, authorID string, targetThreshold int) int {
	validStars := 0
	afterID := ""
	for {
		reactors, err := s.MessageReactions(channelID, messageID, "⭐", 100, "", afterID)
		if err != nil || len(reactors) == 0 {
			break
		}
		for _, u := range reactors {
			if u.ID != authorID && !u.Bot {
				validStars++
			}
		}
		if validStars >= targetThreshold || len(reactors) < 100 {
			break
		}
		afterID = reactors[len(reactors)-1].ID
	}
	return validStars
}
