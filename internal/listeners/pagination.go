package listeners

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gobot/internal/helpers"
	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

type PaginationSession struct {
	mu          sync.Mutex
	MessageID   string
	ChannelID   string
	AuthorID    string
	CommandName string
	Embeds      []*discordgo.MessageEmbed
	CurrentPage int
	Created     time.Time
	Timer       *time.Timer
	gen         uint64
}

type PaginationStore struct {
	mu       sync.RWMutex
	sessions map[string]*PaginationSession
	stopChan chan struct{}
	running  bool
	wg       sync.WaitGroup
}

var GlobalPaginationStore = NewPaginationStore()

func NewPaginationStore() *PaginationStore {
	return &PaginationStore{
		sessions: make(map[string]*PaginationSession),
		stopChan: make(chan struct{}),
	}
}

func (s *PaginationStore) Set(session *PaginationSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.MessageID] = session
}

func (s *PaginationStore) Get(msgID string) *PaginationSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[msgID]
}

func (s *PaginationStore) Delete(msgID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[msgID]; ok {
		if session.Timer != nil {
			session.Timer.Stop()
		}
		delete(s.sessions, msgID)
	}
}

func (s *PaginationStore) Start(ctx context.Context) {
	s.StartCleanupRoutine(ctx, 5*time.Minute, 15*time.Minute)
}

func (s *PaginationStore) StartCleanupRoutine(ctx context.Context, interval, maxAge time.Duration) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopChan = make(chan struct{})
	stopCh := s.stopChan
	s.mu.Unlock()

	s.wg.Add(1)
	helpers.Spawn(func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.Lock()
				now := time.Now()
				for id, session := range s.sessions {
					if now.Sub(session.Created) > maxAge {
						if session.Timer != nil {
							session.Timer.Stop()
						}
						delete(s.sessions, id)
					}
				}
				s.mu.Unlock()
			}
		}
	})
}

func (s *PaginationStore) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopChan)
	s.mu.Unlock()

	s.wg.Wait()
}

func BuildPaginationComponents(currentPage, totalPages int, msgID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Label: "<", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("page_prev_%s", msgID)},
				discordgo.Button{Label: fmt.Sprintf("Page %d/%d", currentPage+1, totalPages), Style: discordgo.SecondaryButton, CustomID: fmt.Sprintf("page_input_%s", msgID)},
				discordgo.Button{Label: ">", Style: discordgo.PrimaryButton, CustomID: fmt.Sprintf("page_next_%s", msgID)},
				discordgo.Button{Label: "x", Style: discordgo.DangerButton, CustomID: fmt.Sprintf("page_close_%s", msgID)},
			},
		},
	}
}

func resetSessionTimer(s *discordgo.Session, session *PaginationSession) {
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.Timer != nil {
		session.Timer.Stop()
	}
	session.gen++
	capturedGen := session.gen
	msgID := session.MessageID
	channelID := session.ChannelID
	session.Timer = time.AfterFunc(helpers.DurationPagination, func() {
		session.mu.Lock()
		if session.gen != capturedGen {
			session.mu.Unlock()
			return
		}
		session.mu.Unlock()
		GlobalPaginationStore.Delete(msgID)
		_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    channelID,
			ID:         msgID,
			Components: &[]discordgo.MessageComponent{},
		})
	})
}

func OnPaginationInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	session, ok := validatePaginationInteraction(s, i)
	if !ok {
		return
	}

	if i.Type == discordgo.InteractionModalSubmit {
		handlePaginationModalSubmit(s, i, session)
		return
	}

	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	customID := i.MessageComponentData().CustomID
	if strings.HasPrefix(customID, "page_input_") {
		promptPaginationJumpModal(s, i, session)
		return
	}

	if strings.HasPrefix(customID, "page_close_") {
		GlobalPaginationStore.Delete(session.MessageID)
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		}); err != nil {
			logger.Debugf("[PAGINATION] Failed to defer close interaction: %v", err)
		}
		_ = s.ChannelMessageDelete(session.ChannelID, session.MessageID)
		return
	}

	handlePaginationPageTurn(s, i, session, customID)
}

func validatePaginationInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) (*PaginationSession, bool) {
	if i == nil {
		return nil, false
	}

	var msgID string
	if i.Message != nil {
		msgID = i.Message.ID
	}
	if msgID == "" {
		return nil, false
	}

	session := GlobalPaginationStore.Get(msgID)
	if session == nil {
		helpers.RespondEphemeral(s, i, "This session has expired.")
		return nil, false
	}

	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	if userID != session.AuthorID {
		helpers.RespondEphemeral(s, i, "Only the command author can use these navigation controls.")
		return nil, false
	}
	return session, true
}

func handlePaginationModalSubmit(s *discordgo.Session, i *discordgo.InteractionCreate, session *PaginationSession) {
	data := i.ModalSubmitData()
	if !strings.HasPrefix(data.CustomID, "page_modal_") {
		return
	}

	var newPage int
	hasNewPage := false
	for _, rowComp := range data.Components {
		if row, ok := rowComp.(*discordgo.ActionsRow); ok {
			for _, inputComp := range row.Components {
				if input, ok := inputComp.(*discordgo.TextInput); ok && input.CustomID == "page_num_input" {
					if pageNum, err := strconv.Atoi(strings.TrimSpace(input.Value)); err == nil {
						session.mu.Lock()
						if pageNum >= 1 && pageNum <= len(session.Embeds) {
							session.CurrentPage = pageNum - 1
							newPage = session.CurrentPage
							hasNewPage = true
						}
						session.mu.Unlock()
					}
				}
			}
		}
	}

	if hasNewPage {
		session.mu.Lock()
		currentEmbed := session.Embeds[newPage]
		totalEmbeds := len(session.Embeds)
		session.mu.Unlock()

		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{currentEmbed},
				Components: BuildPaginationComponents(newPage, totalEmbeds, session.MessageID),
			},
		}); err != nil {
			logger.Debugf("[PAGINATION] Failed to update message after modal submit: %v", err)
		}
		resetSessionTimer(s, session)
	}
}

func promptPaginationJumpModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *PaginationSession) {
	session.mu.Lock()
	totalEmbeds := len(session.Embeds)
	curPage := session.CurrentPage
	session.mu.Unlock()

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: fmt.Sprintf("page_modal_%s", session.MessageID),
			Title:    "Go to Page",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "page_num_input",
							Label:       fmt.Sprintf("Enter Page Number (1-%d)", totalEmbeds),
							Style:       discordgo.TextInputShort,
							Placeholder: fmt.Sprintf("Current page: %d", curPage+1),
							Required:    helpers.Ptr(true),
							MinLength:   1,
							MaxLength:   5,
						},
					},
				},
			},
		},
	}); err != nil {
		logger.Debugf("[PAGINATION] Failed to send page input modal: %v", err)
	}
}

func handlePaginationPageTurn(s *discordgo.Session, i *discordgo.InteractionCreate, session *PaginationSession, customID string) {
	session.mu.Lock()
	switch {
	case strings.HasPrefix(customID, "page_prev_"):
		if session.CurrentPage > 0 {
			session.CurrentPage--
		} else {
			session.CurrentPage = len(session.Embeds) - 1
		}
	case strings.HasPrefix(customID, "page_next_"):
		if session.CurrentPage < len(session.Embeds)-1 {
			session.CurrentPage++
		} else {
			session.CurrentPage = 0
		}
	default:
		session.mu.Unlock()
		return
	}
	curPage := session.CurrentPage
	currentEmbed := session.Embeds[curPage]
	totalEmbeds := len(session.Embeds)
	session.mu.Unlock()

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{currentEmbed},
			Components: BuildPaginationComponents(curPage, totalEmbeds, session.MessageID),
		},
	}); err != nil {
		logger.Debugf("[PAGINATION] Failed to update message on page turn: %v", err)
	}
	resetSessionTimer(s, session)
}
