package juicewrld

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	jw "gobot/internal/juicewrld"

	"github.com/bwmarrin/discordgo"
)

const sessionTTL = 15 * time.Minute

type SearchSession struct {
	CommandName      string
	Songs            []jw.Song
	AuthorID         string
	StatusMsgID      string
	ArtistFilter     string
	CurrentIndex     int
	CoverPage        int
	CoverURLs        []string
	CachedComponents map[int][]map[string]interface{}
	LastInteraction  time.Time
	CreatedAt        time.Time
}

var (
	searchSessionsMu sync.RWMutex
	searchSessions   = make(map[string]*SearchSession)
)

func pruneExpiredSessionsLocked(now time.Time) {
	for id, session := range searchSessions {
		lastActive := session.CreatedAt
		if !session.LastInteraction.IsZero() {
			lastActive = session.LastInteraction
		}
		if now.Sub(lastActive) > sessionTTL {
			delete(searchSessions, id)
		}
	}
}
func PruneSessions() {
	searchSessionsMu.Lock()
	defer searchSessionsMu.Unlock()
	pruneExpiredSessionsLocked(time.Now())
}

func init() {
	bot.RegisterPeriodicWorker("juicewrld_sessions", 5*time.Minute, func(ctx context.Context) {
		PruneSessions()
	})
}

func InteractionCommandName(i *discordgo.InteractionCreate) string {
	if i == nil || i.Type != discordgo.InteractionMessageComponent || i.Message == nil {
		return ""
	}
	data := i.MessageComponentData()
	if !strings.HasPrefix(data.CustomID, "ver_sel_") && data.CustomID != "jw_cover_prev" && data.CustomID != "jw_cover_next" {
		return ""
	}

	searchSessionsMu.Lock()
	defer searchSessionsMu.Unlock()
	pruneExpiredSessionsLocked(time.Now())
	session := searchSessions[i.Message.ID]
	if session == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(session.CommandName))
}

func HandleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}
	data := i.MessageComponentData()
	if !strings.HasPrefix(data.CustomID, "ver_sel_") && data.CustomID != "jw_cover_prev" && data.CustomID != "jw_cover_next" {
		return
	}

	searchSessionsMu.Lock()
	pruneExpiredSessionsLocked(time.Now())
	session, exists := searchSessions[i.Message.ID]
	if !exists {
		searchSessionsMu.Unlock()
		helpers.RespondEphemeral(s, i, "This search session has expired. Please run the command again.")
		return
	}

	interactorID := ""
	if i.Member != nil && i.Member.User != nil {
		interactorID = i.Member.User.ID
	} else if i.User != nil {
		interactorID = i.User.ID
	}

	if interactorID == "" {
		searchSessionsMu.Unlock()
		helpers.RespondEphemeral(s, i, "Unable to verify the author of this interaction.")
		return
	}

	if interactorID != session.AuthorID {
		searchSessionsMu.Unlock()
		helpers.RespondEphemeral(s, i, "You cannot interact with another user's search session.")
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	now := time.Now()
	if !session.LastInteraction.IsZero() && now.Sub(session.LastInteraction) < 200*time.Millisecond {
		searchSessionsMu.Unlock()
		return
	}

	if data.CustomID == "jw_cover_prev" {
		if session.CoverPage > 0 {
			session.CoverPage--
		}
		newCoverPage := session.CoverPage
		cachedCovers := session.CoverURLs
		session.LastInteraction = now
		searchSessionsMu.Unlock()

		ctx := &bot.Context{
			Session: s,
			Message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: i.ChannelID,
				},
			},
		}
		_ = RenderCommandResponse(ctx, RenderOptions{
			CommandName:     session.CommandName,
			Songs:           session.Songs,
			SelectedIndex:   session.CurrentIndex,
			StatusMessage:   i.Message,
			ArtistFilter:    session.ArtistFilter,
			CoverPage:       newCoverPage,
			CachedCoverURLs: cachedCovers,
		})
		return
	}

	if data.CustomID == "jw_cover_next" {
		session.CoverPage++
		newCoverPage := session.CoverPage
		cachedCovers := session.CoverURLs
		session.LastInteraction = now
		searchSessionsMu.Unlock()

		ctx := &bot.Context{
			Session: s,
			Message: &discordgo.MessageCreate{
				Message: &discordgo.Message{
					ChannelID: i.ChannelID,
				},
			},
		}
		_ = RenderCommandResponse(ctx, RenderOptions{
			CommandName:     session.CommandName,
			Songs:           session.Songs,
			SelectedIndex:   session.CurrentIndex,
			StatusMessage:   i.Message,
			ArtistFilter:    session.ArtistFilter,
			CoverPage:       newCoverPage,
			CachedCoverURLs: cachedCovers,
		})
		return
	}

	if len(data.Values) == 0 {
		searchSessionsMu.Unlock()
		return
	}

	selectedIdx, err := strconv.Atoi(data.Values[0])
	if err != nil {
		searchSessionsMu.Unlock()
		return
	}

	if selectedIdx == session.CurrentIndex {
		searchSessionsMu.Unlock()
		return
	}

	oldIdx := session.CurrentIndex
	session.CurrentIndex = selectedIdx
	session.CoverPage = 0
	session.CoverURLs = nil
	session.LastInteraction = now
	searchSessionsMu.Unlock()

	ctx := &bot.Context{
		Session: s,
		Message: &discordgo.MessageCreate{
			Message: &discordgo.Message{
				ChannelID: i.ChannelID,
			},
		},
	}

	if err := RenderCommandResponse(ctx, RenderOptions{
		CommandName:   session.CommandName,
		Songs:         session.Songs,
		SelectedIndex: selectedIdx,
		StatusMessage: i.Message,
		ArtistFilter:  session.ArtistFilter,
	}); err != nil {
		bot.Warnf("[JUICEWRLD] Failed to render interaction response: %v", err)
		searchSessionsMu.Lock()
		session.CurrentIndex = oldIdx
		session.LastInteraction = now
		searchSessionsMu.Unlock()
		return
	}
}
