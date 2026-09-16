package radio

import (
	"fmt"
	"strconv"
	"strings"

	"gobot/internal/helpers"
	"gobot/internal/logger"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

func OnChannelDeleteHandler(m *coordinator.Module) func(*discordgo.Session, *discordgo.ChannelDelete) {
	return func(s *discordgo.Session, c *discordgo.ChannelDelete) {
		if c == nil || c.Channel == nil || c.GuildID == "" || m == nil {
			return
		}
		m.HandleChannelDeleted(m.LifecycleContext(), coordinator.ChannelDeleted{
			GuildID:   c.GuildID,
			ChannelID: c.ID,
		})
	}
}

func OnVoiceStateUpdateHandler(m *coordinator.Module) func(*discordgo.Session, *discordgo.VoiceStateUpdate) {
	return func(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
		if s == nil || v == nil || v.VoiceState == nil || v.GuildID == "" || m == nil {
			return
		}
		botUserID, voiceStates, haveGuild := helpers.SnapshotGuildVoiceState(s, v.GuildID)

		botChannelID := ""
		if v.UserID == botUserID {
			botChannelID = v.ChannelID
		} else if snap, ok := m.Snapshot(v.GuildID); ok && snap.VoiceChannelID != "" {
			botChannelID = snap.VoiceChannelID
		} else if botUserID != "" {
			for _, vs := range voiceStates {
				if vs.UserID == botUserID {
					botChannelID = vs.ChannelID
					break
				}
			}
		}

		humanCount := 0
		humanCountSet := false
		if botChannelID != "" && haveGuild {
			seen := make(map[string]struct{})
			for _, vs := range voiceStates {
				if vs.UserID == botUserID {
					continue
				}
				seen[vs.UserID] = struct{}{}
				ch := vs.ChannelID
				if vs.UserID == v.UserID {
					ch = v.ChannelID
				}
				if ch == botChannelID {
					if vs.Bot {
						continue
					}
					humanCount++
				}
			}
			if _, ok := seen[v.UserID]; !ok && v.UserID != botUserID && v.ChannelID == botChannelID {
				if !isVoiceStateBot(s, v.GuildID, v.UserID) {
					humanCount++
				}
			}
			humanCountSet = true
		}

		m.HandleVoiceState(m.LifecycleContext(), coordinator.VoiceStateEvent{
			GuildID:       v.GuildID,
			ChannelID:     v.ChannelID,
			UserID:        v.UserID,
			BotUserID:     botUserID,
			HumanCount:    humanCount,
			HumanCountSet: humanCountSet,
		})
	}
}

func isVoiceStateBot(s *discordgo.Session, guildID, userID string) bool {
	if s == nil || s.State == nil {
		return false
	}
	if member, err := s.State.Member(guildID, userID); err == nil && member != nil {
		s.State.RLock()
		defer s.State.RUnlock()
		return member.User != nil && member.User.Bot
	}
	return false
}

func CheckInteractionAccess(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) (bool, string) {
	var userID string
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	}
	if userID == "" && i.User != nil {
		userID = i.User.ID
	}

	if userID == "" {
		return false, "Unable to verify user permissions."
	}

	mode := "vc"
	botChannelID := ""
	if m != nil {
		if snap, ok := m.Snapshot(i.GuildID); ok {
			mode = snap.RestrictionMode
			botChannelID = snap.VoiceChannelID
		}
	}

	guildID := i.GuildID

	isGuildOwner, _ := helpers.IsGuildOwner(s, guildID, userID)

	perms, err := s.UserChannelPermissions(userID, i.ChannelID)
	isAdmin := err == nil && perms&discordgo.PermissionAdministrator != 0
	canManageServer := err == nil && perms&discordgo.PermissionManageGuild != 0
	canManageMessages := err == nil && perms&discordgo.PermissionManageMessages != 0

	if !isAdmin && i.Member != nil && i.Member.Roles != nil {
		if guild, _ := helpers.GetGuild(s, guildID); guild != nil {
			for _, role := range guild.Roles {
				for _, rID := range i.Member.Roles {
					if role.ID == rID {
						if role.Permissions&discordgo.PermissionAdministrator != 0 {
							isAdmin = true
						}
						if role.Permissions&discordgo.PermissionManageGuild != 0 {
							canManageServer = true
						}
						if role.Permissions&discordgo.PermissionManageMessages != 0 {
							canManageMessages = true
						}
					}
				}
			}
		}
	}

	isPrivileged := isAdmin || canManageServer || isGuildOwner

	voiceState, errVS := GetUserVoiceState(s, guildID, userID)
	if voiceState == nil || voiceState.ChannelID == "" || errVS != nil {
		if !isPrivileged {
			return false, "You must be connected to a voice channel to use radio controls."
		}
	}

	if botChannelID != "" && voiceState != nil && voiceState.ChannelID != "" && voiceState.ChannelID != botChannelID {
		if !isPrivileged {
			return false, fmt.Sprintf("You must be connected to <#%s> to use radio controls.", botChannelID)
		}
	}

	if isPrivileged || canManageMessages {
		return true, ""
	}

	if mode == "mods" {
		return false, "Radio control is set to **Mods Only**."
	}

	return true, ""
}

func OnRadioInteraction(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent || i.GuildID == "" {
		return
	}

	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, "r_") {
		return
	}

	allowed, denyReason := CheckInteractionAccess(m, s, i)
	if !allowed {
		helpers.RespondEphemeral(s, i, denyReason)
		return
	}

	username := "User"
	if i.Member != nil && i.Member.User != nil {
		username = i.Member.User.Username
	} else if i.User != nil {
		username = i.User.Username
	}

	if strings.HasPrefix(customID, "r_lyr_") {
		handleRadioLyricsPage(m, s, i)
		return
	}

	switch customID {
	case "r_prev", "r_previous":
		handleRadioPrevious(m, s, i)
	case "r_pause", "r_pause_resume":
		handleRadioPauseResume(m, s, i)
	case "r_loop":
		handleRadioLoop(m, s, i)
	case "r_skip":
		handleRadioSkip(m, s, i, username)
	case "r_download":
		handleRadioDownload(m, s, i)
	case "r_stop":
		handleRadioStop(m, s, i)
	case "r_player_options":
		handleRadioPlayerOptions(m, s, i)
	case "r_q_back":
		handleRadioQueueBack(m, s, i)
	case "r_q_prev":
		handleRadioQueuePrev(m, s, i)
	case "r_q_next":
		handleRadioQueueNext(m, s, i)
	case "r_q_shuffle":
		handleRadioQueueShuffle(m, s, i)
	case "r_q_clear":
		handleRadioQueueClear(m, s, i)
	}
}

func handleRadioDownload(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	if m == nil {
		helpers.RespondEphemeral(s, i, "Radio module is unavailable.")
		return
	}
	snap, ok := m.Snapshot(i.GuildID)
	if !ok || snap.Current == nil {
		helpers.RespondEphemeral(s, i, "No track currently playing.")
		return
	}

	dlURL := ""
	btnLabel := "Download Audio"
	isStream := snap.Current.Category == "stream" || strings.Contains(strings.ToLower(snap.Current.URL), "googlevideo.com")

	if isStream {
		if snap.Current.WebpageURL != "" && len(snap.Current.WebpageURL) <= 512 && helpers.IsValidHTTPURL(snap.Current.WebpageURL) {
			dlURL = snap.Current.WebpageURL
			btnLabel = "Open Source / Video"
		}
	}

	if dlURL == "" {
		if snap.Current.URL != "" && len(snap.Current.URL) <= 512 && helpers.IsValidHTTPURL(snap.Current.URL) && !strings.Contains(strings.ToLower(snap.Current.URL), "googlevideo.com") {
			dlURL = snap.Current.URL
			btnLabel = "Download Audio"
		} else if snap.Current.WebpageURL != "" && len(snap.Current.WebpageURL) <= 512 && helpers.IsValidHTTPURL(snap.Current.WebpageURL) {
			dlURL = snap.Current.WebpageURL
			if isStream {
				btnLabel = "Open Source / Video"
			} else {
				btnLabel = "Download Audio"
			}
		}
	}

	if dlURL == "" {
		helpers.RespondEphemeral(s, i, "No direct download or source link is available for this track.")
		return
	}

	thumb := snap.Current.Thumbnail
	if thumb == "" || !helpers.IsValidHTTPURL(thumb) {
		thumb = helpers.DefaultRadioCoverURL
	}

	eraText := snap.Current.Era
	if eraText != "" {
		eraText = eraText + " · "
	}

	subtext := fmt.Sprintf("-# %sDirect Audio Download", eraText)
	if isStream {
		subtext = fmt.Sprintf("-# %sExternal Stream Link", eraText)
	}

	card := []map[string]interface{}{
		helpers.SectionWithAccessory(
			helpers.MediaAccessory(thumb),
			helpers.TextDisplay(fmt.Sprintf("## %s\n%s", snap.Current.Title, subtext)),
		),
		helpers.ActionRow(
			helpers.LinkButton(btnLabel, dlURL),
		),
	}
	if err := respondEphemeralCV2(s, i, card); err != nil {
		logger.Warnf("[RADIO DOWNLOAD] Failed to send ephemeral CV2 download card: %v, falling back to text", err)
		helpers.RespondEphemeral(s, i, fmt.Sprintf("**%s**\n%s: %s", snap.Current.Title, btnLabel, dlURL))
	}
}

func handleRadioPrevious(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	var errPrev error
	if m == nil {
		errPrev = fmt.Errorf("radio module is unavailable")
	} else {
		_, errPrev = m.Control(m.LifecycleContext(), coordinator.ControlRequest{
			GuildID: i.GuildID,
			Action:  coordinator.ControlPrevious,
		})
	}
	if errPrev != nil {
		helpers.RespondEphemeral(s, i, "No previous track found in history.")
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
}

func handleRadioPauseResume(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	var errPR error
	if m == nil {
		errPR = fmt.Errorf("radio module is unavailable")
	} else {
		if curSnap, ok := m.Snapshot(i.GuildID); ok && curSnap.Phase == coordinator.PhasePaused {
			_, errPR = m.Control(m.LifecycleContext(), coordinator.ControlRequest{
				GuildID: i.GuildID,
				Action:  coordinator.ControlResume,
			})
		} else {
			_, errPR = m.Control(m.LifecycleContext(), coordinator.ControlRequest{
				GuildID: i.GuildID,
				Action:  coordinator.ControlPause,
			})
		}
	}
	if errPR != nil {
		helpers.RespondEphemeral(s, i, "Radio is not currently active.")
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
}

func handleRadioLoop(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	var errLoop error
	if m == nil {
		errLoop = fmt.Errorf("radio module is unavailable")
	} else {
		_, errLoop = m.Control(m.LifecycleContext(), coordinator.ControlRequest{
			GuildID: i.GuildID,
			Action:  coordinator.ControlLoop,
		})
	}
	if errLoop != nil {
		helpers.RespondEphemeral(s, i, "Radio is not currently active.")
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
}

func handleRadioSkip(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate, username string) {
	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	skipped, msg, err := ProcessVoteSkip(m, s, i.GuildID, i.ChannelID, userID, username)
	if err != nil {
		helpers.RespondEphemeral(s, i, msg)
		return
	}

	if skipped {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})
	} else {
		helpers.RespondEphemeral(s, i, msg)
	}
}

func handleRadioStop(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	var errStop error
	if m == nil {
		errStop = fmt.Errorf("radio module is unavailable")
	} else {
		_, errStop = m.Control(m.LifecycleContext(), coordinator.ControlRequest{
			GuildID: i.GuildID,
			Action:  coordinator.ControlStop,
		})
	}
	if errStop != nil {
		helpers.RespondEphemeral(s, i, "Radio is not currently connected to voice.")
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
}

func handleRadioPlayerOptions(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	values := i.MessageComponentData().Values
	if len(values) == 0 {
		return
	}
	opt := values[0]

	switch opt {
	case "opt_queue":
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseDeferredMessageUpdate,
		})
		if m != nil {
			_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, coordinator.ControllerViewQueue, 1)
		}

	case "opt_info":
		if m == nil {
			helpers.RespondEphemeral(s, i, "Radio module is unavailable.")
			return
		}
		snap, ok := m.Snapshot(i.GuildID)
		if !ok || snap.Current == nil {
			helpers.RespondEphemeral(s, i, "No track currently playing.")
			return
		}

		card := BuildCurrentSongInfoCard(m.LifecycleContext(), snap.Current)
		if len(card) == 0 {
			helpers.RespondEphemeral(s, i, "Track metadata is currently unavailable.")
		} else {
			_ = respondEphemeralCV2(s, i, card)
		}
		go func() {
			_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, snap.ControllerView, snap.QueuePage)
		}()

	case "opt_lyrics":
		if m == nil {
			helpers.RespondEphemeral(s, i, "Radio module is unavailable.")
			return
		}
		snap, ok := m.Snapshot(i.GuildID)
		if !ok || snap.Current == nil {
			helpers.RespondEphemeral(s, i, "No track currently playing.")
			return
		}

		song, pages, errLyrics := FetchTrackLyrics(m.LifecycleContext(), snap.Current)
		if errLyrics != nil {
			helpers.RespondEphemeral(s, i, errLyrics.Error())
			go func() {
				_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, snap.ControllerView, snap.QueuePage)
			}()
			return
		}

		card := BuildLyricsCV2Card(song, pages, 1)
		_ = respondEphemeralCV2(s, i, card)
		go func() {
			_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, snap.ControllerView, snap.QueuePage)
		}()

	case "opt_mode":
		if m == nil {
			helpers.RespondEphemeral(s, i, "Radio module is unavailable.")
			return
		}
		snap, _ := m.Snapshot(i.GuildID)
		curr := snap.Mode
		if curr == "" {
			curr = "all"
		}
		p := GetGuildOrBotPrefix(i.GuildID)
		msg := fmt.Sprintf("Current radio mode: **%s**\n\n**To change mode, run:** `%sr mode <name>`\n• **Eras**: `gbgr`, `wod`, `drfl`, `jw3`, `post`, `pre-gbgr`\n• **Categories**: `unreleased`, `released`, `sessions`\n• **Reset**: `all`", curr, p)
		helpers.RespondEphemeral(s, i, msg)
		go func() {
			_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, snap.ControllerView, snap.QueuePage)
		}()

	case "opt_commands":
		p := GetGuildOrBotPrefix(i.GuildID)
		card := BuildRadioCommandsCV2Card(p)
		if err := respondEphemeralCV2(s, i, card); err != nil {
			logger.Warnf("[RADIO HELP] Failed to send ephemeral CV2 commands card: %v, falling back to text", err)
			helpers.RespondEphemeral(s, i, BuildRadioCommandsFallbackText(p))
		}
		if m != nil {
			snap, _ := m.Snapshot(i.GuildID)
			go func() {
				_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, snap.ControllerView, snap.QueuePage)
			}()
		}

	case "opt_stop":
		handleRadioStop(m, s, i)
	}
}

func handleRadioQueueBack(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if m != nil {
		_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, coordinator.ControllerViewPlayer, 1)
	}
}

func handleRadioQueuePrev(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if m == nil {
		return
	}
	_, page := m.GetControllerView(i.GuildID)
	if page > 1 {
		page--
	}
	_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, coordinator.ControllerViewQueue, page)
}

func handleRadioQueueNext(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if m == nil {
		return
	}
	snap, _ := m.Snapshot(i.GuildID)
	totalPages := (len(snap.Queue) + QueuePageSize - 1) / QueuePageSize
	if totalPages < 1 {
		totalPages = 1
	}
	_, page := m.GetControllerView(i.GuildID)
	if page < totalPages {
		page++
	}
	_, _ = m.SetControllerView(m.LifecycleContext(), i.GuildID, coordinator.ControllerViewQueue, page)
}

func handleRadioQueueShuffle(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if m == nil {
		return
	}
	m.ResetControllerQueuePage(i.GuildID)
	_, _ = m.Control(m.LifecycleContext(), coordinator.ControlRequest{
		GuildID: i.GuildID,
		Action:  coordinator.ControlShuffle,
	})
}

func handleRadioQueueClear(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})
	if m == nil {
		return
	}
	m.ResetControllerQueuePage(i.GuildID)
	_, _ = m.Control(m.LifecycleContext(), coordinator.ControlRequest{
		GuildID: i.GuildID,
		Action:  coordinator.ControlClearQueue,
	})
}

func respondEphemeralCV2(s *discordgo.Session, i *discordgo.InteractionCreate, components []map[string]interface{}) error {
	if s == nil || i == nil || i.Interaction == nil {
		return nil
	}
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral | helpers.CV2FlagsComponentsV2,
			Components: []discordgo.MessageComponent{
				helpers.RawComponent{
					"type":       helpers.ComponentTypeContainer,
					"components": components,
				},
			},
		},
	})
}

func updateEphemeralCV2(s *discordgo.Session, i *discordgo.InteractionCreate, components []map[string]interface{}) error {
	if s == nil || i == nil || i.Interaction == nil {
		return nil
	}
	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral | helpers.CV2FlagsComponentsV2,
			Components: []discordgo.MessageComponent{
				helpers.RawComponent{
					"type":       helpers.ComponentTypeContainer,
					"components": components,
				},
			},
		},
	})
}

func handleRadioLyricsPage(m *coordinator.Module, s *discordgo.Session, i *discordgo.InteractionCreate) {
	if m == nil {
		return
	}
	customID := i.MessageComponentData().CustomID
	pageStr := strings.TrimPrefix(customID, "r_lyr_")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}

	snap, ok := m.Snapshot(i.GuildID)
	if !ok || snap.Current == nil {
		helpers.RespondEphemeral(s, i, "No track currently playing.")
		return
	}

	song, pages, errLyrics := FetchTrackLyrics(m.LifecycleContext(), snap.Current)
	if errLyrics != nil {
		helpers.RespondEphemeral(s, i, errLyrics.Error())
		return
	}

	card := BuildLyricsCV2Card(song, pages, page)
	_ = updateEphemeralCV2(s, i, card)
}
