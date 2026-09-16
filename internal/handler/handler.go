package handler

import (
	"gobot/internal/bot"
	"gobot/internal/commands"
)

type Handler struct {
	bot      *bot.Bot
	registry *commands.Registry
	routes   []interactionRoute
}

func New(b *bot.Bot, registry *commands.Registry) *Handler {
	return &Handler{
		bot:      b,
		registry: registry,
		routes:   newInteractionRoutes(b),
	}
}
