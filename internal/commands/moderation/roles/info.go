package roles

import (
	"fmt"
	"sort"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

func (c *RoleCmd) handleList(ctx *bot.Context) error {
	subArgs := ctx.SubArgs()
	if len(subArgs) == 0 {
		var roles []*discordgo.Role
		if ctx.Session.State != nil {
			if guild, err := ctx.Session.State.Guild(ctx.Message.GuildID); err == nil && guild != nil {
				roles = guild.Roles
			}
		}
		if len(roles) == 0 {
			roles, _ = ctx.Session.GuildRoles(ctx.Message.GuildID)
		}

		if len(roles) == 0 {
			_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
				Description: "No roles found in this server.",
			})
			return err
		}

		sort.Slice(roles, func(i, j int) bool {
			return roles[i].Position > roles[j].Position
		})

		var lines []string
		for _, r := range roles {
			if r.ID == ctx.Message.GuildID {
				lines = append(lines, fmt.Sprintf("• `everyone` (`%s`)", r.ID))
			} else {
				lines = append(lines, fmt.Sprintf("• <@&%s> (`%s`)", r.ID, r.ID))
			}
		}

		const pageSize = 15
		var embeds []*discordgo.MessageEmbed
		totalRoles := len(lines)
		for i := 0; i < totalRoles; i += pageSize {
			end := i + pageSize
			if end > totalRoles {
				end = totalRoles
			}
			embeds = append(embeds, &discordgo.MessageEmbed{
				Title:       fmt.Sprintf("Server Roles (%d)", totalRoles),
				Description: strings.Join(lines[i:end], "\n"),
			})
		}
		return ctx.SendPaginatedEmbeds(embeds)
	}

	roleArg := strings.Join(subArgs, " ")
	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return fmt.Errorf("role not found: `%s`", roleArg)
	}

	var guildMembers []*discordgo.Member
	if ctx.Session.State != nil {
		guild, errGuild := ctx.Session.State.Guild(ctx.Message.GuildID)
		if errGuild == nil && guild != nil && guild.MemberCount > 0 && len(guild.Members) >= guild.MemberCount {
			guildMembers = guild.Members
		}
	}

	if len(guildMembers) == 0 {
		after := ""
		for {
			members, err := ctx.Session.GuildMembers(ctx.Message.GuildID, after, 1000)
			if err != nil || len(members) == 0 {
				break
			}
			guildMembers = append(guildMembers, members...)
			if len(members) < 1000 {
				break
			}
			after = members[len(members)-1].User.ID
		}
	}

	var roleMembers []string
	for _, m := range guildMembers {
		if m == nil || m.User == nil {
			continue
		}
		for _, rID := range m.Roles {
			if rID == targetRole.ID {
				roleMembers = append(roleMembers, fmt.Sprintf("<@%s> (`%s`)", m.User.ID, m.User.ID))
				break
			}
		}
	}

	if len(roleMembers) == 0 {
		_, err := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("No members currently have the <@&%s> role.", targetRole.ID),
		})
		return err
	}

	const pageSize = 15
	var embeds []*discordgo.MessageEmbed
	totalMembers := len(roleMembers)

	for i := 0; i < totalMembers; i += pageSize {
		end := i + pageSize
		if end > totalMembers {
			end = totalMembers
		}

		pageContent := strings.Join(roleMembers[i:end], "\n")
		embeds = append(embeds, &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("Members with %s (%d total)", targetRole.Name, totalMembers),
			Description: pageContent,
			Color:       targetRole.Color,
		})
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

func (c *RoleCmd) handleInfo(ctx *bot.Context) error {
	if !ctx.RequireSubArgs(1, "role info <role>") {
		return nil
	}

	subArgs := ctx.SubArgs()
	roleArg := strings.Join(subArgs, " ")
	targetRole, err := ctx.ResolveRole(roleArg)
	if err != nil || targetRole == nil {
		return fmt.Errorf("role not found: `%s`", roleArg)
	}

	embed, err := BuildRoleInfoEmbed(ctx, targetRole)
	if err != nil {
		return err
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

func BuildRoleInfoEmbed(ctx *bot.Context, targetRole *discordgo.Role) (*discordgo.MessageEmbed, error) {
	if targetRole == nil {
		return nil, fmt.Errorf("role cannot be nil")
	}

	guildID := ctx.Message.GuildID

	embed := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name: fmt.Sprintf("Role Info - %s", targetRole.Name),
		},
		Color: targetRole.Color,
	}

	if targetRole.Icon != "" {
		iconURL := fmt.Sprintf("https://cdn.discordapp.com/role-icons/%s/%s.png", targetRole.ID, targetRole.Icon)
		embed.Author.IconURL = iconURL
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: iconURL,
		}
	}

	var guildMembers []*discordgo.Member

	if ctx.Session.State != nil {
		guild, errGuild := ctx.Session.State.Guild(guildID)
		if errGuild == nil && guild != nil && guild.MemberCount > 0 && len(guild.Members) >= guild.MemberCount {
			guildMembers = guild.Members
		}
	}

	if len(guildMembers) == 0 {
		after := ""
		for {
			members, err := ctx.Session.GuildMembers(guildID, after, 1000)
			if err != nil || len(members) == 0 {
				break
			}
			guildMembers = append(guildMembers, members...)
			if len(members) < 1000 {
				break
			}
			after = members[len(members)-1].User.ID
		}
	}

	var memberCount int
	var userNames []string
	const maxDisplay = 10

	for _, member := range guildMembers {
		if member == nil || member.User == nil {
			continue
		}
		for _, roleID := range member.Roles {
			if roleID == targetRole.ID {
				memberCount++
				if len(userNames) < maxDisplay {
					userNames = append(userNames, fmt.Sprintf("<@%s>", member.User.ID))
				}
				break
			}
		}
	}

	var usersListStr string
	if memberCount == 0 {
		usersListStr = "*No users have this role.*"
	} else if memberCount > len(userNames) {
		usersListStr = strings.Join(userNames, ", ") + fmt.Sprintf(", ... (+%d more)", memberCount-len(userNames))
	} else {
		usersListStr = strings.Join(userNames, ", ")
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Role ID",
		Value:  fmt.Sprintf("`%s`", targetRole.ID),
		Inline: true,
	})

	colorDisplay := "Default"
	if targetRole.Color != 0 {
		colorDisplay = fmt.Sprintf("#%06X", targetRole.Color)
	}
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Color",
		Value:  colorDisplay,
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Position",
		Value:  fmt.Sprintf("%d", targetRole.Position),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Hoisted",
		Value:  helpers.FormatBool(targetRole.Hoist),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Mentionable",
		Value:  helpers.FormatBool(targetRole.Mentionable),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Managed (Bot/Integration)",
		Value:  helpers.FormatBool(targetRole.Managed),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   fmt.Sprintf("Members (%d)", memberCount),
		Value:  usersListStr,
		Inline: false,
	})

	keyPerms := helpers.FormatPermissions(targetRole.Permissions)
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   "Key Permissions",
		Value:  keyPerms,
		Inline: false,
	})

	createdAt, errTime := discordgo.SnowflakeTimestamp(targetRole.ID)
	if errTime == nil {
		embed.Footer = &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Created: %s", createdAt.Format("Jan 02, 2006 15:04:05 UTC")),
		}
	}

	return embed, nil
}
