package utility

import (
	"encoding/json"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

type UserMediaType int

const (
	MediaGlobalAvatar UserMediaType = iota
	MediaServerAvatar
	MediaGlobalBanner
	MediaServerBanner
	MediaAll
)

type UserMediaInfo struct {
	Title           string
	URL             string
	NoticeNote      string
	HasMedia        bool
	GlobalAvatarURL string
	ServerAvatarURL string
	GlobalBannerURL string
	ServerBannerURL string
}

type UserProfileRaw struct {
	Bio         string `json:"bio"`
	Description string `json:"description"`
	Pronouns    string `json:"pronouns"`
	AccentColor int    `json:"accent_color"`
	Banner      string `json:"banner"`
	UserProfile *struct {
		Bio         string `json:"bio"`
		Pronouns    string `json:"pronouns"`
		AccentColor int    `json:"accent_color"`
	} `json:"user_profile"`
	User *struct {
		Bio      string `json:"bio"`
		Pronouns string `json:"pronouns"`
	} `json:"user"`
	AvatarDecorationData *struct {
		Asset string `json:"asset"`
		SkuID string `json:"sku_id"`
	} `json:"avatar_decoration_data"`
	Clan *struct {
		Tag   string `json:"tag"`
		Badge string `json:"badge"`
	} `json:"clan"`
}

type ApplicationPublicRaw struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Summary     string `json:"summary"`
}

func FetchUserProfileRaw(s *discordgo.Session, userID string) (*UserProfileRaw, error) {
	profileEndpoint := fmt.Sprintf("%susers/%s/profile", discordgo.EndpointAPI, userID)
	body, err := s.RequestWithBucketID("GET", profileEndpoint, nil, discordgo.EndpointUsers)

	if err != nil || len(body) == 0 {
		userEndpoint := fmt.Sprintf("%susers/%s", discordgo.EndpointAPI, userID)
		body, err = s.RequestWithBucketID("GET", userEndpoint, nil, discordgo.EndpointUsers)
		if err != nil {
			return nil, err
		}
	}

	var raw UserProfileRaw
	if errJSON := json.Unmarshal(body, &raw); errJSON != nil {
		return nil, fmt.Errorf("failed to parse user profile payload: %w", errJSON)
	}

	if raw.Bio == "" && raw.UserProfile != nil && raw.UserProfile.Bio != "" {
		raw.Bio = raw.UserProfile.Bio
	}
	if raw.Bio == "" && raw.User != nil && raw.User.Bio != "" {
		raw.Bio = raw.User.Bio
	}
	if raw.Bio == "" && raw.Description != "" {
		raw.Bio = raw.Description
	}

	if raw.Pronouns == "" && raw.UserProfile != nil && raw.UserProfile.Pronouns != "" {
		raw.Pronouns = raw.UserProfile.Pronouns
	}
	if raw.Pronouns == "" && raw.User != nil && raw.User.Pronouns != "" {
		raw.Pronouns = raw.User.Pronouns
	}

	if raw.Bio == "" {
		appEndpoint := fmt.Sprintf("%sapplications/%s/public", discordgo.EndpointAPI, userID)
		appBody, errApp := s.RequestWithBucketID("GET", appEndpoint, nil, discordgo.EndpointApplications)
		if errApp == nil && len(appBody) > 0 {
			var appRaw ApplicationPublicRaw
			if errUnmarshal := json.Unmarshal(appBody, &appRaw); errUnmarshal == nil && appRaw.Description != "" {
				raw.Bio = appRaw.Description
				raw.Description = appRaw.Description
			}
		}
	}

	return &raw, nil
}

func GetUserMediaInfo(user *discordgo.User, member *discordgo.Member, mediaType UserMediaType) UserMediaInfo {
	if user == nil {
		return UserMediaInfo{}
	}

	switch mediaType {
	case MediaGlobalAvatar:
		url := user.AvatarURL("1024")
		return UserMediaInfo{
			Title:    fmt.Sprintf("%s's Global Avatar:", user.Username),
			URL:      url,
			HasMedia: true,
		}

	case MediaServerAvatar:
		if member != nil && member.Avatar != "" {
			return UserMediaInfo{
				Title:    fmt.Sprintf("%s's Server Avatar:", user.Username),
				URL:      member.AvatarURL("1024"),
				HasMedia: true,
			}
		}
		url := user.AvatarURL("1024")
		return UserMediaInfo{
			Title:      fmt.Sprintf("%s's Server Avatar:", user.Username),
			URL:        url,
			NoticeNote: fmt.Sprintf("-# %s does not have a server avatar", user.Username),
			HasMedia:   false,
		}

	case MediaGlobalBanner:
		if user.Banner != "" {
			return UserMediaInfo{
				Title:    fmt.Sprintf("%s's Global Banner:", user.Username),
				URL:      user.BannerURL("1024"),
				HasMedia: true,
			}
		}
		return UserMediaInfo{
			Title:      fmt.Sprintf("%s's Global Banner:", user.Username),
			NoticeNote: fmt.Sprintf("-# %s does not have a global banner", user.Username),
			HasMedia:   false,
		}

	case MediaServerBanner:
		if member != nil && member.Banner != "" {
			return UserMediaInfo{
				Title:    fmt.Sprintf("%s's Server Banner:", user.Username),
				URL:      member.BannerURL("1024"),
				HasMedia: true,
			}
		}
		if user.Banner != "" {
			return UserMediaInfo{
				Title:      fmt.Sprintf("%s's Server Banner:", user.Username),
				URL:        user.BannerURL("1024"),
				NoticeNote: fmt.Sprintf("-# %s does not have a server banner", user.Username),
				HasMedia:   false,
			}
		}
		return UserMediaInfo{
			Title:      fmt.Sprintf("%s's Server Banner:", user.Username),
			NoticeNote: fmt.Sprintf("-# %s does not have a banner", user.Username),
			HasMedia:   false,
		}

	case MediaAll:
		globalAv := user.AvatarURL("1024")
		serverAv := ""
		if member != nil && member.Avatar != "" {
			serverAv = member.AvatarURL("1024")
		}
		globalBn := ""
		if user.Banner != "" {
			globalBn = user.BannerURL("1024")
		}
		serverBn := ""
		if member != nil && member.Banner != "" {
			serverBn = member.BannerURL("1024")
		}
		activeAvatar := serverAv
		if activeAvatar == "" {
			activeAvatar = globalAv
		}
		return UserMediaInfo{
			Title:           fmt.Sprintf("%s's Media Overview", user.Username),
			URL:             activeAvatar,
			HasMedia:        true,
			GlobalAvatarURL: globalAv,
			ServerAvatarURL: serverAv,
			GlobalBannerURL: globalBn,
			ServerBannerURL: serverBn,
		}
	}

	return UserMediaInfo{}
}

func parseUserFlags(flags discordgo.UserFlags) []string {
	var badges []string
	if flags&discordgo.UserFlagDiscordEmployee != 0 {
		badges = append(badges, "Discord Staff")
	}
	if flags&discordgo.UserFlagDiscordPartner != 0 {
		badges = append(badges, "Partnered Server Owner")
	}
	if flags&discordgo.UserFlagHypeSquadEvents != 0 {
		badges = append(badges, "HypeSquad Events")
	}
	if flags&discordgo.UserFlagBugHunterLevel1 != 0 {
		badges = append(badges, "Bug Hunter Level 1")
	}
	if flags&discordgo.UserFlagHouseBravery != 0 {
		badges = append(badges, "HypeSquad Bravery")
	}
	if flags&discordgo.UserFlagHouseBrilliance != 0 {
		badges = append(badges, "HypeSquad Brilliance")
	}
	if flags&discordgo.UserFlagHouseBalance != 0 {
		badges = append(badges, "HypeSquad Balance")
	}
	if flags&discordgo.UserFlagEarlySupporter != 0 {
		badges = append(badges, "Early Supporter")
	}
	if flags&discordgo.UserFlagBugHunterLevel2 != 0 {
		badges = append(badges, "Bug Hunter Level 2")
	}
	if flags&discordgo.UserFlagVerifiedBotDeveloper != 0 {
		badges = append(badges, "Early Verified Bot Developer")
	}
	if flags&discordgo.UserFlagDiscordCertifiedModerator != 0 {
		badges = append(badges, "Discord Certified Moderator")
	}
	if flags&(1<<22) != 0 {
		badges = append(badges, "Active Developer")
	}
	return badges
}

func parseNitroType(nitroType discordgo.UserPremiumType) string {
	switch nitroType {
	case discordgo.UserPremiumTypeNitroClassic:
		return "Nitro Classic"
	case discordgo.UserPremiumTypeNitro:
		return "Nitro"
	case discordgo.UserPremiumTypeNitroBasic:
		return "Nitro Basic"
	default:
		return "None"
	}
}
