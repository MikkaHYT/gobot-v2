package modlog

import (
	"fmt"
	"strings"
	"time"

	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type Event interface {
	Guild() string
	Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File)
}

// overflowToFile truncates text > limit and returns the full text as a file
func overflowToFile(text, filename string, limit int) (string, *discordgo.File) {
	if len([]rune(text)) <= limit {
		return helpers.EscapeMarkdownContent(text), nil
	}
	truncated := helpers.EscapeMarkdownContent(helpers.TruncateString(text, limit))
	display := fmt.Sprintf("%s...\n> *(Full text attached as `%s`)*", truncated, filename)
	file := &discordgo.File{
		Name:        filename,
		ContentType: "text/plain",
		Reader:      strings.NewReader(text),
	}
	return display, file
}

func resolveUserAvatar(s *discordgo.Session, guildID string, u *discordgo.User, directAvatarURL string) string {
	if directAvatarURL != "" {
		return directAvatarURL
	}
	if u == nil {
		return helpers.UserAvatar(nil)
	}
	if u.Avatar != "" {
		return helpers.UserAvatar(u)
	}
	if s != nil && u.ID != "" {
		if guildID != "" {
			if member, err := helpers.GetGuildMember(s, guildID, u.ID); err == nil && member != nil {
				return helpers.MemberAvatar(member)
			}
		}
		if fetched, err := s.User(u.ID); err == nil && fetched != nil && fetched.Avatar != "" {
			return fetched.AvatarURL("256")
		}
	}
	return helpers.UserAvatar(u)
}

type AutoModViolationEvent struct {
	GuildID        string
	TargetUser     *discordgo.User
	RuleName       string
	Action         string
	ChannelID      string
	MatchedKeyword string
	MatchedContent string
	Content        string
}

func (e *AutoModViolationEvent) Guild() string { return e.GuildID }

func (e *AutoModViolationEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.TargetUser == nil {
		return nil, nil
	}

	rawContent := e.Content
	if rawContent == "" {
		rawContent = e.MatchedContent
	}

	triggerType := "Keyword Match"
	cleanKeyword := e.MatchedKeyword
	if strings.HasPrefix(e.MatchedKeyword, "regex:") {
		triggerType = "Regex Pattern"
		cleanKeyword = strings.TrimSpace(strings.TrimPrefix(e.MatchedKeyword, "regex:"))
	} else if strings.Contains(strings.ToLower(e.RuleName), "invite") || strings.Contains(strings.ToLower(cleanKeyword), "invite") {
		triggerType = "Discord Invite Link"
	}

	accountCreatedStr := "Unknown"
	if createdTS, err := discordgo.SnowflakeTimestamp(e.TargetUser.ID); err == nil && !createdTS.IsZero() {
		accountCreatedStr = fmt.Sprintf("<t:%d:F> (<t:%d:R>)", createdTS.Unix(), createdTS.Unix())
	}

	topBlock := fmt.Sprintf("> **Target User** %s (`%s`)\n> **Account Created** %s\n> **Channel** <#%s>\n> **Punishment Executed** `%s`",
		e.TargetUser.Mention(), e.TargetUser.ID, accountCreatedStr, e.ChannelID, e.Action)

	var files []*discordgo.File
	bodyContent := "*(No text content)*"
	if rawContent != "" {
		display, file := overflowToFile(rawContent, "content.txt", 1000)
		bodyContent = display
		if file != nil {
			files = append(files, file)
		}
	}

	filterBlock := fmt.Sprintf("> **Rule** %s\n> **Trigger Type** %s", e.RuleName, triggerType)
	if cleanKeyword != "" {
		filterBlock += fmt.Sprintf("\n> **Matched Pattern** `%s`", cleanKeyword)
	}
	filterBlock += fmt.Sprintf("\n> **Content** %s", bodyContent)

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.TargetUser, "")),
		helpers.TextDisplay("### AutoMod Alert"),
		helpers.TextDisplay(topBlock),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay("### Filter Details & Match"),
		helpers.TextDisplay(filterBlock),
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# AutoMod • %s • User ID %s", time.Now().Format("Jan 2, 2006 3:04 PM"), e.TargetUser.ID)),
	}

	return components, files
}

type PunishmentEvent struct {
	GuildID    string
	Action     string
	TargetUser *discordgo.User
	Moderator  *discordgo.User
	Duration   string
	Reason     string
	CaseID     int64
}

func (e *PunishmentEvent) Guild() string { return e.GuildID }

func (e *PunishmentEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.TargetUser == nil {
		return nil, nil
	}

	modStr := "System"
	if e.Moderator != nil {
		modStr = fmt.Sprintf("%s (`%s`)", e.Moderator.String(), e.Moderator.ID)
	}

	reason := e.Reason
	if reason == "" {
		reason = "No reason specified"
	}

	title := fmt.Sprintf("Member %s", e.Action)
	if e.CaseID > 0 {
		title = fmt.Sprintf("Case #%d | Member %s", e.CaseID, e.Action)
	}

	bodyText := fmt.Sprintf("> **Target Member** %s (`%s`)\n> **Action** %s\n> **Responsible Mod** %s",
		e.TargetUser.String(), e.TargetUser.ID, e.Action, modStr)
	if e.Duration != "" {
		bodyText += fmt.Sprintf("\n> **Duration** %s", e.Duration)
	}
	bodyText += fmt.Sprintf("\n> **Reason** %s", reason)

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.TargetUser, "")),
		helpers.TextDisplay(fmt.Sprintf("### %s", title)),
		helpers.TextDisplay(bodyText),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Moderation Event • %s • User ID %s", time.Now().Format("Jan 2, 2006 3:04 PM"), e.TargetUser.ID)),
	}

	return components, nil
}

type MessageDeleteEvent struct {
	GuildID      string
	Author       *discordgo.User
	AuthorAvatar string
	ChannelID    string
	MessageID    string
	Content      string
	Attachments  []string
	SentAt       time.Time
}

func (e *MessageDeleteEvent) Guild() string { return e.GuildID }

func (e *MessageDeleteEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	authorStr := "Unknown User"
	authorID := "N/A"
	if e.Author != nil {
		authorStr = e.Author.String()
		authorID = e.Author.ID
	}

	content := e.Content
	if content == "" {
		content = "*(No text content)*"
	}

	createdStr := "Unknown"
	if !e.SentAt.IsZero() {
		createdStr = fmt.Sprintf("<t:%d:F> (<t:%d:R>)", e.SentAt.Unix(), e.SentAt.Unix())
	}

	section1Content := fmt.Sprintf("> **Author** %s (`%s`)\n> **Channel** <#%s>\n> **Message Sent** %s",
		authorStr, authorID, e.ChannelID, createdStr)

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.Author, e.AuthorAvatar)),
		helpers.TextDisplay("### Message Deleted"),
		helpers.TextDisplay(section1Content),
	)

	var files []*discordgo.File
	displayContent, file := overflowToFile(content, "deleted_message.txt", 1000)
	if file != nil {
		files = append(files, file)
	}

	detailsContent := fmt.Sprintf("> **Content** %s", displayContent)
	if len(e.Attachments) > 0 {
		detailsContent += fmt.Sprintf("\n> **Attached Files (%d)**\n> %s", len(e.Attachments), strings.Join(e.Attachments, "\n> "))
	}

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay("### Deleted Content"),
		helpers.TextDisplay(detailsContent),
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Message ID: %s • %s", e.MessageID, time.Now().Format("Jan 2, 2006 3:04 PM"))),
	}

	return components, files
}

type MessageEditEvent struct {
	GuildID      string
	Author       *discordgo.User
	AuthorAvatar string
	ChannelID    string
	MessageID    string
	OldContent   string
	NewContent   string
}

func (e *MessageEditEvent) Guild() string { return e.GuildID }

func (e *MessageEditEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	authorStr := "Unknown User"
	authorID := "N/A"
	if e.Author != nil {
		authorStr = e.Author.String()
		authorID = e.Author.ID
	}

	oldContent := e.OldContent
	if oldContent == "" {
		oldContent = "*(Previous content unavailable)*"
	}
	newContent := e.NewContent
	if newContent == "" {
		newContent = "*(No text content)*"
	}

	jumpURL := helpers.MessageURL(e.GuildID, e.ChannelID, e.MessageID)
	section1Content := fmt.Sprintf("> **Author** %s (`%s`)\n> **Channel** <#%s>\n> **Link** [Jump to Message](%s)",
		authorStr, authorID, e.ChannelID, jumpURL)

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.Author, e.AuthorAvatar)),
		helpers.TextDisplay("### Message Edited"),
		helpers.TextDisplay(section1Content),
	)

	var files []*discordgo.File
	displayOld, oldFile := overflowToFile(oldContent, "old_content.txt", 800)
	if oldFile != nil {
		files = append(files, oldFile)
	}
	displayNew, newFile := overflowToFile(newContent, "new_content.txt", 800)
	if newFile != nil {
		files = append(files, newFile)
	}

	changesContent := fmt.Sprintf("> **Before** %s\n> **After** %s", displayOld, displayNew)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay("### Content Changes"),
		helpers.TextDisplay(changesContent),
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Message ID: %s • %s", e.MessageID, time.Now().Format("Jan 2, 2006 3:04 PM"))),
	}

	return components, files
}

type MemberRoleUpdateEvent struct {
	GuildID    string
	TargetUser *discordgo.User
	RoleID     string
	IsAdd      bool
	Moderator  *discordgo.User
}

func (e *MemberRoleUpdateEvent) Guild() string { return e.GuildID }

func (e *MemberRoleUpdateEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.TargetUser == nil {
		return nil, nil
	}

	title := "Member Role Updated"
	actionText := fmt.Sprintf("Granted <@&%s> (`%s`)", e.RoleID, e.RoleID)
	if !e.IsAdd {
		actionText = fmt.Sprintf("Removed <@&%s> (`%s`)", e.RoleID, e.RoleID)
	}

	modStr := "System"
	if e.Moderator != nil {
		modStr = fmt.Sprintf("%s (`%s`)", e.Moderator.String(), e.Moderator.ID)
	}

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.TargetUser, "")),
		helpers.TextDisplay(fmt.Sprintf("### %s", title)),
		helpers.TextDisplay(fmt.Sprintf("> **Target User** %s (`%s`)\n> **Action** %s\n> **Moderator** %s",
			e.TargetUser.Mention(), e.TargetUser.ID, actionText, modStr)),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Role Update • %s • User ID %s", time.Now().Format("Jan 2, 2006 3:04 PM"), e.TargetUser.ID)),
	}

	return components, nil
}

type NicknameChangeEvent struct {
	GuildID string
	User    *discordgo.User
	OldNick string
	NewNick string
}

func (e *NicknameChangeEvent) Guild() string { return e.GuildID }

func (e *NicknameChangeEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.User == nil {
		return nil, nil
	}

	oldNick := e.OldNick
	if oldNick == "" {
		oldNick = "*(No Nickname)*"
	}
	newNick := e.NewNick
	if newNick == "" {
		newNick = "*(No Nickname)*"
	}

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.User, "")),
		helpers.TextDisplay("### Nickname Updated"),
		helpers.TextDisplay(fmt.Sprintf("> **User** %s (`%s`)\n> **Old Nickname** %s\n> **New Nickname** %s",
			e.User.String(), e.User.ID, helpers.EscapeMarkdown(oldNick), helpers.EscapeMarkdown(newNick))),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Member Update • %s • User ID %s", time.Now().Format("Jan 2, 2006 3:04 PM"), e.User.ID)),
	}

	return components, nil
}

type MemberJoinEvent struct {
	GuildID       string
	User          *discordgo.User
	RestoredRoles []string
}

func (e *MemberJoinEvent) Guild() string { return e.GuildID }

func (e *MemberJoinEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.User == nil {
		return nil, nil
	}

	createdTS, _ := discordgo.SnowflakeTimestamp(e.User.ID)
	createdStr := "Unknown"
	if !createdTS.IsZero() {
		createdStr = fmt.Sprintf("<t:%d:F> (<t:%d:R>)", createdTS.Unix(), createdTS.Unix())
	}

	body := fmt.Sprintf("> **User** %s (`%s`)\n> **Account Created** %s", e.User.Mention(), e.User.ID, createdStr)
	if len(e.RestoredRoles) > 0 {
		var mentions []string
		for _, rID := range e.RestoredRoles {
			mentions = append(mentions, fmt.Sprintf("<@&%s>", rID))
		}
		body += fmt.Sprintf("\n> **Restored Roles** %s", strings.Join(mentions, ", "))
	}

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.User, "")),
		helpers.TextDisplay("### Member Joined Server"),
		helpers.TextDisplay(body),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Member Join • %s • User ID %s", time.Now().Format("Jan 2, 2006 3:04 PM"), e.User.ID)),
	}

	return components, nil
}

type MemberLeaveEvent struct {
	GuildID string
	User    *discordgo.User
}

func (e *MemberLeaveEvent) Guild() string { return e.GuildID }

func (e *MemberLeaveEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.User == nil {
		return nil, nil
	}

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.User, "")),
		helpers.TextDisplay("### Member Left Server"),
		helpers.TextDisplay(fmt.Sprintf("> **User** %s (`%s`)", e.User.String(), e.User.ID)),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Member Leave • %s • User ID %s", time.Now().Format("Jan 2, 2006 3:04 PM"), e.User.ID)),
	}

	return components, nil
}

type MemberAuditEvent struct {
	GuildID   string
	Action    string
	User      *discordgo.User
	Moderator *discordgo.User
	Reason    string
}

func (e *MemberAuditEvent) Guild() string { return e.GuildID }

func (e *MemberAuditEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.User == nil {
		return nil, nil
	}

	modStr := "System / Native Discord"
	if e.Moderator != nil {
		modStr = fmt.Sprintf("%s (`%s`)", e.Moderator.String(), e.Moderator.ID)
	}

	reason := e.Reason
	if reason == "" {
		reason = "No reason specified"
	}

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.User, "")),
		helpers.TextDisplay(fmt.Sprintf("### %s", e.Action)),
		helpers.TextDisplay(fmt.Sprintf("> **Target User** %s (`%s`)\n> **Moderator** %s\n> **Reason** %s",
			e.User.Mention(), e.User.ID, modStr, reason)),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Member Event • %s • User ID %s", time.Now().Format("Jan 2, 2006 3:04 PM"), e.User.ID)),
	}

	return components, nil
}

type ChannelEvent struct {
	GuildID     string
	Action      string
	ChannelName string
	ChannelID   string
	Moderator   *discordgo.User
	Reason      string
}

func (e *ChannelEvent) Guild() string { return e.GuildID }

func (e *ChannelEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	modStr := "System"
	if e.Moderator != nil {
		modStr = fmt.Sprintf("%s (`%s`)", e.Moderator.String(), e.Moderator.ID)
	}

	reason := e.Reason
	if reason == "" {
		reason = "N/A"
	}

	chanRef := fmt.Sprintf("#%s (`%s`)", e.ChannelName, e.ChannelID)
	if e.ChannelID != "" && e.ChannelName != "" && e.ChannelName != e.ChannelID {
		chanRef = fmt.Sprintf("<#%s> (`%s`)", e.ChannelID, e.ChannelID)
	} else if e.ChannelID != "" {
		chanRef = fmt.Sprintf("<#%s> (`%s`)", e.ChannelID, e.ChannelID)
	} else if e.ChannelName != "" {
		chanRef = e.ChannelName
	}

	var accessory map[string]interface{}
	if e.Moderator != nil {
		accessory = helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.Moderator, ""))
	}

	section1 := helpers.SectionWithAccessory(
		accessory,
		helpers.TextDisplay(fmt.Sprintf("### %s", e.Action)),
		helpers.TextDisplay(fmt.Sprintf("> **Target Channel** %s\n> **Moderator** %s\n> **Reason** %s", chanRef, modStr, reason)),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Channel log • %s", time.Now().Format("Jan 2, 2006 3:04 PM"))),
	}

	return components, nil
}

type VoiceEvent struct {
	GuildID   string
	Action    string
	User      *discordgo.User
	ChannelID string
}

func (e *VoiceEvent) Guild() string { return e.GuildID }

func (e *VoiceEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.User == nil {
		return nil, nil
	}

	chanRef := fmt.Sprintf("<#%s>", e.ChannelID)
	if e.ChannelID == "" {
		chanRef = "Unknown Channel"
	}

	section1 := helpers.SectionWithAccessory(
		helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.User, "")),
		helpers.TextDisplay(fmt.Sprintf("### Voice State: %s", e.Action)),
		helpers.TextDisplay(fmt.Sprintf("> **User** %s (`%s`)\n> **Channel** %s", e.User.Mention(), e.User.ID, chanRef)),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Voice Event • %s • User ID %s", time.Now().Format("Jan 2, 2006 3:04 PM"), e.User.ID)),
	}

	return components, nil
}

type RoleEvent struct {
	GuildID   string
	Action    string
	Role      *discordgo.Role
	Moderator *discordgo.User
	Details   string
}

func (e *RoleEvent) Guild() string { return e.GuildID }

func (e *RoleEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	if e.Role == nil {
		return nil, nil
	}

	modStr := "System"
	if e.Moderator != nil {
		modStr = fmt.Sprintf("%s (`%s`)", e.Moderator.String(), e.Moderator.ID)
	}

	roleRef := fmt.Sprintf("<@&%s> (`%s`)", e.Role.ID, e.Role.ID)
	if e.Role.Name != "" {
		roleRef = fmt.Sprintf("@%s (`%s`)", e.Role.Name, e.Role.ID)
	}

	var accessory map[string]interface{}
	if e.Moderator != nil {
		accessory = helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.Moderator, ""))
	}

	body := fmt.Sprintf("> **Role** %s\n> **Moderator** %s", roleRef, modStr)
	if e.Details != "" {
		body += fmt.Sprintf("\n%s", e.Details)
	}

	section1 := helpers.SectionWithAccessory(
		accessory,
		helpers.TextDisplay(fmt.Sprintf("### %s", e.Action)),
		helpers.TextDisplay(body),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Role Event • %s", time.Now().Format("Jan 2, 2006 3:04 PM"))),
	}

	return components, nil
}

type EmojiEvent struct {
	GuildID   string
	Action    string
	Name      string
	EmojiID   string
	Moderator *discordgo.User
	Details   string
}

func (e *EmojiEvent) Guild() string { return e.GuildID }

func (e *EmojiEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	modStr := "System"
	if e.Moderator != nil {
		modStr = fmt.Sprintf("%s (`%s`)", e.Moderator.String(), e.Moderator.ID)
	}

	emojiRef := e.Name
	if e.EmojiID != "" {
		emojiRef = fmt.Sprintf("<:%s:%s> (`%s`)", e.Name, e.EmojiID, e.EmojiID)
	}

	body := fmt.Sprintf("> **Emoji** %s\n> **Moderator** %s", emojiRef, modStr)
	if e.Details != "" {
		body += fmt.Sprintf("\n%s", e.Details)
	}

	var accessory map[string]interface{}
	if e.Moderator != nil {
		accessory = helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.Moderator, ""))
	}

	section1 := helpers.SectionWithAccessory(
		accessory,
		helpers.TextDisplay(fmt.Sprintf("### %s", e.Action)),
		helpers.TextDisplay(body),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Emoji Event • %s", time.Now().Format("Jan 2, 2006 3:04 PM"))),
	}

	return components, nil
}

type GiveawayEvent struct {
	GuildID     string
	Action      string
	Prize       string
	ChannelID   string
	MessageID   string
	HostID      string
	WinnersText string
	Actor       *discordgo.User
}

func (e *GiveawayEvent) Guild() string { return e.GuildID }

func (e *GiveawayEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	hostMention := fmt.Sprintf("<@%s>", e.HostID)
	if s != nil && e.HostID != "" {
		if hostUser, err := s.User(e.HostID); err == nil && hostUser != nil {
			hostMention = fmt.Sprintf("%s (`%s`)", hostUser.String(), e.HostID)
		}
	}

	content := fmt.Sprintf("> **Prize:** %s\n> **Channel:** <#%s>\n> **Hosted By:** %s",
		helpers.EscapeMarkdown(e.Prize), e.ChannelID, hostMention)
	if e.WinnersText != "" {
		content += fmt.Sprintf("\n> **Winners:** %s", e.WinnersText)
	}
	if e.Actor != nil {
		content += fmt.Sprintf("\n> **Action By:** %s (`%s`)", e.Actor.String(), e.Actor.ID)
	}
	if e.MessageID != "" {
		content += fmt.Sprintf("\n> **Message Link:** [Jump to Message](%s)", helpers.MessageURL(e.GuildID, e.ChannelID, e.MessageID))
	}

	var accessory map[string]interface{}
	if e.Actor != nil {
		accessory = helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.Actor, ""))
	}

	section1 := helpers.SectionWithAccessory(
		accessory,
		helpers.TextDisplay(fmt.Sprintf("### Giveaway %s", e.Action)),
		helpers.TextDisplay(content),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Giveaway Log • %s", time.Now().Format("Jan 2, 2006 3:04 PM"))),
	}

	return components, nil
}

type ConfigChangeEvent struct {
	GuildID   string
	Setting   string
	OldValue  string
	NewValue  string
	Moderator *discordgo.User
	Details   string
}

func (e *ConfigChangeEvent) Guild() string { return e.GuildID }

func (e *ConfigChangeEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	modStr := "System"
	if e.Moderator != nil {
		modStr = fmt.Sprintf("%s (`%s`)", e.Moderator.String(), e.Moderator.ID)
	}

	body := fmt.Sprintf("> **Setting** %s\n> **Moderator** %s", e.Setting, modStr)
	if e.OldValue != "" {
		body += fmt.Sprintf("\n> **Previous** %s", e.OldValue)
	}
	if e.NewValue != "" {
		body += fmt.Sprintf("\n> **New** %s", e.NewValue)
	}
	if e.Details != "" {
		body += fmt.Sprintf("\n> **Details** %s", e.Details)
	}

	var accessory map[string]interface{}
	if e.Moderator != nil {
		accessory = helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.Moderator, ""))
	}

	section1 := helpers.SectionWithAccessory(
		accessory,
		helpers.TextDisplay(fmt.Sprintf("### Server Configuration: %s", e.Setting)),
		helpers.TextDisplay(body),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Configuration Log • %s", time.Now().Format("Jan 2, 2006 3:04 PM"))),
	}

	return components, nil
}

type BlacklistEvent struct {
	GuildID    string
	Action     string
	TargetType string
	TargetName string
	TargetID   string
	Moderator  *discordgo.User
	Reason     string
}

func (e *BlacklistEvent) Guild() string { return e.GuildID }

func (e *BlacklistEvent) Build(s *discordgo.Session) ([]map[string]interface{}, []*discordgo.File) {
	modStr := "System"
	if e.Moderator != nil {
		modStr = fmt.Sprintf("%s (`%s`)", e.Moderator.String(), e.Moderator.ID)
	}

	title := fmt.Sprintf("%s %s", e.Action, e.TargetType)
	if e.TargetType == "" {
		title = e.Action
	}

	targetRef := e.TargetName
	if e.TargetID != "" && e.TargetID != e.TargetName {
		targetRef = fmt.Sprintf("%s (`%s`)", e.TargetName, e.TargetID)
	}

	body := fmt.Sprintf("> **Target** %s\n> **Moderator** %s", targetRef, modStr)
	if e.Reason != "" {
		body += fmt.Sprintf("\n> **Reason** %s", e.Reason)
	}

	var accessory map[string]interface{}
	if e.Moderator != nil {
		accessory = helpers.MediaAccessory(resolveUserAvatar(s, e.GuildID, e.Moderator, ""))
	}

	section1 := helpers.SectionWithAccessory(
		accessory,
		helpers.TextDisplay(fmt.Sprintf("### %s", title)),
		helpers.TextDisplay(body),
	)

	components := []map[string]interface{}{
		section1,
		helpers.Separator(),
		helpers.TextDisplay(fmt.Sprintf("-# Blacklist Log • %s", time.Now().Format("Jan 2, 2006 3:04 PM"))),
	}

	return components, nil
}
