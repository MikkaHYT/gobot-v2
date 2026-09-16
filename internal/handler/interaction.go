package handler

import (
	"fmt"
	"runtime/debug"

	"gobot/internal/bot"
	"gobot/internal/commands/giveaway"
	"gobot/internal/helpers"
	"gobot/internal/policy"

	"github.com/bwmarrin/discordgo"
)

func (h *Handler) OnInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i == nil {
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

	if userID != "" {
		if limited, remaining, shouldNotify := bot.GlobalRateLimiter.CheckLimit(i.GuildID, userID); limited {
			if shouldNotify {
				helpers.RespondEphemeral(s, i, fmt.Sprintf("You are interacting too quickly. Please slow down (cooldown: %.1fs).", remaining.Seconds()))
			} else {
				switch i.Type {
				case discordgo.InteractionMessageComponent:
					_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseDeferredMessageUpdate,
					})
				case discordgo.InteractionApplicationCommandAutocomplete:
					_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionApplicationCommandAutocompleteResult,
						Data: &discordgo.InteractionResponseData{
							Choices: []*discordgo.ApplicationCommandOptionChoice{},
						},
					})
				default:
					_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData{
							Flags: discordgo.MessageFlagsEphemeral,
						},
					})
				}
			}
			return
		}
	}

	if h.bot.Policy == nil {
		rejectInteraction(s, i)
		return
	}

	cmdName := ""
	if i.Type == discordgo.InteractionApplicationCommand || i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		cmdName = i.ApplicationCommandData().Name
	} else {
		cmdName = h.commandNameForInteraction(i)
	}
	res := h.bot.Policy.CanExecute(policy.ExecutionRequest{
		GuildID:       i.GuildID,
		ChannelID:     i.ChannelID,
		UserID:        userID,
		MemberRoleIDs: memberRoles,
		CanonicalName: cmdName,
		InvokedName:   cmdName,
	})
	if !res.Allowed() {
		rejectInteraction(s, i)
		return
	}

	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			bot.Errorf("[INTERACTION PANIC] Recovered from panic in interaction: %v\n%s", r, stack)
			bot.SendConsoleWebhook(h.bot.Config.ConsoleWebhookURL, "Interaction Panic", fmt.Sprintf("Interaction panicked: `%v`\n```\n%.1500s\n```", r, stack), helpers.ColorError)
		}
	}()

	switch i.Type {
	case discordgo.InteractionApplicationCommand, discordgo.InteractionApplicationCommandAutocomplete:
		giveaway.HandleGiveawayInteraction(s, i, h.bot.DB, h.bot.Config)
	case discordgo.InteractionMessageComponent, discordgo.InteractionModalSubmit:
		h.routeCustomIDInteraction(s, i)
	}
}

func rejectInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		helpers.RespondEphemeral(s, i, "You are restricted from using this bot.")
	case discordgo.InteractionApplicationCommandAutocomplete:
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{
				Choices: []*discordgo.ApplicationCommandOptionChoice{},
			},
		})
	case discordgo.InteractionMessageComponent, discordgo.InteractionModalSubmit:
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})
	}
}
