package radio

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	juicewrldCmd "gobot/internal/commands/juicewrld"
	"gobot/internal/helpers"
	juicewrldPkg "gobot/internal/juicewrld"
	coordinator "gobot/internal/radio"
)

// BuildCurrentSongInfoCard creates info card for current track
func BuildCurrentSongInfoCard(ctx context.Context, current *coordinator.Track) []map[string]interface{} {
	if current == nil {
		return nil
	}

	apiCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	songs, errSearch := juicewrldPkg.SearchJuiceWRLDSongs(apiCtx, current.Title, "")
	if errSearch == nil && len(songs) > 0 {
		targetSong := songs[0]
		sorted := juicewrldPkg.SortBestMatch(songs, current.Title, "radio")
		if len(sorted) > 0 {
			targetSong = sorted[0]
		}
		return juicewrldCmd.BuildSongInfoCV2Card(targetSong)
	}

	eraText := current.Era
	if eraText == "" {
		eraText = "Unknown"
	}
	catText := current.Category
	if catText == "" {
		catText = "Audio"
	}
	thumb := current.Thumbnail
	if thumb == "" {
		thumb = helpers.DefaultRadioCoverURL
	}
	return []map[string]interface{}{
		helpers.SectionWithAccessory(
			helpers.MediaAccessory(thumb),
			helpers.TextDisplay(fmt.Sprintf("## %s\n-# **Era**: %s\n-# **Category**: %s\n-# **Duration**: %s",
				current.Title, eraText, catText, FormatTime(float64(current.Duration)))),
		),
	}
}

func FetchTrackLyrics(ctx context.Context, current *coordinator.Track) (*juicewrldPkg.Song, []string, error) {
	if current == nil {
		return nil, nil, fmt.Errorf("no track currently playing")
	}

	apiCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	songs, errSearch := juicewrldPkg.SearchJuiceWRLDSongs(apiCtx, current.Title, "")
	if errSearch != nil || len(songs) == 0 {
		return nil, nil, fmt.Errorf("no lyrics found for this track")
	}

	targetSong := songs[0]
	sorted := juicewrldPkg.SortBestMatch(songs, current.Title, "radio")
	if len(sorted) > 0 {
		targetSong = sorted[0]
	}

	rawLyrics := strings.TrimSpace(targetSong.Lyrics)
	if rawLyrics == "" {
		return &targetSong, nil, fmt.Errorf("no lyrics available for **%s**", targetSong.GetName())
	}

	pages := splitLyricsIntoPages(rawLyrics, 1200)
	return &targetSong, pages, nil
}

func splitLyricsIntoPages(text string, maxChars int) []string {
	stanzas := strings.Split(text, "\n\n")
	var pages []string
	var currentStanzaChunk []string
	currentLen := 0

	for _, stanza := range stanzas {
		trimmedStanza := strings.TrimSpace(stanza)
		if trimmedStanza == "" {
			continue
		}

		if len(trimmedStanza) > maxChars {
			lines := strings.Split(trimmedStanza, "\n")
			var lineChunk []string
			lineChunkLen := 0
			for _, line := range lines {
				if lineChunkLen+len(line)+1 > maxChars && len(lineChunk) > 0 {
					pages = append(pages, strings.Join(lineChunk, "\n"))
					lineChunk = nil
					lineChunkLen = 0
				}
				lineChunk = append(lineChunk, line)
				lineChunkLen += len(line) + 1
			}
			if len(lineChunk) > 0 {
				pages = append(pages, strings.Join(lineChunk, "\n"))
			}
			continue
		}

		if currentLen+len(trimmedStanza)+2 > maxChars && len(currentStanzaChunk) > 0 {
			pages = append(pages, strings.Join(currentStanzaChunk, "\n\n"))
			currentStanzaChunk = nil
			currentLen = 0
		}

		currentStanzaChunk = append(currentStanzaChunk, trimmedStanza)
		currentLen += len(trimmedStanza) + 2
	}

	if len(currentStanzaChunk) > 0 {
		pages = append(pages, strings.Join(currentStanzaChunk, "\n\n"))
	}

	if len(pages) == 0 {
		pages = append(pages, strings.TrimSpace(text))
	}

	return pages
}

func BuildLyricsCV2Card(song *juicewrldPkg.Song, pages []string, page int) []map[string]interface{} {
	if song == nil || len(pages) == 0 {
		return []map[string]interface{}{
			helpers.TextDisplay("No lyrics available."),
		}
	}

	totalPages := len(pages)
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	titleName := song.GetName()
	if titleName == "" {
		titleName = "Lyrics"
	}

	producers := strings.TrimSpace(song.Producers)
	shortEra := juicewrldPkg.GetShortEra(*song)

	var subParts []string
	if producers != "" {
		subParts = append(subParts, "Prod. "+producers)
	}
	if shortEra != "" {
		subParts = append(subParts, shortEra)
	}

	subtitle := ""
	if len(subParts) > 0 {
		subtitle = fmt.Sprintf("-# %s\n\n", strings.Join(subParts, " • "))
	}

	rawPage := pages[page-1]
	lines := strings.Split(rawPage, "\n")
	var quoteLines []string
	for _, l := range lines {
		cleanLine := strings.TrimSpace(l)
		if cleanLine == "" {
			quoteLines = append(quoteLines, ">")
		} else {
			quoteLines = append(quoteLines, "> "+cleanLine)
		}
	}

	lyricsBlock := strings.Join(quoteLines, "\n")

	pageFooter := ""
	if totalPages > 1 {
		pageFooter = fmt.Sprintf("\n\n-# Page %d of %d", page, totalPages)
	}

	content := fmt.Sprintf("### %s - Lyrics\n%s%s%s", titleName, subtitle, lyricsBlock, pageFooter)

	components := []map[string]interface{}{
		helpers.TextDisplay(content),
	}

	if totalPages > 1 {
		components = append(components, helpers.ActionRow(
			map[string]interface{}{
				"type":      helpers.ComponentTypeButton,
				"style":     helpers.ButtonStyleSecondary,
				"custom_id": fmt.Sprintf("r_lyr_%d", page-1),
				"label":     "Previous",
				"disabled":  page <= 1,
			},
			map[string]interface{}{
				"type":      helpers.ComponentTypeButton,
				"style":     helpers.ButtonStyleSecondary,
				"custom_id": fmt.Sprintf("r_lyr_%d", page+1),
				"label":     "Next",
				"disabled":  page >= totalPages,
			},
		))
	}

	return components
}

func (c *RadioGroupCmd) handleInfo(ctx *bot.Context) error {
	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	snap, ok := radio.Snapshot(ctx.Message.GuildID)
	if !ok || snap.Current == nil {
		return ctx.SendError("No track currently playing.")
	}

	card := BuildCurrentSongInfoCard(ctx.Context(), snap.Current)
	if len(card) == 0 {
		return ctx.SendError("Track metadata is currently unavailable.")
	}

	return helpers.SendCV2Message(ctx.Session, ctx.Message.ChannelID, card, nil)
}

func (c *RadioGroupCmd) handleLyrics(ctx *bot.Context) error {
	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	snap, ok := radio.Snapshot(ctx.Message.GuildID)
	if !ok || snap.Current == nil {
		return ctx.SendError("No track currently playing.")
	}

	song, pages, errLyrics := FetchTrackLyrics(ctx.Context(), snap.Current)
	if errLyrics != nil {
		return ctx.SendError(errLyrics.Error())
	}

	card := BuildLyricsCV2Card(song, pages, 1)
	return helpers.SendCV2Message(ctx.Session, ctx.Message.ChannelID, card, nil)
}
