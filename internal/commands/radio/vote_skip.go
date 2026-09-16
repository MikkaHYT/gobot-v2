package radio

import (
	"fmt"
	"sync"

	"gobot/internal/helpers"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

type voteSkipState struct {
	mu           sync.Mutex
	currentTrack map[string]string
	votes        map[string]map[string]bool
}

var globalVoteSkip = &voteSkipState{
	currentTrack: make(map[string]string),
	votes:        make(map[string]map[string]bool),
}

func ResetVoteSkip(guildID string) {
	globalVoteSkip.mu.Lock()
	defer globalVoteSkip.mu.Unlock()
	delete(globalVoteSkip.votes, guildID)
	delete(globalVoteSkip.currentTrack, guildID)
}

func ProcessVoteSkip(
	m *coordinator.Module,
	s *discordgo.Session,
	guildID string,
	channelID string,
	userID string,
	username string,
) (skipped bool, message string, err error) {
	if m == nil {
		return false, "Radio module is unavailable.", fmt.Errorf("radio module is nil")
	}

	snap, ok := m.Snapshot(guildID)
	if !ok || snap.Current == nil {
		return false, "There is no track currently playing to skip.", nil
	}

	trackKey := fmt.Sprintf("%s:%s", snap.Current.Title, snap.Current.URL)

	globalVoteSkip.mu.Lock()
	if globalVoteSkip.currentTrack[guildID] != trackKey {
		globalVoteSkip.currentTrack[guildID] = trackKey
		globalVoteSkip.votes[guildID] = make(map[string]bool)
	}
	userVotes := globalVoteSkip.votes[guildID]
	globalVoteSkip.mu.Unlock()

	isGuildOwner, _ := helpers.IsGuildOwner(s, guildID, userID)
	perms, pErr := s.UserChannelPermissions(userID, channelID)
	isAdmin := pErr == nil && perms&discordgo.PermissionAdministrator != 0
	canManageServer := pErr == nil && perms&discordgo.PermissionManageGuild != 0

	if !isAdmin {
		if guild, _ := helpers.GetGuild(s, guildID); guild != nil {
			if member, _ := s.State.Member(guildID, userID); member != nil {
				for _, role := range guild.Roles {
					for _, rID := range member.Roles {
						if role.ID == rID {
							if role.Permissions&discordgo.PermissionAdministrator != 0 {
								isAdmin = true
							}
							if role.Permissions&discordgo.PermissionManageGuild != 0 {
								canManageServer = true
							}
						}
					}
				}
			}
		}
	}

	isRequester := snap.Current.RequesterID != "" && snap.Current.RequesterID == userID
	isPrivileged := isGuildOwner || isAdmin || canManageServer

	listenerCount := countHumanListeners(s, guildID, snap.VoiceChannelID)

	if isPrivileged || isRequester || listenerCount <= 2 {
		globalVoteSkip.mu.Lock()
		delete(globalVoteSkip.votes, guildID)
		globalVoteSkip.mu.Unlock()

		if _, err := m.Control(m.LifecycleContext(), coordinator.ControlRequest{
			GuildID: guildID,
			Action:  coordinator.ControlSkip,
		}); err != nil {
			return false, "There is no track currently playing to skip.", err
		}
		return true, fmt.Sprintf("Skipped by **%s**.", username), nil
	}

	globalVoteSkip.mu.Lock()
	defer globalVoteSkip.mu.Unlock()

	if userVotes[userID] {
		return false, "You have already voted to skip this track.", nil
	}

	userVotes[userID] = true
	totalVotes := len(userVotes)
	neededVotes := (listenerCount + 1) / 2

	if totalVotes >= neededVotes {
		delete(globalVoteSkip.votes, guildID)
		if _, err := m.Control(m.LifecycleContext(), coordinator.ControlRequest{
			GuildID: guildID,
			Action:  coordinator.ControlSkip,
		}); err != nil {
			return false, "Failed to skip track.", err
		}
		return true, "Skipping track (vote threshold reached).", nil
	}

	return false, fmt.Sprintf("Vote to skip recorded (%d/%d votes needed).", totalVotes, neededVotes), nil
}

func countHumanListeners(s *discordgo.Session, guildID, channelID string) int {
	if s == nil || channelID == "" {
		return 1
	}
	botID := ""
	if s.State != nil && s.State.User != nil {
		botID = s.State.User.ID
	}
	guild, err := s.State.Guild(guildID)
	if err != nil || guild == nil {
		return 1
	}
	count := 0
	for _, vs := range guild.VoiceStates {
		if vs != nil && vs.ChannelID == channelID {
			if vs.UserID == botID {
				continue
			}
			if m, err := s.State.Member(guildID, vs.UserID); err == nil && m != nil && m.User != nil && m.User.Bot {
				continue
			}
			count++
		}
	}
	if count < 1 {
		count = 1
	}
	return count
}
