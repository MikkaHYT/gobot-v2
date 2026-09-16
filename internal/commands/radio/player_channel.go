package radio

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/database"
	"gobot/internal/logger"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

func OnMessageCreateForPlayerChannel(b *bot.Bot) func(s *discordgo.Session, m *discordgo.MessageCreate) {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if s == nil || m == nil || m.GuildID == "" || b == nil || b.Radio == nil {
			return
		}

		botID := ""
		if s.State != nil && s.State.User != nil {
			botID = s.State.User.ID
		}

		if botID != "" && m.Author != nil && m.Author.ID == botID {
			return
		}

		playerChannelID := ""
		if snap, ok := b.Radio.Snapshot(m.GuildID); ok && snap.PlayerChannelID != "" {
			playerChannelID = snap.PlayerChannelID
		}
		if playerChannelID == "" && b.DB != nil {
			playerChannelID, _ = b.DB.GetGuildSettingString(m.GuildID, database.SettingRadioPlayerChannelID)
		}

		if playerChannelID == "" || m.ChannelID != playerChannelID {
			return
		}

		_ = s.ChannelMessageDelete(m.ChannelID, m.ID)

		if m.Author == nil || m.Author.Bot {
			return
		}

		sendEphemeralNotice := func(text string) {
			msg, err := s.ChannelMessageSend(m.ChannelID, text)
			if err == nil && msg != nil {
				time.AfterFunc(4*time.Second, func() {
					_ = s.ChannelMessageDelete(m.ChannelID, msg.ID)
				})
			}
		}

		voiceState, errVS := GetUserVoiceState(s, m.GuildID, m.Author.ID)
		if errVS != nil || voiceState == nil || voiceState.ChannelID == "" {
			sendEphemeralNotice(fmt.Sprintf("<@%s> Connect to a voice channel before playing songs.", m.Author.ID))
			return
		}

		if err := checkBotVoicePermissions(s, m.GuildID, voiceState.ChannelID); err != nil {
			sendEphemeralNotice(fmt.Sprintf("<@%s> %s", m.Author.ID, err.Error()))
			return
		}

		ctx, cancel := context.WithTimeout(b.Radio.LifecycleContext(), 15*time.Second)
		defer cancel()

		if _, errConnect := b.Radio.Connect(ctx, coordinator.ConnectRequest{
			GuildID:          m.GuildID,
			VoiceChannelID:   voiceState.ChannelID,
			TextChannelID:    playerChannelID,
			SuppressAutoplay: true,
		}); errConnect != nil {
			sendEphemeralNotice(fmt.Sprintf("<@%s> Failed to join voice channel: %v", m.Author.ID, errConnect))
			return
		}

		var queuedTracks []*coordinator.Track

		if len(m.Attachments) > 0 {
			att := m.Attachments[0]
			if IsSupportedAudioAttachment(att) {
				track, errAudio := ResolveAudioAttachment(att, ctx)
				if errAudio != nil {
					sendEphemeralNotice(fmt.Sprintf("<@%s> %s", m.Author.ID, errAudio.Error()))
					return
				}
				queuedTracks = []*coordinator.Track{track}
			} else if IsSupportedPlaylistAttachment(att) {
				tempCtx := &bot.Context{
					Session: s,
					Message: m,
					DB:      b.DB,
					Config:  b.Config,
					Radio:   b.Radio,
				}
				tracks, _, errPl := ResolvePlaylistAttachment(tempCtx, att)
				if errPl != nil {
					sendEphemeralNotice(fmt.Sprintf("<@%s> %s", m.Author.ID, errPl.Error()))
					return
				}
				queuedTracks = tracks
			} else {
				sendEphemeralNotice(fmt.Sprintf("<@%s> Unsupported attachment `%s`. Attach an audio or playlist file.", m.Author.ID, att.Filename))
				return
			}
		} else {
			query := strings.TrimSpace(m.Content)
			if query == "" {
				return
			}

			tracks, errResolve := b.Radio.ResolveQuery(ctx, query)
			if errResolve != nil || len(tracks) == 0 {
				sendEphemeralNotice(fmt.Sprintf("<@%s> No tracks found matching `%s`.", m.Author.ID, query))
				return
			}
			queuedTracks = tracks
		}

		if len(queuedTracks) == 0 {
			return
		}

		tracksToQueue := make([]coordinator.Track, len(queuedTracks))
		for idx, tr := range queuedTracks {
			if tr != nil {
				clone := tr.Clone()
				clone.RequesterID = m.Author.ID
				tracksToQueue[idx] = clone
			}
		}

		_, errEnqueue := b.Radio.Enqueue(ctx, coordinator.EnqueueRequest{
			GuildID:        m.GuildID,
			VoiceChannelID: voiceState.ChannelID,
			TextChannelID:  playerChannelID,
			Tracks:         tracksToQueue,
			Placement:      coordinator.EnqueueBack,
		})
		if errEnqueue != nil {
			logger.Warnf("[RADIO PLAYER CHANNEL] Enqueue failed for guild %s: %v", m.GuildID, errEnqueue)
			sendEphemeralNotice(fmt.Sprintf("<@%s> Failed to queue track: %v", m.Author.ID, errEnqueue))
			return
		}

		if len(tracksToQueue) == 1 {
			sendEphemeralNotice(fmt.Sprintf("Queued **%s**.", tracksToQueue[0].Title))
		} else {
			sendEphemeralNotice(fmt.Sprintf("Queued **%d** tracks.", len(tracksToQueue)))
		}
	}
}
