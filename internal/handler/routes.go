package handler

import (
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands/giveaway"
	"gobot/internal/commands/juicewrld"
	"gobot/internal/commands/radio"
	"gobot/internal/listeners"

	"github.com/bwmarrin/discordgo"
)

type interactionRoute struct {
	prefixes      []string
	handle        func(*discordgo.Session, *discordgo.InteractionCreate)
	canonicalName string
	canonicalFor  func(*discordgo.InteractionCreate) string
}

func newInteractionRoutes(b *bot.Bot) []interactionRoute {
	return []interactionRoute{
		{
			prefixes:      []string{"emb_"},
			canonicalName: "embed",
			handle:        listeners.OnEmbedInteraction,
		},
		{
			prefixes: []string{"page_"},
			canonicalFor: func(i *discordgo.InteractionCreate) string {
				if i == nil || i.Message == nil {
					return ""
				}
				session := listeners.GlobalPaginationStore.Get(i.Message.ID)
				if session == nil {
					return ""
				}
				return session.CommandName
			},
			handle: listeners.OnPaginationInteraction,
		},
		{
			prefixes:      []string{"giveaway_", "modal_giveaway_", "modal_gw_", "glist_"},
			canonicalName: "giveaway",
			handle: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
				giveaway.HandleGiveawayInteraction(s, i, b.DB, b.Config)
			},
		},
		{
			prefixes:      []string{"r_", "radio_"},
			canonicalName: "radio",
			handle: func(s *discordgo.Session, i *discordgo.InteractionCreate) {
				radio.OnRadioInteraction(b.Radio, s, i)
			},
		},

		{
			prefixes:     []string{"jw_", "ver_sel_"},
			canonicalFor: juicewrld.InteractionCommandName,
			handle:       juicewrld.HandleInteraction,
		},
	}
}

func (r interactionRoute) commandName(i *discordgo.InteractionCreate) string {
	if r.canonicalFor != nil {
		return r.canonicalFor(i)
	}
	return r.canonicalName
}

func interactionCustomID(i *discordgo.InteractionCreate) string {
	if i == nil {
		return ""
	}
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		return i.MessageComponentData().CustomID
	case discordgo.InteractionModalSubmit:
		return i.ModalSubmitData().CustomID
	default:
		return ""
	}
}

func (h *Handler) commandNameForInteraction(i *discordgo.InteractionCreate) string {
	customID := interactionCustomID(i)
	for _, route := range h.routes {
		if route.matches(customID) {
			return route.commandName(i)
		}
	}
	return ""
}

func (h *Handler) routeCustomIDInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	customID := interactionCustomID(i)

	if bot.HandleConfirmationInteraction(s, i) {
		return
	}

	for _, route := range h.routes {
		if route.matches(customID) {
			route.handle(s, i)
			return
		}
	}

	bot.Debugf("[INTERACTION] Unhandled component custom ID %q", customID)
}

func (r interactionRoute) matches(customID string) bool {
	for _, prefix := range r.prefixes {
		if strings.HasPrefix(customID, prefix) {
			return true
		}
	}
	return false
}
