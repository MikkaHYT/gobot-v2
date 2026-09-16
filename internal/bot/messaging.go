package bot

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"gobot/internal/helpers"
	"gobot/internal/listeners"

	"github.com/bwmarrin/discordgo"
)

func (c *Context) prepareEmbed(embed *discordgo.MessageEmbed) *discordgo.MessageEmbed {
	if embed != nil && (embed.Color == 0 || embed.Color == helpers.ColorDefault) {
		if c.GuildConfig != nil && c.GuildConfig.EmbedColor != "" {
			if col := helpers.ParseHexColor(c.GuildConfig.EmbedColor); col != 0 {
				embed.Color = col
				return embed
			}
		}
		if c.Config != nil && c.Config.DefaultEmbedColor != 0 {
			embed.Color = c.Config.DefaultEmbedColor
		} else {
			embed.Color = helpers.ColorDefault
		}
	}
	return embed
}

func (c *Context) ReplyEmbed(embed *discordgo.MessageEmbed) (*discordgo.Message, error) {
	return c.Session.ChannelMessageSendComplex(c.Message.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{c.prepareEmbed(embed)},
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	})
}

func (c *Context) ReplyText(content string) (*discordgo.Message, error) {
	return c.Session.ChannelMessageSendComplex(c.Message.ChannelID, &discordgo.MessageSend{
		Content: content,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	})
}

func (c *Context) SendFile(name string, r io.Reader) (*discordgo.Message, error) {
	return c.Session.ChannelFileSend(c.Message.ChannelID, name, r)
}

func (c *Context) SendFileWithEmbed(file *discordgo.File, embed *discordgo.MessageEmbed) (*discordgo.Message, error) {
	return c.Session.ChannelMessageSendComplex(c.Message.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{c.prepareEmbed(embed)},
		Files:  []*discordgo.File{file},
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	})
}

func (c *Context) ReactSuccess() error {
	return c.Session.MessageReactionAdd(c.Message.ChannelID, c.Message.ID, "✔️")
}

func (c *Context) SendSuccess(format string, a ...any) error {
	_ = c.ReactSuccess()
	_, err := c.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf(format, a...),
	})
	return err
}

func (c *Context) SendError(desc string) error {
	_, err := c.ReplyEmbed(&discordgo.MessageEmbed{
		Description: CleanErrorMessage(desc),
		Color:       helpers.ColorDarkRed,
	})
	return err
}

func CleanErrorMessage(raw string) string {
	if raw = strings.TrimSpace(raw); raw == "" {
		return "An unexpected error occurred."
	}
	low := strings.ToLower(raw)

	if strings.Contains(low, "50013") || (strings.Contains(low, "403") && strings.Contains(low, "missing permissions")) {
		switch {
		case strings.Contains(low, "ban"):
			return "I do not have permission to ban this user. Make sure my role is higher than theirs in Server Settings."
		case strings.Contains(low, "kick"):
			return "I do not have permission to kick this user. Make sure my role is higher than theirs in Server Settings."
		case strings.Contains(low, "role"):
			return "I do not have permission to manage this role. Make sure my role is placed higher than the target role."
		case strings.Contains(low, "timeout") || strings.Contains(low, "mute"):
			return "I do not have permission to timeout/mute this user. Make sure my role is higher than theirs."
		default:
			return "I do not have permission to perform this action. Please check my role hierarchy and permissions."
		}
	}

	rules := []struct{ key, msg string }{
		{"50001", "I am missing access to perform this action in this channel or server."},
		{"missing access", "I am missing access to perform this action in this channel or server."},
		{"10013", "User not found. Provide a valid user mention or ID."},
		{"unknown user", "User not found. Provide a valid user mention or ID."},
		{"10007", "That member could not be found in this server."},
		{"unknown member", "That member could not be found in this server."},
		{"10003", "The specified channel could not be found."},
		{"unknown channel", "The specified channel could not be found."},
		{"10011", "The specified role could not be found."},
		{"unknown role", "The specified role could not be found."},
		{"50006", "Cannot send an empty message."},
		{"cannot send an empty message", "Cannot send an empty message."},
		{"50035", "Invalid argument format provided."},
		{"invalid form body", "Invalid argument format provided."},
	}
	for _, r := range rules {
		if strings.Contains(low, r.key) {
			return r.msg
		}
	}

	if start := strings.Index(raw, `{"message": "`); start != -1 {
		start += 13
		if end := strings.Index(raw[start:], `"`); end != -1 {
			jsonMsg := raw[start : start+end]
			if idx := strings.Index(raw, ": HTTP "); idx != -1 {
				return raw[:idx] + ": " + jsonMsg
			}
			return jsonMsg
		}
	}

	return raw
}

type CommandInfo interface {
	Name() string
	Usage() string
	Example() string
}

func (c *Context) FormatUsage(cmd CommandInfo) string {
	u := strings.TrimSpace(cmd.Usage())
	if u == "" {
		return fmt.Sprintf("`%s%s`", c.Prefix, cmd.Name())
	}
	return fmt.Sprintf("`%s%s %s`", c.Prefix, cmd.Name(), u)
}

func (c *Context) FormatExample(cmd CommandInfo) string {
	e := strings.TrimSpace(cmd.Example())
	if e == "" {
		return fmt.Sprintf("`%s%s`", c.Prefix, cmd.Name())
	}
	return fmt.Sprintf("`%s%s %s`", c.Prefix, cmd.Name(), e)
}

func (c *Context) SendUsage(cmd CommandInfo) (*discordgo.Message, error) {
	return c.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("**Usage:** %s\n**Example:** %s", c.FormatUsage(cmd), c.FormatExample(cmd)),
	})
}

func (c *Context) SendPaginatedEmbeds(embeds []*discordgo.MessageEmbed) error {
	if len(embeds) == 0 {
		return nil
	}
	for _, emb := range embeds {
		c.prepareEmbed(emb)
	}
	if len(embeds) == 1 {
		_, err := c.ReplyEmbed(embeds[0])
		return err
	}

	msg, err := c.Session.ChannelMessageSendComplex(c.Message.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embeds[0]},
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
		Components: listeners.BuildPaginationComponents(0, len(embeds), c.Message.ID),
	})
	if err != nil {
		return err
	}

	session := &listeners.PaginationSession{
		MessageID:   msg.ID,
		ChannelID:   c.Message.ChannelID,
		AuthorID:    c.Message.Author.ID,
		CommandName: c.InvokedCommand,
		Embeds:      embeds,
		CurrentPage: 0,
		Created:     time.Now(),
	}

	session.Timer = time.AfterFunc(helpers.DurationPagination, func() {
		listeners.GlobalPaginationStore.Delete(msg.ID)
		_, _ = c.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    c.Message.ChannelID,
			ID:         msg.ID,
			Components: &[]discordgo.MessageComponent{},
		})
	})

	listeners.GlobalPaginationStore.Set(session)
	return nil
}

func (c *Context) SendSelfDeletingEmbed(text string, color int, delay ...time.Duration) error {
	embed := &discordgo.MessageEmbed{
		Description: text,
		Color:       color,
	}
	return c.SendSelfDeletingEmbedObject(embed, delay...)
}

func (c *Context) SendSelfDeletingEmbedObject(embed *discordgo.MessageEmbed, delay ...time.Duration) error {
	d := helpers.DurationFeedbackShort
	if len(delay) > 0 && delay[0] > 0 {
		d = delay[0]
	}
	msg, err := c.ReplyEmbed(embed)
	if err == nil && msg != nil {
		time.AfterFunc(d, func() {
			_ = c.Session.ChannelMessageDelete(msg.ChannelID, msg.ID)
		})
	}
	return err
}

type promptWaiter struct {
	authorID string
	ch       chan bool
}

var (
	promptMu      sync.RWMutex
	activePrompts = make(map[string]*promptWaiter)
)

func HandleConfirmationInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) bool {
	if i.Type != discordgo.InteractionMessageComponent || i.Message == nil {
		return false
	}

	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, "confirm_") && !strings.HasPrefix(customID, "cancel_") {
		return false
	}

	promptKey := strings.TrimPrefix(strings.TrimPrefix(customID, "confirm_"), "cancel_")

	promptMu.RLock()
	waiter, exists := activePrompts[promptKey]
	if !exists {
		waiter, exists = activePrompts[i.Message.ID]
	}
	promptMu.RUnlock()
	if !exists {
		return false
	}

	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	if userID != waiter.authorID {
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "Only the command author can respond to this confirmation.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		}); err != nil {
			Debugf("[PROMPT] Failed to respond to non-author interaction: %v", err)
		}
		return true
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	}); err != nil {
		Debugf("[PROMPT] Failed to defer confirmation interaction: %v", err)
	}

	if strings.HasPrefix(customID, "confirm_") {
		select {
		case waiter.ch <- true:
		default:
		}
	} else {
		select {
		case waiter.ch <- false:
		default:
		}
	}
	return true
}

func (c *Context) PromptConfirmation(promptText string) (bool, error) {
	expireTime := time.Now().Add(30 * time.Second).Unix()
	fullPrompt := fmt.Sprintf("%s\n\nThis request will expire <t:%d:R>.", promptText, expireTime)

	embed := &discordgo.MessageEmbed{
		Description: fullPrompt,
	}

	authorID := ""
	channelID := ""
	if c.Message != nil {
		if c.Message.Author != nil {
			authorID = c.Message.Author.ID
		}
		channelID = c.Message.ChannelID
	}
	if authorID == "" || channelID == "" {
		return false, fmt.Errorf("cannot prompt confirmation without author or channel context")
	}

	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		binary.LittleEndian.PutUint64(nonce, uint64(time.Now().UnixNano()))
	}
	promptKey := hex.EncodeToString(nonce)

	confirmCustomID := "confirm_" + promptKey
	cancelCustomID := "cancel_" + promptKey

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Confirm",
					Style:    discordgo.DangerButton,
					CustomID: confirmCustomID,
				},
				discordgo.Button{
					Label:    "Cancel",
					Style:    discordgo.SecondaryButton,
					CustomID: cancelCustomID,
				},
			},
		},
	}

	waiter := &promptWaiter{
		authorID: authorID,
		ch:       make(chan bool, 1),
	}

	promptMu.Lock()
	activePrompts[promptKey] = waiter
	promptMu.Unlock()

	defer func() {
		promptMu.Lock()
		delete(activePrompts, promptKey)
		promptMu.Unlock()
	}()

	promptMsg, err := c.Session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	if err != nil {
		return false, err
	}

	promptTimer := time.NewTimer(30 * time.Second)
	defer promptTimer.Stop()

	var confirmed bool
	select {
	case confirmed = <-waiter.ch:
	case <-c.Context().Done():
		confirmed = false
	case <-promptTimer.C:
		confirmed = false
	}

	if promptMsg != nil {
		_ = c.Session.ChannelMessageDelete(channelID, promptMsg.ID)
	}

	return confirmed, nil
}
