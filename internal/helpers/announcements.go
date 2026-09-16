package helpers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func FormatOrdinal(n int) string {
	abs := n
	if abs < 0 {
		abs = -abs
	}
	rem100 := abs % 100
	if rem100 >= 11 && rem100 <= 13 {
		return fmt.Sprintf("%dth", n)
	}
	switch abs % 10 {
	case 1:
		return fmt.Sprintf("%dst", n)
	case 2:
		return fmt.Sprintf("%dnd", n)
	case 3:
		return fmt.Sprintf("%drd", n)
	default:
		return fmt.Sprintf("%dth", n)
	}
}

func FormatAnnouncementVariables(template string, m *discordgo.Member, guild *discordgo.Guild) string {
	if template == "" {
		return ""
	}

	userName := ""
	displayName := ""
	userMention := ""
	userID := ""
	userAvatar := ""
	createdAtStr := ""
	accountAgeStr := ""

	if m != nil && m.User != nil {
		userName = m.User.Username
		displayName = m.User.GlobalName
		if displayName == "" {
			displayName = m.User.Username
		}
		userMention = m.User.Mention()
		userID = m.User.ID
		userAvatar = MemberAvatar(m)

		if createdTime, err := discordgo.SnowflakeTimestamp(m.User.ID); err == nil {
			createdAtStr = createdTime.Format("2006-01-02")
			accountAgeStr = FormatDuration(time.Since(createdTime))
		}
	}

	serverName := ""
	serverID := ""
	serverIcon := ""
	memberCountStr := "0"
	memberCountOrdinal := "0th"

	if guild != nil {
		serverName = guild.Name
		serverID = guild.ID
		if guild.Icon != "" {
			serverIcon = guild.IconURL("256")
		}
		count := guild.MemberCount
		if count == 0 && guild.ApproximateMemberCount > 0 {
			count = guild.ApproximateMemberCount
		}
		memberCountStr = strconv.Itoa(count)
		memberCountOrdinal = FormatOrdinal(count)
	}

	replacer := strings.NewReplacer(
		"{user.name}", displayName,
		"{user.avatar}", userAvatar,
		"{user.created_at}", createdAtStr,
		"{user.created}", createdAtStr,
		"{user.age}", accountAgeStr,
		"{user.mention}", userMention,
		"{user.id}", userID,
		"{user}", userName,
		"{mention}", userMention,
		"{server.name}", serverName,
		"{server.icon}", serverIcon,
		"{server.id}", serverID,
		"{server}", serverName,
		"{guild.name}", serverName,
		"{guild.icon}", serverIcon,
		"{guild.id}", serverID,
		"{guild}", serverName,
		"{membercount.ordinal}", memberCountOrdinal,
		"{member.count.ordinal}", memberCountOrdinal,
		"{membercount}", memberCountStr,
		"{member.count}", memberCountStr,
	)

	return replacer.Replace(template)
}

func FormatAnnouncementEmbed(embed *discordgo.MessageEmbed, m *discordgo.Member, guild *discordgo.Guild) *discordgo.MessageEmbed {
	if embed == nil {
		return nil
	}

	clone := *embed
	if clone.Color == 0 {
		clone.Color = ColorDefault
	}
	clone.Title = FormatAnnouncementVariables(clone.Title, m, guild)
	clone.Description = FormatAnnouncementVariables(clone.Description, m, guild)
	clone.URL = FormatAnnouncementVariables(clone.URL, m, guild)

	if embed.Footer != nil {
		f := *embed.Footer
		f.Text = FormatAnnouncementVariables(f.Text, m, guild)
		f.IconURL = FormatAnnouncementVariables(f.IconURL, m, guild)
		if strings.TrimSpace(f.Text) == "" && strings.TrimSpace(f.IconURL) == "" {
			clone.Footer = nil
		} else {
			clone.Footer = &f
		}
	}

	if embed.Author != nil {
		a := *embed.Author
		a.Name = FormatAnnouncementVariables(a.Name, m, guild)
		a.URL = FormatAnnouncementVariables(a.URL, m, guild)
		a.IconURL = FormatAnnouncementVariables(a.IconURL, m, guild)
		if strings.TrimSpace(a.Name) == "" && strings.TrimSpace(a.IconURL) == "" {
			clone.Author = nil
		} else {
			clone.Author = &a
		}
	}

	if embed.Thumbnail != nil {
		t := *embed.Thumbnail
		t.URL = FormatAnnouncementVariables(t.URL, m, guild)
		if strings.TrimSpace(t.URL) == "" {
			clone.Thumbnail = nil
		} else {
			clone.Thumbnail = &t
		}
	}

	if embed.Image != nil {
		img := *embed.Image
		img.URL = FormatAnnouncementVariables(img.URL, m, guild)
		if strings.TrimSpace(img.URL) == "" {
			clone.Image = nil
		} else {
			clone.Image = &img
		}
	}

	if len(embed.Fields) > 0 {
		clone.Fields = make([]*discordgo.MessageEmbedField, len(embed.Fields))
		for i, field := range embed.Fields {
			if field != nil {
				f := *field
				f.Name = FormatAnnouncementVariables(f.Name, m, guild)
				f.Value = FormatAnnouncementVariables(f.Value, m, guild)
				clone.Fields[i] = &f
			}
		}
	}

	return &clone
}

func AnnouncementVariablesHelp() string {
	return "`{user}` = Member username handle (e.g. cloudyy)\n" +
		"`{user.name}` = Global display name (e.g. Cloudyy)\n" +
		"`{mention}` / `{user.mention}` = Direct member mention (<@ID>)\n" +
		"`{user.id}` = Member Discord Snowflake ID\n" +
		"`{user.avatar}` = Dynamic avatar URL\n" +
		"`{user.created_at}` = Account creation date (YYYY-MM-DD)\n" +
		"`{user.age}` = Human-readable account age (e.g. 3 days, 2 months)\n" +
		"`{server}` = Server name\n" +
		"`{server.icon}` = Server icon URL\n" +
		"`{server.id}` = Server Snowflake ID\n" +
		"`{membercount}` = Total member count\n" +
		"`{membercount.ordinal}` = Ordinal member position (e.g. 1st, 2nd, 3rd, 100th)"
}

func AnnouncementVariablesEmbedField() *discordgo.MessageEmbedField {
	return &discordgo.MessageEmbedField{
		Name:   "Available Variables",
		Value:  AnnouncementVariablesHelp(),
		Inline: false,
	}
}
