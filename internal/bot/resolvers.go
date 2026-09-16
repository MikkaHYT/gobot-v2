package bot

import (
	"errors"
	"fmt"
	"gobot/internal/helpers"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func (c *Context) ParseUserID(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "<@") && strings.HasSuffix(arg, ">") {
		arg = strings.TrimPrefix(arg, "<@")
		arg = strings.TrimPrefix(arg, "!")
		arg = strings.TrimSuffix(arg, ">")
		if helpers.IsSnowflake(arg) {
			return arg
		}
		return ""
	}
	if helpers.IsSnowflake(arg) {
		return arg
	}
	return ""
}

func (c *Context) ParseRoleID(arg string) string {
	arg = strings.TrimSpace(arg)
	if strings.HasPrefix(arg, "<@&") && strings.HasSuffix(arg, ">") {
		arg = strings.TrimPrefix(arg, "<@&")
		arg = strings.TrimSuffix(arg, ">")
		if helpers.IsSnowflake(arg) {
			return arg
		}
		return ""
	}
	if helpers.IsSnowflake(arg) {
		return arg
	}
	return ""
}

func (c *Context) ParseChannelID(arg string) string {
	if id := helpers.ParseChannelID(arg); id != "" {
		return id
	}
	if strings.TrimSpace(arg) == "" && c.Message != nil {
		return c.Message.ChannelID
	}
	return ""
}

type MessageLinkInfo struct {
	GuildID   string
	ChannelID string
	MessageID string
}

var messageLinkRegex = regexp.MustCompile(`https://(?:ptb\.|canary\.)?discord(?:app)?\.com/channels/(\d+)/(\d+)/(\d+)`)

func (c *Context) ParseMessageLink(input string) *MessageLinkInfo {
	input = strings.TrimSpace(input)
	if match := messageLinkRegex.FindStringSubmatch(input); len(match) > 3 {
		return &MessageLinkInfo{
			GuildID:   match[1],
			ChannelID: match[2],
			MessageID: match[3],
		}
	}
	if input != "" && c.Message != nil {
		return &MessageLinkInfo{
			GuildID:   c.Message.GuildID,
			ChannelID: c.Message.ChannelID,
			MessageID: input,
		}
	}
	return nil
}

func (c *Context) FetchMessageFromInput(input string) (*discordgo.Message, error) {
	linkInfo := c.ParseMessageLink(input)
	if linkInfo == nil || linkInfo.MessageID == "" {
		return nil, fmt.Errorf("invalid message ID or Discord message link")
	}

	channelID := linkInfo.ChannelID
	if channelID == "" {
		if c.Message != nil {
			channelID = c.Message.ChannelID
		}
	}
	if channelID == "" {
		return nil, fmt.Errorf("could not determine channel ID")
	}

	ch, err := helpers.GetChannel(c.Session, channelID)
	if err != nil || ch == nil {
		return nil, fmt.Errorf("failed to access channel: %w", err)
	}

	if c.Message != nil && c.Message.GuildID != "" {
		if ch.GuildID != c.Message.GuildID {
			return nil, fmt.Errorf("cannot access messages from another server")
		}
		if !c.IsOwner() {
			if isOwner, errOwner := helpers.IsGuildOwner(c.Session, c.Message.GuildID, c.Message.Author.ID); !isOwner || errOwner != nil {
				perms, errP := c.Session.UserChannelPermissions(c.Message.Author.ID, ch.ID)
				if errP != nil {
					return nil, fmt.Errorf("failed to verify channel permissions: %w", errP)
				}
				if perms&discordgo.PermissionAdministrator == 0 &&
					(perms&discordgo.PermissionViewChannel == 0 || perms&discordgo.PermissionReadMessageHistory == 0) {
					return nil, fmt.Errorf("you do not have permission to view messages in that channel")
				}
			}
		}
	} else if c.Message != nil && c.Message.ChannelID != "" {
		if ch.ID != c.Message.ChannelID {
			return nil, fmt.Errorf("cannot access messages outside this channel")
		}
	}

	msg, err := c.Session.ChannelMessage(channelID, linkInfo.MessageID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch message: %w", err)
	}
	return msg, nil
}

func (c *Context) Guild() (*discordgo.Guild, error) {
	if c.Message == nil || c.Message.GuildID == "" {
		return nil, errors.New("command not invoked in a guild")
	}
	return helpers.GetGuild(c.Session, c.Message.GuildID)
}

func (c *Context) GetMember(userID string) (*discordgo.Member, error) {
	if c.Message == nil || c.Message.GuildID == "" {
		return nil, errors.New("command not invoked in a guild")
	}
	return helpers.GetGuildMember(c.Session, c.Message.GuildID, userID)
}

func (c *Context) IsGuildOwner(userIDs ...string) bool {
	if c.Message == nil || c.Message.GuildID == "" {
		return false
	}
	targetID := ""
	if len(userIDs) > 0 {
		targetID = userIDs[0]
	} else if c.Message.Author != nil {
		targetID = c.Message.Author.ID
	}
	if targetID == "" {
		return false
	}
	isOwner, err := helpers.IsGuildOwner(c.Session, c.Message.GuildID, targetID)
	return err == nil && isOwner
}

func (c *Context) ParseBool(arg string) (bool, bool) {
	return helpers.ParseBool(arg)
}

func (c *Context) getGuildMember(user *discordgo.User) (*discordgo.Member, error) {
	if user == nil || c.Message == nil || c.Message.GuildID == "" {
		return nil, nil
	}
	return helpers.GetGuildMemberWithUserErr(c.Session, c.Message.GuildID, user)
}

func (c *Context) getAuthorMember() *discordgo.Member {
	if c.Message == nil || c.Message.GuildID == "" {
		return nil
	}
	if c.Message.Member != nil {
		if c.Message.Member.User == nil && c.Message.Author != nil {
			c.Message.Member.User = c.Message.Author
		}
		return c.Message.Member
	}
	m, _ := c.getGuildMember(c.Message.Author)
	return m
}

func (c *Context) ResolveUserAndMember(query string) (*discordgo.User, *discordgo.Member, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil, errors.New("no user specified")
	}

	if uid := c.ParseUserID(query); uid != "" {
		var member *discordgo.Member
		var memberErr error
		if c.Message != nil && c.Message.GuildID != "" {
			member, memberErr = helpers.GetGuildMember(c.Session, c.Message.GuildID, uid)
			if memberErr != nil && !helpers.IsDiscordNotFound(memberErr) {
				return nil, nil, fmt.Errorf("failed to verify guild membership for user `%s`: %w", uid, memberErr)
			}
		}

		u, errU := c.Session.User(uid)
		if member != nil {
			if member.User == nil && u != nil {
				member.User = u
			}
			if member.User != nil {
				return member.User, member, nil
			}
		}

		if errU == nil && u != nil {
			return u, member, nil
		}
	}

	cleanQuery := strings.TrimPrefix(query, "@")

	if c.Message.GuildID != "" {
		members, err := c.Session.GuildMembersSearch(c.Message.GuildID, cleanQuery, 10)
		if err == nil && len(members) > 0 {
			for _, m := range members {
				if m.User != nil && (strings.EqualFold(m.User.Username, cleanQuery) || strings.EqualFold(m.Nick, cleanQuery)) {
					return m.User, m, nil
				}
			}
			if members[0].User != nil {
				return members[0].User, members[0], nil
			}
		}

		if c.Session.State != nil {
			if guild, err := c.Session.State.Guild(c.Message.GuildID); err == nil && guild != nil {
				for _, m := range guild.Members {
					if m.User != nil && (strings.EqualFold(m.User.Username, cleanQuery) || strings.EqualFold(m.Nick, cleanQuery)) {
						targetUser := m.User
						targetMember := m
						return targetUser, targetMember, nil
					}
				}
			}
		}
	}

	return nil, nil, fmt.Errorf("user `%s` not found", query)
}

func (c *Context) TargetUserAndMember(argIndex ...int) (*discordgo.User, *discordgo.Member, error) {
	idx := 0
	if len(argIndex) > 0 {
		idx = argIndex[0]
	}
	u, m, _, err := c.TargetUserAndMemberArg(idx)
	return u, m, err
}

func (c *Context) TargetUserAndMemberArg(argIndex int) (*discordgo.User, *discordgo.Member, bool, error) {
	if len(c.Args) > argIndex {
		u, m, err := c.ResolveUserAndMember(c.Args[argIndex])
		if err != nil || u == nil {
			return nil, nil, false, fmt.Errorf("could not find user `%s`", c.Args[argIndex])
		}
		return u, m, true, nil
	}

	if c.Message.ReferencedMessage != nil && c.Message.ReferencedMessage.Author != nil {
		targetUser := c.Message.ReferencedMessage.Author
		member, err := c.getGuildMember(targetUser)
		if err != nil && !helpers.IsDiscordNotFound(err) {
			return nil, nil, false, fmt.Errorf("failed to verify guild membership for user `%s`: %w", targetUser.ID, err)
		}
		return targetUser, member, false, nil
	}

	if len(c.Message.Mentions) > 0 {
		targetUser := c.Message.Mentions[0]
		member, err := c.getGuildMember(targetUser)
		if err != nil && !helpers.IsDiscordNotFound(err) {
			return nil, nil, false, fmt.Errorf("failed to verify guild membership for user `%s`: %w", targetUser.ID, err)
		}
		return targetUser, member, false, nil
	}

	return c.Message.Author, c.getAuthorMember(), false, nil
}

func (c *Context) ResolveRole(query string) (*discordgo.Role, error) {
	if c.Message.GuildID == "" {
		return nil, errors.New("command must be executed in a server")
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("no role, role ID, or role name specified")
	}

	roleID := c.ParseRoleID(query)
	guildID := c.Message.GuildID

	var roles []*discordgo.Role
	guild, err := c.Session.State.Guild(guildID)
	if err == nil && guild != nil && len(guild.Roles) > 0 {
		roles = guild.Roles
	} else {
		roles, err = c.Session.GuildRoles(guildID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch server roles: %w", err)
		}
	}

	for _, r := range roles {
		if r.ID == roleID {
			return r, nil
		}
	}

	lowerQuery := strings.ToLower(query)
	for _, r := range roles {
		if strings.ToLower(r.Name) == lowerQuery {
			return r, nil
		}
	}

	var partialMatches []*discordgo.Role
	for _, r := range roles {
		if strings.Contains(strings.ToLower(r.Name), lowerQuery) {
			partialMatches = append(partialMatches, r)
		}
	}

	if len(partialMatches) == 1 {
		return partialMatches[0], nil
	}
	if len(partialMatches) > 1 {
		names := make([]string, 0, len(partialMatches))
		for _, r := range partialMatches {
			names = append(names, fmt.Sprintf("%q (%s)", r.Name, r.ID))
		}
		return nil, fmt.Errorf("multiple roles matched %q: %s; Specify an exact role mention or ID", query, strings.Join(names, ", "))
	}

	return nil, fmt.Errorf("role not found for query: `%s`", query)
}

func (c *Context) ResolveChannel(query string) (*discordgo.Channel, error) {
	if c.Message == nil || c.Message.GuildID == "" {
		return nil, errors.New("command must be executed in a server")
	}
	return helpers.ResolveGuildChannel(c.Session, c.Message.GuildID, query)
}

func (c *Context) ExtractImageURL() string {
	if c.Message == nil {
		return ""
	}

	for _, arg := range c.Args {
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			return arg
		}
	}

	if len(c.Message.Attachments) > 0 {
		for _, att := range c.Message.Attachments {
			if att.Width > 0 || att.Height > 0 || helpers.IsImageURL(att.URL) {
				return att.URL
			}
		}
		return c.Message.Attachments[0].URL
	}

	if c.Message.ReferencedMessage != nil {
		ref := c.Message.ReferencedMessage
		if len(ref.Attachments) > 0 {
			for _, att := range ref.Attachments {
				if att.Width > 0 || att.Height > 0 || helpers.IsImageURL(att.URL) {
					return att.URL
				}
			}
			return ref.Attachments[0].URL
		}
		if len(ref.Embeds) > 0 {
			if ref.Embeds[0].Image != nil && ref.Embeds[0].Image.URL != "" {
				return ref.Embeds[0].Image.URL
			}
			if ref.Embeds[0].Thumbnail != nil && ref.Embeds[0].Thumbnail.URL != "" {
				return ref.Embeds[0].Thumbnail.URL
			}
		}
	}

	return ""
}
