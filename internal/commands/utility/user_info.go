package utility

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)


func formatDiscordTimestamp(t time.Time) string {
	if t.IsZero() {
		return "Unknown"
	}
	epoch := t.Unix()
	return fmt.Sprintf("<t:%d:F> (<t:%d:R>)", epoch, epoch)
}

func formatSnowflakeTimestamp(id string) string {
	t, err := discordgo.SnowflakeTimestamp(id)
	if err != nil {
		return "Unknown"
	}
	return formatDiscordTimestamp(t)
}

func formatRoleMentions(roles []string, guildID string, limit int) (string, int) {
	var mentions []string
	for _, id := range roles {
		if id != guildID {
			mentions = append(mentions, fmt.Sprintf("<@&%s>", id))
		}
	}

	total := len(mentions)
	if total == 0 {
		return "None", 0
	}

	if total > limit {
		return fmt.Sprintf("%s ... (+%d more)", strings.Join(mentions[:limit], ", "), total-limit), total
	}

	return strings.Join(mentions, ", "), total
}

func formatMediaLinks(media UserMediaInfo, globalBanner string) string {
	var links []string
	if media.GlobalAvatarURL != "" {
		links = append(links, fmt.Sprintf("[Global Avatar](%s)", media.GlobalAvatarURL))
	}
	if media.ServerAvatarURL != "" {
		links = append(links, fmt.Sprintf("[Server Avatar](%s)", media.ServerAvatarURL))
	}

	banner := media.GlobalBannerURL
	if banner == "" {
		banner = globalBanner
	}
	if banner != "" {
		links = append(links, fmt.Sprintf("[Global Banner](%s)", banner))
	}

	if media.ServerBannerURL != "" {
		links = append(links, fmt.Sprintf("[Server Banner](%s)", media.ServerBannerURL))
	}

	if len(links) == 0 {
		return "None"
	}
	return strings.Join(links, " - ")
}

type UserinfoCmd struct{}

func (c *UserinfoCmd) Name() string        { return "userinfo" }
func (c *UserinfoCmd) Aliases() []string   { return []string{"ui", "whois"} }
func (c *UserinfoCmd) Category() string    { return "Utility" }
func (c *UserinfoCmd) Description() string { return "Displays detailed information about a user." }
func (c *UserinfoCmd) Usage() string       { return "(@user)" }
func (c *UserinfoCmd) Example() string     { return "@Cloudyy" }

func (c *UserinfoCmd) Execute(ctx *bot.Context) error {
	target, member, err := ctx.TargetUserAndMember()
	if err != nil || target == nil {
		return ctx.SendError("Could not find a valid user with that ID.")
	}

	media := GetUserMediaInfo(target, member, MediaAll)
	icon := media.URL
	if icon == "" {
		icon = helpers.UserAvatar(target)
	}

	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s's info:", target.Username),
			IconURL: icon,
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: icon,
		},
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "User ID",
				Value:  fmt.Sprintf("`%s`", target.ID),
				Inline: true,
			},
			{
				Name:   "Bot",
				Value:  helpers.FormatBool(target.Bot),
				Inline: true,
			},
			{
				Name:   "Media",
				Value:  formatMediaLinks(media, ""),
				Inline: false,
			},
			{
				Name:   "Account Created",
				Value:  formatSnowflakeTimestamp(target.ID),
				Inline: false,
			},
		},
	}

	if member != nil && ctx.Message.GuildID != "" {
		if !member.JoinedAt.IsZero() {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:   "Joined Server",
				Value:  formatDiscordTimestamp(member.JoinedAt),
				Inline: false,
			})
		}

		roles, count := formatRoleMentions(member.Roles, ctx.Message.GuildID, 10)
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Roles [%d]", count),
			Value:  roles,
			Inline: false,
		})
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

type DetailedUserinfoCmd struct{}

func (c *DetailedUserinfoCmd) Name() string      { return "detaileduserinfo" }
func (c *DetailedUserinfoCmd) Aliases() []string { return []string{"dui", "detailedui", "fullinfo"} }
func (c *DetailedUserinfoCmd) Category() string  { return "Utility" }
func (c *DetailedUserinfoCmd) Description() string {
	return "Displays detailed info about a user."
}
func (c *DetailedUserinfoCmd) Usage() string   { return "(@user)" }
func (c *DetailedUserinfoCmd) Example() string { return "@Cloudyy" }

func (c *DetailedUserinfoCmd) Execute(ctx *bot.Context) error {
	target, member, err := ctx.TargetUserAndMember()
	if err != nil || target == nil {
		return ctx.SendError("Could not find a valid user with that ID.")
	}

	if fetched, err := ctx.Session.User(target.ID); err == nil && fetched != nil {
		target = fetched
	}

	rawProfile, _ := FetchUserProfileRaw(ctx.Session, target.ID)
	media := GetUserMediaInfo(target, member, MediaAll)

	icon := media.URL
	if icon == "" {
		icon = helpers.UserAvatar(target)
	}

	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("Detailed UI - %s", target.Username),
			IconURL: icon,
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: icon,
		},
	}

	if target.AccentColor != 0 {
		embed.Color = target.AccentColor
	} else if rawProfile != nil && rawProfile.AccentColor != 0 {
		embed.Color = rawProfile.AccentColor
	}

	embed.Fields = append(embed.Fields, buildUserIdentityField(target, rawProfile))
	embed.Fields = append(embed.Fields, buildUserBadgesField(target, member, rawProfile, embed.Color))

	if rawProfile != nil && rawProfile.Bio != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "About Me (Bio)",
			Value:  helpers.TruncateStringWithEllipsis(rawProfile.Bio, 1024),
			Inline: false,
		})
	}

	embed.Fields = append(embed.Fields, buildUserPresenceField(ctx, target, member))
	embed.Fields = append(embed.Fields, buildUserTimestampsField(target, member))

	if member != nil && ctx.Message.GuildID != "" {
		embed.Fields = append(embed.Fields, buildMemberProfileField(ctx, target, member))
		if perms, err := ctx.Session.UserChannelPermissions(target.ID, ctx.Message.ChannelID); err == nil {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:   "Key Permissions",
				Value:  helpers.FormatPermissions(perms),
				Inline: false,
			})
		}
		roles, count := formatRoleMentions(member.Roles, ctx.Message.GuildID, 12)
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("Roles [%d]", count),
			Value:  roles,
			Inline: false,
		})
	}

	rawBannerURL := resolveRawBannerURL(target, rawProfile)
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Media Links",
		Value:  formatMediaLinks(media, rawBannerURL),
		Inline: false,
	})

	if rawBannerURL != "" {
		embed.Image = &discordgo.MessageEmbedImage{URL: rawBannerURL}
	} else if media.ServerBannerURL != "" {
		embed.Image = &discordgo.MessageEmbedImage{URL: media.ServerBannerURL}
	}

	if file := createUserRawDataFile(target, member, rawProfile); file != nil {
		_, err = ctx.SendFileWithEmbed(file, embed)
		return err
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

func buildUserIdentityField(target *discordgo.User, rawProfile *UserProfileRaw) *discordgo.MessageEmbedField {
	globalName := target.GlobalName
	if globalName == "" {
		globalName = "None"
	}
	discrim := target.Discriminator
	if discrim == "" || discrim == "0" {
		discrim = "None (Migrated Username)"
	}

	identity := fmt.Sprintf("• **ID:** `%s`\n• **Username:** `%s`\n• **Global Name:** `%s`\n• **Discriminator:** `%s`\n• **Bot:** `%s` | **System:** `%s`",
		target.ID, target.Username, globalName, discrim, helpers.FormatBool(target.Bot), helpers.FormatBool(target.System))

	if rawProfile != nil {
		if rawProfile.Pronouns != "" {
			identity += fmt.Sprintf("\n• **Pronouns:** `%s`", rawProfile.Pronouns)
		}
		if rawProfile.Clan != nil && rawProfile.Clan.Tag != "" {
			identity += fmt.Sprintf("\n• **Clan Tag:** `[%s]`", rawProfile.Clan.Tag)
		}
	}

	return &discordgo.MessageEmbedField{
		Name:   "User Identity",
		Value:  identity,
		Inline: false,
	}
}

func buildUserBadgesField(target *discordgo.User, member *discordgo.Member, rawProfile *UserProfileRaw, embedColor int) *discordgo.MessageEmbedField {
	badges := parseUserFlags(discordgo.UserFlags(target.Flags) | target.PublicFlags)
	badgesStr := "None"
	if len(badges) > 0 {
		badgesStr = strings.Join(badges, ", ")
	}

	accentColor := "Default"
	if embedColor != 0 {
		accentColor = fmt.Sprintf("#%06X (`%d`)", embedColor, embedColor)
	}

	nitroStr := parseNitroType(target.PremiumType)
	if member != nil && member.PremiumSince != nil {
		nitroStr = "Nitro (Boosting Server)"
	}

	badgeVal := fmt.Sprintf("• **Public Badges:** %s\n• **Nitro Tier:** `%s`\n• **Accent Color:** %s",
		badgesStr, nitroStr, accentColor)

	if rawProfile != nil && rawProfile.AvatarDecorationData != nil && rawProfile.AvatarDecorationData.Asset != "" {
		badgeVal += fmt.Sprintf("\n• **Avatar Decor Asset:** [Click Here](https://cdn.discordapp.com/avatar-decoration-presets/%s.png?size=4096)", rawProfile.AvatarDecorationData.Asset)
	}

	return &discordgo.MessageEmbedField{
		Name:   "Badges & Nitro Profile",
		Value:  badgeVal,
		Inline: false,
	}
}

func buildUserPresenceField(ctx *bot.Context, target *discordgo.User, member *discordgo.Member) *discordgo.MessageEmbedField {
	presenceStr := "• **Status:** `Offline / Invisible`"
	if member != nil && ctx.Message.GuildID != "" {
		if presence, err := ctx.Session.State.Presence(ctx.Message.GuildID, target.ID); err == nil {
			var clients []string
			if presence.ClientStatus.Desktop != "" {
				clients = append(clients, fmt.Sprintf("Desktop (%s)", presence.ClientStatus.Desktop))
			}
			if presence.ClientStatus.Mobile != "" {
				clients = append(clients, fmt.Sprintf("Mobile (%s)", presence.ClientStatus.Mobile))
			}
			if presence.ClientStatus.Web != "" {
				clients = append(clients, fmt.Sprintf("Web (%s)", presence.ClientStatus.Web))
			}

			clientStr := "Unknown"
			if len(clients) > 0 {
				clientStr = strings.Join(clients, ", ")
			}

			activitySummary := "None"
			if len(presence.Activities) > 0 {
				var acts []string
				for _, act := range presence.Activities {
					if act.Type == discordgo.ActivityTypeCustom {
						acts = append(acts, fmt.Sprintf("*\"%s\"*", act.State))
					} else {
						acts = append(acts, fmt.Sprintf("Playing `%s`", act.Name))
					}
				}
				activitySummary = helpers.TruncateStringWithEllipsis(strings.Join(acts, " | "), 512)
			}
			presenceStr = fmt.Sprintf("• **Status:** `%s`\n• **Active Devices:** `%s`\n• **Activity:** %s", presence.Status, clientStr, activitySummary)
		}
	}

	return &discordgo.MessageEmbedField{
		Name:   "Live Status & Presence",
		Value:  presenceStr,
		Inline: false,
	}
}

func buildUserTimestampsField(target *discordgo.User, member *discordgo.Member) *discordgo.MessageEmbedField {
	timeVal := fmt.Sprintf("• **Account Created:** %s", formatSnowflakeTimestamp(target.ID))
	if member != nil {
		if !member.JoinedAt.IsZero() {
			timeVal += fmt.Sprintf("\n• **Joined Server:** %s", formatDiscordTimestamp(member.JoinedAt))
		}
		if member.PremiumSince != nil && !member.PremiumSince.IsZero() {
			timeVal += fmt.Sprintf("\n• **Server Boosting Since:** %s", formatDiscordTimestamp(*member.PremiumSince))
		}
		if member.CommunicationDisabledUntil != nil && member.CommunicationDisabledUntil.After(time.Now()) {
			timeVal += fmt.Sprintf("\n• **Timed Out Until:** %s", formatDiscordTimestamp(*member.CommunicationDisabledUntil))
		}
	}

	return &discordgo.MessageEmbedField{
		Name:   "Timestamps & Account Age",
		Value:  timeVal,
		Inline: false,
	}
}

func buildMemberProfileField(ctx *bot.Context, target *discordgo.User, member *discordgo.Member) *discordgo.MessageEmbedField {
	nick := member.Nick
	if nick == "" {
		nick = "None"
	}

	voiceStr := "Not in a Voice Channel"
	if vs, err := ctx.Session.State.VoiceState(ctx.Message.GuildID, target.ID); err == nil && vs != nil {
		chName := vs.ChannelID
		if ch, errCh := ctx.Session.State.Channel(vs.ChannelID); errCh == nil {
			chName = ch.Name
		}
		voiceStr = fmt.Sprintf("Connected to `%s` (SelfMute: %v | SelfDeaf: %v | Video: %v)", chName, vs.SelfMute, vs.SelfDeaf, vs.SelfVideo)
	}

	return &discordgo.MessageEmbedField{
		Name:   "Server Member Profile",
		Value:  fmt.Sprintf("• **Server Nickname:** `%s`\n• **Pending Screening:** `%s`\n• **Voice Status:** %s", nick, helpers.FormatBool(member.Pending), voiceStr),
		Inline: false,
	}
}

func resolveRawBannerURL(target *discordgo.User, rawProfile *UserProfileRaw) string {
	if target.Banner != "" {
		return target.BannerURL("4096")
	}
	if rawProfile != nil && rawProfile.Banner != "" {
		return fmt.Sprintf("https://cdn.discordapp.com/banners/%s/%s.png?size=4096", target.ID, rawProfile.Banner)
	}
	return ""
}

func createUserRawDataFile(target *discordgo.User, member *discordgo.Member, rawProfile *UserProfileRaw) *discordgo.File {
	rawData := map[string]any{
		"user":        target,
		"member":      member,
		"profile_raw": rawProfile,
	}

	jsonBytes, err := json.MarshalIndent(rawData, "", "  ")
	if err != nil {
		return nil
	}
	return &discordgo.File{
		Name:        fmt.Sprintf("%s_data.json", target.Username),
		ContentType: "application/json",
		Reader:      bytes.NewReader(jsonBytes),
	}
}
