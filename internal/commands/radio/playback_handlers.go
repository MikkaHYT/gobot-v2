package radio

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/juicewrld"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

func (c *RadioGroupCmd) handleJoin(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	voiceState, err := c.ensureConnected(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("Connected to voice channel <#%s>", voiceState.ChannelID),
		Color:       helpers.ColorDefault,
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *RadioGroupCmd) handlePlay(ctx *bot.Context, query string) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	defer c.cleanAttachmentMessage(ctx)

	query = strings.TrimSpace(query)
	if clean, isURL := helpers.CleanMediaURL(query); isURL {
		query = clean
	}
	if query == "" && len(ctx.Message.Attachments) == 0 {
		return ctx.SendError("Please provide a song name, YouTube/SoundCloud URL, or attach an audio file.")
	}

	if len(ctx.Message.Attachments) > 0 {
		if err := c.handlePlayFile(ctx); err != nil {
			return err
		}
		return nil
	}

	if _, err := c.ensureConnected(ctx, true); err != nil {
		return ctx.SendError(err.Error())
	}

	statusMsg, _ := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Searching for `%s`...", query),
		Color:       helpers.ColorPending,
	})

	if !strings.HasPrefix(query, "http://") && !strings.HasPrefix(query, "https://") {
		if candidates, err := fetchJuiceWRLDCandidates(ctx.Context(), query); err == nil && len(candidates) > 1 {
			if statusMsg != nil {
				_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, statusMsg.ID)
			}
			return handleMultiSongSelection(ctx, candidates)
		}
	}

	tracks, err := resolveQuery(ctx, query)
	if err != nil || len(tracks) == 0 {
		if statusMsg != nil {
			_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, statusMsg.ID)
		}
		return ctx.SendError(fmt.Sprintf("Could not resolve song query: `%s`", query))
	}

	cTracks := make([]coordinator.Track, len(tracks))
	for i, tr := range tracks {
		if tr != nil {
			cTracks[i] = tr.Clone()
		}
	}
	prompted, errEnqueue := enqueueRadioTracksWithDuplicateConfirmation(ctx, cTracks, coordinator.EnqueueBack)
	if errEnqueue != nil {
		if statusMsg != nil {
			_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, statusMsg.ID)
		}
		return ctx.SendError(queueEnqueueErrorMessage("Failed to queue song", errEnqueue))
	}

	if statusMsg != nil {
		_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, statusMsg.ID)
	}
	if prompted {
		return nil
	}

	return SendSelfDeletingEmbed(ctx, fmt.Sprintf("Queued **[%s](%s)** (by **%s**).", tracks[0].Title, tracks[0].WebpageURL, ctx.Message.Author.Username), helpers.ColorDefault, helpers.DurationFeedbackShort)
}

func (c *RadioGroupCmd) handlePlayNext(ctx *bot.Context, query string) error {
	defer c.cleanAttachmentMessage(ctx)

	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	query = strings.TrimSpace(query)
	if clean, isURL := helpers.CleanMediaURL(query); isURL {
		query = clean
	}
	if query == "" && len(ctx.Message.Attachments) == 0 {
		return ctx.SendError("Please provide a song query or attach a file to play next.")
	}

	if _, err := c.ensureConnected(ctx, true); err != nil {
		return ctx.SendError(err.Error())
	}

	var queuedTracks []*coordinator.Track
	var notifTitle string

	if len(ctx.Message.Attachments) > 0 {
		att := ctx.Message.Attachments[0]
		isPlaylistTxt := IsSupportedPlaylistAttachment(att)
		isAudio := IsSupportedAudioAttachment(att)

		if !isPlaylistTxt && !isAudio {
			return ctx.SendError(fmt.Sprintf("Unsupported file format `%s`. Please attach an audio file (.mp3, .wav, .m4a, .flac, .ogg, .opus, .aac) or a `.txt` playlist file.", att.Filename))
		}

		if isPlaylistTxt {
			tracks, filename, err := ResolvePlaylistAttachment(ctx, att)
			if err != nil {
				return ctx.SendError(err.Error())
			}
			queuedTracks = tracks
			notifTitle = fmt.Sprintf("Queued **%d** track(s) from `%s` next", len(tracks), filename)
		} else {
			track, err := ResolveAudioAttachment(att, ctx.Context())
			if err != nil {
				return ctx.SendError(err.Error())
			}
			queuedTracks = []*coordinator.Track{track}
			notifTitle = fmt.Sprintf("Queued audio file **%s** next", track.Title)
		}
	} else {
		statusMsg, _ := ctx.ReplyEmbed(&discordgo.MessageEmbed{
			Description: fmt.Sprintf("Searching for `%s`...", query),
			Color:       helpers.ColorPending,
		})

		tracks, err := resolveQuery(ctx, query)
		if statusMsg != nil {
			_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, statusMsg.ID)
		}

		if err != nil || len(tracks) == 0 {
			return ctx.SendError(fmt.Sprintf("Could not resolve song query: `%s`", query))
		}

		queuedTracks = tracks
		if tracks[0].WebpageURL != "" {
			notifTitle = fmt.Sprintf("Queued **[%s](%s)** next", tracks[0].Title, tracks[0].WebpageURL)
		} else {
			notifTitle = fmt.Sprintf("Queued **%s** next", tracks[0].Title)
		}
	}

	if len(queuedTracks) > 0 {
		cTracks := make([]coordinator.Track, len(queuedTracks))
		for i, tr := range queuedTracks {
			if tr != nil {
				cTracks[i] = tr.Clone()
			}
		}
		prompted, errEnqueue := enqueueRadioTracksWithDuplicateConfirmation(ctx, cTracks, coordinator.EnqueueNext)
		if errEnqueue != nil {
			return ctx.SendError(queueEnqueueErrorMessage("Failed to queue song next", errEnqueue))
		}
		if prompted {
			return nil
		}
	}

	embed := &discordgo.MessageEmbed{
		Description: fmt.Sprintf("%s (at top of queue).", notifTitle),
		Color:       helpers.ColorSuccess,
	}
	_, _ = ctx.ReplyEmbed(embed)
	return nil
}

func (c *RadioGroupCmd) handlePlayFile(ctx *bot.Context) error {
	defer c.cleanAttachmentMessage(ctx)

	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	if len(ctx.Message.Attachments) == 0 {
		return ctx.SendError("Please attach an audio file or `.txt` playlist file to your message.")
	}

	att := ctx.Message.Attachments[0]
	isPlaylistTxt := IsSupportedPlaylistAttachment(att)
	isAudio := IsSupportedAudioAttachment(att)

	if !isPlaylistTxt && !isAudio {
		return ctx.SendError(fmt.Sprintf("Unsupported file format `%s`. Please attach an audio file (.mp3, .wav, .m4a, .flac, .ogg, .opus, .aac) or a `.txt` playlist file.", att.Filename))
	}

	var queuedTracks []*coordinator.Track
	var notifDesc string

	if isPlaylistTxt {
		tracks, filename, err := ResolvePlaylistAttachment(ctx, att)
		if err != nil {
			return ctx.SendError(err.Error())
		}
		queuedTracks = tracks
		notifDesc = fmt.Sprintf("Queued **%d** track(s) from playlist file `%s`.", len(tracks), filename)
	} else {
		track, err := ResolveAudioAttachment(att, ctx.Context())
		if err != nil {
			return ctx.SendError(err.Error())
		}
		queuedTracks = []*coordinator.Track{track}
		notifDesc = fmt.Sprintf("Queued audio file **%s** to Juice WRLD Radio.", track.Title)
	}

	if _, err := c.ensureConnected(ctx, true); err != nil {
		return ctx.SendError(err.Error())
	}

	if len(queuedTracks) > 0 {
		cTracks := make([]coordinator.Track, len(queuedTracks))
		for i, tr := range queuedTracks {
			if tr != nil {
				cTracks[i] = tr.Clone()
			}
		}
		prompted, errEnqueue := enqueueRadioTracksWithDuplicateConfirmation(ctx, cTracks, coordinator.EnqueueBack)
		if errEnqueue != nil {
			return ctx.SendError(queueEnqueueErrorMessage("Failed to queue audio file", errEnqueue))
		}
		if prompted {
			return nil
		}
	}

	embed := &discordgo.MessageEmbed{
		Description: notifDesc,
		Color:       helpers.ColorDefault,
	}
	_, _ = ctx.ReplyEmbed(embed)
	return nil
}

func (c *RadioGroupCmd) handlePlaySession(ctx *bot.Context, query string) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}
	if _, err := requireRadio(ctx); err != nil {
		return ctx.SendError(err.Error())
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return ctx.SendError(fmt.Sprintf("Usage: `%sr play session <song_name>` (e.g. `%sr play session Sometimes`)", ctx.Prefix, ctx.Prefix))
	}

	if _, err := c.ensureConnected(ctx, true); err != nil {
		return ctx.SendError(err.Error())
	}

	statusMsg, _ := ctx.ReplyEmbed(&discordgo.MessageEmbed{
		Description: fmt.Sprintf("Searching Juice WRLD sessions for `%s`...", query),
		Color:       helpers.ColorPending,
	})

	apiCtx, cancel := context.WithTimeout(ctx.Context(), 20*time.Second)
	defer cancel()

	songs, err := juicewrld.SearchJuiceWRLDSongs(apiCtx, query, "recording_session")
	if err != nil || len(songs) == 0 {
		songs, err = juicewrld.SearchJuiceWRLDSongs(apiCtx, query, "")
	}

	if statusMsg != nil {
		_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, statusMsg.ID)
	}

	if err != nil || len(songs) == 0 {
		return ctx.SendError(fmt.Sprintf("No Juice WRLD session found for `%s`.", query))
	}

	songs = juicewrld.FilterValidSongs(songs, "session")
	if len(songs) == 0 {
		return ctx.SendError(fmt.Sprintf("No valid Juice WRLD session found for `%s`.", query))
	}

	songs = juicewrld.SortBestMatch(songs, query, "session")
	bestSong := songs[0]

	edits := juicewrld.FindSessionEditItems(apiCtx, bestSong)
	if len(edits) == 0 {
		return ctx.SendError(fmt.Sprintf("No playable session edits found for **%s**.", bestSong.GetName()))
	}

	thumbnail := ""
	if bestSong.ImageURL != "" {
		thumbnail = juicewrld.BuildFullImageURL(bestSong.ImageURL)
	}
	eraStr := ""
	if bestSong.Era != nil {
		eraStr = bestSong.Era.Name
	}

	if len(edits) == 1 {
		edit := edits[0]
		track := &coordinator.Track{
			Title:      formatSessionEditTitle(bestSong.GetName(), edit.Name),
			URL:        edit.URL,
			WebpageURL: edit.URL,
			Uploader:   "Juice WRLD (Studio Session)",
			Thumbnail:  thumbnail,
			Duration:   resolveTrackDuration(bestSong.Length),
			Era:        eraStr,
			Category:   "recording_session",
		}

		prompted, errEnqueue := enqueueRadioTracksWithDuplicateConfirmation(ctx, []coordinator.Track{track.Clone()}, coordinator.EnqueueBack)
		if errEnqueue != nil {
			return ctx.SendError(queueEnqueueErrorMessage("Failed to queue session edit", errEnqueue))
		}
		if prompted {
			return nil
		}

		_ = SendSelfDeletingEmbed(ctx, fmt.Sprintf("Queued session edit **[%s](%s)** (by **%s**).", track.Title, track.WebpageURL, ctx.Message.Author.Username), helpers.ColorDefault, helpers.DurationFeedbackShort)
		return nil
	}

	return handleSessionEditSelection(ctx, bestSong, edits, thumbnail, eraStr)
}

func formatSessionEditTitle(songName, editName string) string {
	cleanSong := helpers.CleanTrackTitle(songName)
	cleanEdit := helpers.CleanTrackTitle(editName)
	if cleanEdit == "" {
		return cleanSong
	}
	if cleanSong == "" {
		return cleanEdit
	}
	if strings.EqualFold(cleanEdit, cleanSong) {
		return cleanSong
	}
	if strings.HasPrefix(strings.ToLower(cleanEdit), strings.ToLower(cleanSong)) {
		return cleanEdit
	}
	return helpers.CleanTrackTitle(fmt.Sprintf("%s (%s)", cleanSong, cleanEdit))
}
