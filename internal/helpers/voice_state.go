package helpers

import "github.com/bwmarrin/discordgo"

type VoiceParticipant struct {
	UserID    string
	ChannelID string
	Bot       bool
}

func SnapshotGuildVoiceState(s *discordgo.Session, guildID string) (string, []VoiceParticipant, bool) {
	if s == nil || s.State == nil {
		return "", nil, false
	}
	state := s.State
	guild, err := state.Guild(guildID)
	state.RLock()
	defer state.RUnlock()
	botID := ""
	if state.User != nil {
		botID = state.User.ID
	}
	if err != nil || guild == nil {
		return botID, nil, false
	}
	bots := make(map[string]bool)
	for _, member := range guild.Members {
		if member != nil && member.User != nil {
			bots[member.User.ID] = member.User.Bot
		}
	}
	participants := make([]VoiceParticipant, 0, len(guild.VoiceStates))
	for _, vs := range guild.VoiceStates {
		if vs != nil {
			participants = append(participants, VoiceParticipant{
				UserID: vs.UserID, ChannelID: vs.ChannelID, Bot: bots[vs.UserID],
			})
		}
	}
	return botID, participants, true
}
