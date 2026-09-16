package setup

import (
	"fmt"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/policy"

	"github.com/bwmarrin/discordgo"
)

var defaultFooter = &discordgo.MessageEmbedFooter{
	Text: "You have 45 seconds per step. Type 'skip' to skip.",
}

var (
	activeSetupSessions   = make(map[string]string)
	activeSetupSessionsMu sync.Mutex
)

func boolLabel(value bool) string {
	return helpers.FormatEnabled(value)
}

func AcquireSetupLock(guildID, userID string) (bool, string) {
	activeSetupSessionsMu.Lock()
	defer activeSetupSessionsMu.Unlock()

	if activeUser, exists := activeSetupSessions[guildID]; exists {
		return false, activeUser
	}
	activeSetupSessions[guildID] = userID
	return true, ""
}

func ReleaseSetupLock(guildID string) {
	activeSetupSessionsMu.Lock()
	defer activeSetupSessionsMu.Unlock()
	delete(activeSetupSessions, guildID)
}

func editPromptNoButtons(s *discordgo.Session, channelID, msgID string, embed *discordgo.MessageEmbed) {
	_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    channelID,
		ID:         msgID,
		Embeds:     &[]*discordgo.MessageEmbed{embed},
		Components: &[]discordgo.MessageComponent{},
	})
}

func awaitMessage(ctx *bot.Context, timeout time.Duration) (*discordgo.Message, error) {
	msgChan := make(chan *discordgo.Message, 1)
	channelID := ctx.Message.ChannelID
	authorID := ctx.Message.Author.ID

	removeHandler := ctx.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.ChannelID != channelID || m.Author == nil || m.Author.ID != authorID {
			return
		}
		var memberRoles []string
		if m.Member != nil {
			memberRoles = m.Member.Roles
		}
		res := ctx.Policy.CanExecute(policy.ExecutionRequest{
			GuildID:       m.GuildID,
			ChannelID:     m.ChannelID,
			UserID:        m.Author.ID,
			MemberRoleIDs: memberRoles,
		})
		if !res.Allowed() {
			return
		}
		select {
		case msgChan <- m.Message:
		default:
		}
	})
	defer removeHandler()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case msg := <-msgChan:
		return msg, nil
	case <-ctx.Context().Done():
		return nil, ctx.Context().Err()
	case <-timer.C:
		return nil, fmt.Errorf("timeout")
	}
}

func awaitComponentInteraction(ctx *bot.Context, timeout time.Duration, selectID string, buttonIDs ...string) (customID string, values []string, timedOut bool) {
	interChan := make(chan string, 1)
	selectValsChan := make(chan []string, 1)
	channelID := ctx.Message.ChannelID
	authorID := ctx.Message.Author.ID

	removeHandler := ctx.Session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionMessageComponent || i.ChannelID != channelID {
			return
		}

		var userID string
		var memberRoles []string
		if i.Member != nil {
			memberRoles = i.Member.Roles
			if i.Member.User != nil {
				userID = i.Member.User.ID
			}
		}
		if userID == "" && i.User != nil {
			userID = i.User.ID
		}

		if userID != authorID {
			return
		}

		res := ctx.Policy.CanExecute(policy.ExecutionRequest{
			GuildID:       i.GuildID,
			ChannelID:     i.ChannelID,
			UserID:        userID,
			MemberRoleIDs: memberRoles,
		})
		if !res.Allowed() {
			return
		}

		cID := i.MessageComponentData().CustomID
		matched := false
		if selectID != "" && cID == selectID {
			matched = true
		} else {
			for _, bID := range buttonIDs {
				if cID == bID {
					matched = true
					break
				}
			}
		}

		if !matched {
			return
		}

		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})

		if selectID != "" && cID == selectID {
			select {
			case selectValsChan <- i.MessageComponentData().Values:
			default:
			}
		} else {
			select {
			case interChan <- cID:
			default:
			}
		}
	})
	defer removeHandler()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case btnID := <-interChan:
		return btnID, nil, false
	case vals := <-selectValsChan:
		return "", vals, false
	case <-ctx.Context().Done():
		return "", nil, true
	case <-timer.C:
		return "", nil, true
	}
}

func awaitInteraction(ctx *bot.Context, timeout time.Duration, validIDs ...string) (string, error) {
	btnID, _, timedOut := awaitComponentInteraction(ctx, timeout, "", validIDs...)
	if timedOut {
		return "", fmt.Errorf("timeout")
	}
	return btnID, nil
}

func awaitMessageOrButton(ctx *bot.Context, btnCustomID string, timeout time.Duration) (string, error) {
	resChan := make(chan string, 1)
	channelID := ctx.Message.ChannelID
	authorID := ctx.Message.Author.ID

	rmBtn := ctx.Session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionMessageComponent || i.ChannelID != channelID {
			return
		}
		var userID string
		var memberRoles []string
		if i.Member != nil {
			memberRoles = i.Member.Roles
			if i.Member.User != nil {
				userID = i.Member.User.ID
			}
		}
		if userID == "" && i.User != nil {
			userID = i.User.ID
		}

		if userID != authorID {
			return
		}

		res := ctx.Policy.CanExecute(policy.ExecutionRequest{
			GuildID:       i.GuildID,
			ChannelID:     i.ChannelID,
			UserID:        userID,
			MemberRoleIDs: memberRoles,
		})
		if !res.Allowed() {
			return
		}

		if i.MessageComponentData().CustomID == btnCustomID {
			select {
			case resChan <- "auto":
				_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseDeferredMessageUpdate,
				})
			default:
			}
		}
	})

	rmMsg := ctx.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.ChannelID != channelID || m.Author == nil || m.Author.ID != authorID {
			return
		}
		var memberRoles []string
		if m.Member != nil {
			memberRoles = m.Member.Roles
		}
		res := ctx.Policy.CanExecute(policy.ExecutionRequest{
			GuildID:       m.GuildID,
			ChannelID:     m.ChannelID,
			UserID:        m.Author.ID,
			MemberRoleIDs: memberRoles,
		})
		if !res.Allowed() {
			return
		}
		select {
		case resChan <- m.Content:
			_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
		default:
		}
	})

	defer func() {
		rmBtn()
		rmMsg()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case res := <-resChan:
		return res, nil
	case <-ctx.Context().Done():
		return "", ctx.Context().Err()
	case <-timer.C:
		return "", fmt.Errorf("timeout")
	}
}

func autoCreateTextChannel(s *discordgo.Session, guildID, name, description string) (*discordgo.Channel, error) {
	return s.GuildChannelCreateComplex(guildID, discordgo.GuildChannelCreateData{
		Name:  name,
		Type:  discordgo.ChannelTypeGuildText,
		Topic: description,
	})
}

func autoCreateRole(s *discordgo.Session, guildID, name string, color int) (*discordgo.Role, error) {
	return s.GuildRoleCreate(guildID, &discordgo.RoleParams{
		Name:  name,
		Color: &color,
	})
}
