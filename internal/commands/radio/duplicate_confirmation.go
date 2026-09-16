package radio

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/policy"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

const queueConfirmationLifetime = 30 * time.Second

var queueConfirmationSequence atomic.Uint64

var (
	errQueueConfirmationUnauthorized = errors.New("only the command author can confirm this")
	errQueueConfirmationPolicyDenied = errors.New("queue confirmation is no longer allowed by policy")
)

type queueConfirmationRequest struct {
	GuildID     string
	ChannelID   string
	InitiatorID string
	Track       coordinator.Track
	Match       coordinator.DuplicateMatch
}

type confirmationPrompter func(request queueConfirmationRequest, confirm func(userID string, roleIDs []string) error) error

type policyChecker func(userID string, roleIDs []string) bool

func enqueueRadioTracksWithDuplicateConfirmation(ctx *bot.Context, tracks []coordinator.Track, placement coordinator.EnqueuePlacement) (bool, error) {
	radio, err := requireRadio(ctx)
	if err != nil {
		return false, err
	}
	if ctx == nil || ctx.Message == nil || ctx.Message.Author == nil {
		return false, fmt.Errorf("radio command author is unavailable")
	}

	canExecute := func(userID string, roleIDs []string) bool {
		if ctx.Policy == nil {
			return true
		}
		return ctx.Policy.CanExecute(policy.ExecutionRequest{
			GuildID:       ctx.Message.GuildID,
			ChannelID:     ctx.Message.ChannelID,
			UserID:        userID,
			MemberRoleIDs: roleIDs,
		}).Allowed()
	}

	prompt := func(req queueConfirmationRequest, confirm func(userID string, roleIDs []string) error) error {
		return promptDiscordQueueConfirmation(ctx, req, confirm)
	}

	for i := range tracks {
		if tracks[i].RequesterID == "" && ctx.Message != nil && ctx.Message.Author != nil {
			tracks[i].RequesterID = ctx.Message.Author.ID
		}
	}

	return enqueueTracksWithDuplicateConfirmation(
		ctx.Context(),
		radio,
		canExecute,
		prompt,
		coordinator.EnqueueRequest{
			GuildID:       ctx.Message.GuildID,
			TextChannelID: ctx.Message.ChannelID,
			Tracks:        tracks,
			Placement:     placement,
		},
		ctx.Message.Author.ID,
	)
}

func enqueueTracksWithDuplicateConfirmation(
	ctx context.Context,
	radio *coordinator.Module,
	canExecute policyChecker,
	prompt confirmationPrompter,
	request coordinator.EnqueueRequest,
	initiatorID string,
) (bool, error) {
	if radio == nil {
		return false, fmt.Errorf("radio module is unavailable")
	}
	for _, track := range request.Tracks {
		match := radio.Duplicate(request.GuildID, track)
		if !match.Found {
			continue
		}

		if prompt == nil {
			return false, fmt.Errorf("duplicate queue confirmation is unavailable")
		}

		confirmation := queueConfirmationRequest{
			GuildID:     request.GuildID,
			ChannelID:   request.TextChannelID,
			InitiatorID: initiatorID,
			Track:       track,
			Match:       match,
		}
		if err := prompt(confirmation, func(userID string, roleIDs []string) error {
			if userID != initiatorID {
				return errQueueConfirmationUnauthorized
			}
			if canExecute != nil && !canExecute(userID, roleIDs) {
				return errQueueConfirmationPolicyDenied
			}
			_, err := radio.Enqueue(ctx, request)
			return err
		}); err != nil {
			return false, err
		}
		return true, nil
	}

	_, err := radio.Enqueue(ctx, request)
	return false, err
}

func duplicateConfirmationMessage(track coordinator.Track, match coordinator.DuplicateMatch) string {
	title := strings.TrimSpace(track.Title)
	if match.Current {
		return fmt.Sprintf("**%s** is already playing. Queue anyway?", title)
	}
	return fmt.Sprintf("**%s** is already #%d in queue. Queue anyway?", title, match.QueuePosition)
}

func queueEnqueueErrorMessage(action string, err error) string {
	var limitErr *coordinator.QueueLimitError
	if errors.As(err, &limitErr) {
		limit := limitErr.Limit
		if limit <= 0 {
			limit = coordinator.MaxQueuedTracks
		}
		return fmt.Sprintf("The queue can hold at most %d pending tracks. Please wait for a track to finish before adding more.", limit)
	}
	return fmt.Sprintf("%s: %v", action, err)
}

type queueConfirmationCleanup struct {
	mu            sync.Mutex
	settled       bool
	stopTimer     func()
	removeHandler func()
}

func (c *queueConfirmationCleanup) finish(action func()) bool {
	c.mu.Lock()
	if c.settled {
		c.mu.Unlock()
		return false
	}
	c.settled = true
	stopTimer := c.stopTimer
	removeHandler := c.removeHandler
	c.stopTimer = nil
	c.removeHandler = nil
	c.mu.Unlock()

	if stopTimer != nil {
		stopTimer()
	}
	if removeHandler != nil {
		removeHandler()
	}
	if action != nil {
		action()
	}
	return true
}

func (c *queueConfirmationCleanup) setTimer(stopTimer func()) {
	c.mu.Lock()
	if c.settled {
		c.mu.Unlock()
		if stopTimer != nil {
			stopTimer()
		}
		return
	}
	c.stopTimer = stopTimer
	c.mu.Unlock()
}

func (c *queueConfirmationCleanup) setRemoveHandler(removeHandler func()) {
	c.mu.Lock()
	if c.settled {
		c.mu.Unlock()
		if removeHandler != nil {
			removeHandler()
		}
		return
	}
	c.removeHandler = removeHandler
	c.mu.Unlock()
}

func promptDiscordQueueConfirmation(ctx *bot.Context, request queueConfirmationRequest, confirm func(userID string, roleIDs []string) error) error {
	if ctx == nil || ctx.Session == nil || ctx.Message == nil {
		return fmt.Errorf("discord session context is unavailable")
	}
	sequence := queueConfirmationSequence.Add(1)
	confirmID := fmt.Sprintf("radio_queue_anyway_confirm_%s_%d", request.InitiatorID, sequence)
	cancelID := fmt.Sprintf("radio_queue_anyway_cancel_%s_%d", request.InitiatorID, sequence)

	expiresAt := time.Now().Add(queueConfirmationLifetime).Unix()
	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("%s\n\nThis confirmation expires <t:%d:R>.", duplicateConfirmationMessage(request.Track, request.Match), expiresAt),
		Color:       helpers.ColorPending,
	}
	message, err := ctx.Session.ChannelMessageSendComplex(request.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "Queue anyway", Style: discordgo.PrimaryButton, CustomID: confirmID},
				discordgo.Button{Label: "Cancel", Style: discordgo.SecondaryButton, CustomID: cancelID},
			}},
		},
	})
	if err != nil {
		return err
	}
	if message == nil {
		return fmt.Errorf("queue confirmation message was not created")
	}

	cleanup := &queueConfirmationCleanup{}

	removeHandler := ctx.Session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i == nil || i.Type != discordgo.InteractionMessageComponent || i.Message == nil || i.Message.ID != message.ID {
			return
		}
		data := i.MessageComponentData()
		if data.CustomID != confirmID && data.CustomID != cancelID {
			return
		}

		userID, roleIDs := interactionUser(i)
		if userID != request.InitiatorID {
			helpers.RespondEphemeral(s, i, "Only the requester can confirm this queue action.")
			return
		}

		if data.CustomID == cancelID {
			if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredMessageUpdate}); err != nil {
				return
			}
			cleanup.finish(func() { _ = s.ChannelMessageDelete(message.ChannelID, message.ID) })
			return
		}

		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: discordgo.InteractionResponseDeferredMessageUpdate}); err != nil {
			return
		}
		cleanup.finish(func() {
			enqueueErr := confirm(userID, roleIDs)
			description := fmt.Sprintf("Queued **%s** anyway.", request.Track.Title)
			color := helpers.ColorSuccess
			if enqueueErr != nil {
				description = queueEnqueueErrorMessage("Failed to queue track", enqueueErr)
				color = helpers.ColorDarkRed
			}
			_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Embeds:     &[]*discordgo.MessageEmbed{{Description: description, Color: color}},
				Components: &[]discordgo.MessageComponent{},
			})
		})
	})
	cleanup.setRemoveHandler(removeHandler)

	timer := time.AfterFunc(queueConfirmationLifetime, func() {
		cleanup.finish(func() { _ = ctx.Session.ChannelMessageDelete(message.ChannelID, message.ID) })
	})
	cleanup.setTimer(func() { timer.Stop() })
	return nil
}

func interactionUser(i *discordgo.InteractionCreate) (string, []string) {
	if i == nil {
		return "", nil
	}
	if i.Member != nil {
		if i.Member.User != nil {
			return i.Member.User.ID, i.Member.Roles
		}
		if i.User != nil {
			return i.User.ID, i.Member.Roles
		}
		return "", i.Member.Roles
	}
	if i.User != nil {
		return i.User.ID, nil
	}
	return "", nil
}
