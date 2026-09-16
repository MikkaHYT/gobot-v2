package helpers

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"
)

const (
	MuteDenyFlags         int64 = 0x00000800 | 0x00000040 | 0x00200000
	ImageMuteDenyFlags    int64 = 0x00008000 | 0x00004000
	ReactionMuteDenyFlags int64 = 0x00000040
	JailDenyFlags         int64 = 0x00000400
)

func GetChannel(s *discordgo.Session, channelID string) (*discordgo.Channel, error) {
	if s == nil || channelID == "" {
		return nil, errors.New("session and channelID are required")
	}
	if s.State != nil {
		if ch, err := s.State.Channel(channelID); err == nil && ch != nil {
			return ch, nil
		}
	}
	return s.Channel(channelID)
}

func UpdateEveryoneRoleOverwrite(s *discordgo.Session, guildID, channelID string, mutate func(allow, deny int64) (newAllow, newDeny int64)) error {
	if s == nil || channelID == "" || guildID == "" {
		return nil
	}
	channel, err := GetChannel(s, channelID)
	if err != nil {
		return err
	}

	var allow, deny int64
	for _, ow := range channel.PermissionOverwrites {
		if ow.ID == guildID && ow.Type == discordgo.PermissionOverwriteTypeRole {
			allow = ow.Allow
			deny = ow.Deny
			break
		}
	}

	newAllow, newDeny := mutate(allow, deny)
	return s.ChannelPermissionSet(channelID, guildID, discordgo.PermissionOverwriteTypeRole, newAllow, newDeny)
}

func LockChannel(s *discordgo.Session, guildID, channelID string) error {
	return UpdateEveryoneRoleOverwrite(s, guildID, channelID, func(allow, deny int64) (int64, int64) {
		return allow &^ discordgo.PermissionSendMessages, deny | discordgo.PermissionSendMessages
	})
}

func UnlockChannel(s *discordgo.Session, guildID, channelID string) error {
	return UpdateEveryoneRoleOverwrite(s, guildID, channelID, func(allow, deny int64) (int64, int64) {
		return allow, deny &^ discordgo.PermissionSendMessages
	})
}

func HideChannel(s *discordgo.Session, guildID, channelID string) error {
	return UpdateEveryoneRoleOverwrite(s, guildID, channelID, func(allow, deny int64) (int64, int64) {
		return allow &^ discordgo.PermissionViewChannel, deny | discordgo.PermissionViewChannel
	})
}

func UnhideChannel(s *discordgo.Session, guildID, channelID string) error {
	return UpdateEveryoneRoleOverwrite(s, guildID, channelID, func(allow, deny int64) (int64, int64) {
		return allow | discordgo.PermissionViewChannel, deny &^ discordgo.PermissionViewChannel
	})
}

func IsChannelLocked(ch *discordgo.Channel, guildID string) bool {
	if ch == nil {
		return false
	}
	for _, ow := range ch.PermissionOverwrites {
		if ow.Type == discordgo.PermissionOverwriteTypeRole && ow.ID == guildID {
			return (ow.Deny & discordgo.PermissionSendMessages) != 0
		}
	}
	return false
}

func IsChannelHidden(ch *discordgo.Channel, guildID string) bool {
	if ch == nil {
		return false
	}
	for _, ow := range ch.PermissionOverwrites {
		if ow.Type == discordgo.PermissionOverwriteTypeRole && ow.ID == guildID {
			return (ow.Deny & discordgo.PermissionViewChannel) != 0
		}
	}
	return false
}

func LockStarboardChannel(s *discordgo.Session, guildID, channelID string) error {
	flags := int64(discordgo.PermissionSendMessages | discordgo.PermissionAddReactions)
	return UpdateEveryoneRoleOverwrite(s, guildID, channelID, func(allow, deny int64) (int64, int64) {
		return allow &^ flags, deny | flags
	})
}

func EnsureJailChannelPermissions(s *discordgo.Session, guildID, jailRoleID, jailChannelID string) error {
	if jailChannelID == "" || guildID == "" {
		return nil
	}
	if err := s.ChannelPermissionSet(jailChannelID, guildID, discordgo.PermissionOverwriteTypeRole, 0, discordgo.PermissionViewChannel); err != nil {
		return fmt.Errorf("failed to deny view permissions for everyone on jail channel: %w", err)
	}
	if jailRoleID != "" {
		allowFlags := int64(discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionReadMessageHistory)
		if err := s.ChannelPermissionSet(jailChannelID, jailRoleID, discordgo.PermissionOverwriteTypeRole, allowFlags, 0); err != nil {
			return fmt.Errorf("failed to grant view/send permissions for jail role: %w", err)
		}
	}
	return nil
}

func EnsureRoleOverwrites(s *discordgo.Session, guildID, roleID string, denyFlags int64) error {
	if roleID == "" || guildID == "" {
		return nil
	}
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return err
	}
	const maxOverwriteWorkers = 8
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxOverwriteWorkers)
	var mu sync.Mutex
	var firstErr error
	var failed []string
	for _, ch := range channels {
		if denyFlags == JailDenyFlags {
			if ch.Name == "jail" || ch.Name == "jail-cell" || ch.Name == "jail-room" {
				continue
			}
		}
		if ch.Type == discordgo.ChannelTypeGuildText || ch.Type == discordgo.ChannelTypeGuildVoice || ch.Type == discordgo.ChannelTypeGuildCategory || ch.Type == discordgo.ChannelTypeGuildNews || ch.Type == discordgo.ChannelTypeGuildStageVoice || ch.Type == discordgo.ChannelTypeGuildForum || ch.Type == discordgo.ChannelTypeGuildMedia {
			existingAllow := int64(0)
			existingDeny := int64(0)
			hasOverwrite := false
			for _, ow := range ch.PermissionOverwrites {
				if ow.Type == discordgo.PermissionOverwriteTypeRole && ow.ID == roleID {
					existingAllow = ow.Allow
					existingDeny = ow.Deny
					hasOverwrite = true
					break
				}
			}
			if hasOverwrite && (existingDeny&denyFlags) == denyFlags && (existingAllow&denyFlags) == 0 {
				continue
			}
			targetDeny := existingDeny | denyFlags
			targetAllow := existingAllow & ^denyFlags
			chID := ch.ID
			chName := ch.Name
			targetAllowVal := targetAllow
			targetDenyVal := targetDeny
			sem <- struct{}{}
			wg.Add(1)
			Spawn(func() {
				defer wg.Done()
				defer func() { <-sem }()

				if err := s.ChannelPermissionSet(chID, roleID, discordgo.PermissionOverwriteTypeRole, targetAllowVal, targetDenyVal); err != nil {
					LogWarn("[OVERWRITE] Failed to set overwrite on %s for role %s: %v", chName, roleID, err)
					mu.Lock()
					failed = append(failed, chID)
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				}
			})
		}
	}
	wg.Wait()
	if firstErr != nil {
		return fmt.Errorf("failed for %d channels (%s): %w", len(failed), strings.Join(failed, ","), firstErr)
	}
	return nil
}

func ParseChannelID(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "<#") && strings.HasSuffix(arg, ">") {
		arg = strings.TrimPrefix(arg, "<#")
		arg = strings.TrimSuffix(arg, ">")
	}
	if IsSnowflake(arg) {
		return arg
	}
	return ""
}

func ResolveGuildChannel(s *discordgo.Session, guildID, query string) (*discordgo.Channel, error) {
	if s == nil {
		return nil, errors.New("session is nil")
	}
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, errors.New("command must be executed in a server")
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("no channel specified")
	}

	chID := ParseChannelID(query)
	if chID != "" {
		if ch, err := s.Channel(chID); err == nil && ch != nil && ch.GuildID == guildID {
			return ch, nil
		}
	}

	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch server channels: %w", err)
	}

	if chID != "" {
		for _, channel := range channels {
			if channel.ID == chID {
				return channel, nil
			}
		}
	}

	cleanQuery := strings.TrimPrefix(strings.ToLower(query), "#")

	for _, channel := range channels {
		if strings.ToLower(channel.Name) == cleanQuery {
			return channel, nil
		}
	}

	var partialMatches []*discordgo.Channel
	for _, channel := range channels {
		if strings.Contains(strings.ToLower(channel.Name), cleanQuery) {
			partialMatches = append(partialMatches, channel)
		}
	}

	if len(partialMatches) == 1 {
		return partialMatches[0], nil
	}
	if len(partialMatches) > 1 {
		names := make([]string, 0, len(partialMatches))
		for _, ch := range partialMatches {
			names = append(names, fmt.Sprintf("<#%s>", ch.ID))
		}
		return nil, fmt.Errorf("multiple channels matched %q: %s; specify an exact channel mention or ID", query, strings.Join(names, ", "))
	}

	return nil, fmt.Errorf("channel not found for query: `%s`", query)
}
