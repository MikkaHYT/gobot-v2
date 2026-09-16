package listeners

import (
	"gobot/internal/database"

	"github.com/bwmarrin/discordgo"
)

func OnMessageReactionAdd(db *database.DB) func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	return func(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		if r.GuildID == "" || r.UserID == "" {
			return
		}

		if r.Member != nil && r.Member.User != nil && r.Member.User.Bot {
			return
		}

		isDisabled, err := db.GetGuildSettingBool(r.GuildID, database.SettingSelfReactDisabled)
		if err != nil || !isDisabled {
			return
		}

		msg, err := s.State.Message(r.ChannelID, r.MessageID)
		if err != nil || msg == nil {
			msg, err = s.ChannelMessage(r.ChannelID, r.MessageID)
			if err != nil || msg == nil || msg.Author == nil {
				return
			}
		} else if msg.Author == nil {
			return
		}

		if msg.Author.ID == r.UserID {
			_ = s.MessageReactionRemove(r.ChannelID, r.MessageID, r.Emoji.APIName(), r.UserID)
		}
	}
}
