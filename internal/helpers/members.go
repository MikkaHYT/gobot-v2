package helpers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func IsDiscordNotFound(err error) bool {
	if err == nil {
		return false
	}
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) {
		if restErr.Response != nil && restErr.Response.StatusCode == 404 {
			return true
		}
		if restErr.Message != nil {
			switch restErr.Message.Code {
			case 10007, 10003, 10013, 10008:
				return true
			}
		}
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "unknown member") ||
		strings.Contains(errStr, "unknown user") ||
		strings.Contains(errStr, "unknown channel") ||
		strings.Contains(errStr, "unknown message") ||
		strings.Contains(errStr, "404")
}

func GetGuildMember(s *discordgo.Session, guildID, userID string) (*discordgo.Member, error) {
	if s == nil || guildID == "" || userID == "" {
		return nil, fmt.Errorf("session, guildID, and userID are required")
	}

	if s.State != nil {
		if member, err := s.State.Member(guildID, userID); err == nil && member != nil {
			if member.User != nil {
				return member, nil
			}
		}
	}

	member, err := s.GuildMember(guildID, userID)
	if err != nil || member == nil {
		return nil, err
	}

	return member, nil
}

func GetGuildMemberWithUserErr(s *discordgo.Session, guildID string, user *discordgo.User) (*discordgo.Member, error) {
	if user == nil || s == nil || guildID == "" {
		return nil, fmt.Errorf("session, guildID, and user are required")
	}

	member, err := GetGuildMember(s, guildID, user.ID)
	if err != nil || member == nil {
		return nil, err
	}

	if member.User == nil {
		member.User = user
	}

	return member, nil
}

func GetVoiceStateUser(s *discordgo.Session, v *discordgo.VoiceStateUpdate) *discordgo.User {
	if v == nil || v.VoiceState == nil {
		return nil
	}
	if v.Member != nil && v.Member.User != nil {
		return v.Member.User
	}
	if s == nil || s.State == nil || v.GuildID == "" || v.UserID == "" {
		return nil
	}
	member, err := s.State.Member(v.GuildID, v.UserID)
	if err != nil || member == nil {
		return nil
	}
	return member.User
}

func GetGuild(s *discordgo.Session, guildID string) (*discordgo.Guild, error) {
	if s == nil || guildID == "" {
		return nil, fmt.Errorf("session and guildID are required")
	}
	if s.State != nil {
		if guild, err := s.State.Guild(guildID); err == nil && guild != nil {
			return guild, nil
		}
	}
	return s.Guild(guildID)
}

func IsGuildOwner(s *discordgo.Session, guildID, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	guild, err := GetGuild(s, guildID)
	if err != nil || guild == nil {
		return false, err
	}
	return guild.OwnerID == userID, nil
}

func FetchGuildMembers(s *discordgo.Session, guildID string, maxMembers int) ([]*discordgo.Member, error) {
	if s == nil || guildID == "" {
		return nil, fmt.Errorf("session and guildID are required")
	}
	if maxMembers <= 0 {
		maxMembers = 1000
	}

	var allMembers []*discordgo.Member
	after := ""
	for len(allMembers) < maxMembers {
		limit := 1000
		remaining := maxMembers - len(allMembers)
		if remaining < limit {
			limit = remaining
		}

		members, err := s.GuildMembers(guildID, after, limit)
		if err != nil {
			if len(allMembers) > 0 {
				return allMembers, nil
			}
			return nil, err
		}
		if len(members) == 0 {
			break
		}

		allMembers = append(allMembers, members...)
		if s.State != nil {
			for _, m := range members {
				if m != nil {
					if m.GuildID == "" {
						m.GuildID = guildID
					}
					_ = s.State.MemberAdd(m)
				}
			}
		}

		if len(members) < limit {
			break
		}
		after = members[len(members)-1].User.ID
	}

	return allMembers, nil
}
