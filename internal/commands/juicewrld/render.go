package juicewrld

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	jw "gobot/internal/juicewrld"

	"github.com/bwmarrin/discordgo"
)

const (
	cv2ActionRow  = helpers.ComponentTypeActionRow
	cv2SelectMenu = helpers.ComponentTypeSelectMenu
	cv2Section    = helpers.ComponentTypeSection
	cv2Text       = helpers.ComponentTypeTextDisplay
	cv2Divider    = helpers.ComponentTypeSeparator
)

func buildCV2ButtonRowsRaw(items [][2]string, maxItems int) []map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	limit := len(items)
	if maxItems > 0 && limit > maxItems {
		limit = maxItems
	}

	var rows []map[string]interface{}
	var currentButtons []map[string]interface{}

	for i := 0; i < limit; i++ {
		btn := helpers.LinkButton(items[i][0], items[i][1])
		currentButtons = append(currentButtons, btn)

		if len(currentButtons) == 5 {
			rows = append(rows, helpers.ActionRow(currentButtons...))
			currentButtons = nil
		}
	}
	if len(currentButtons) > 0 {
		rows = append(rows, helpers.ActionRow(currentButtons...))
	}
	return rows
}

type RenderOptions struct {
	CommandName     string
	Songs           []jw.Song
	SelectedIndex   int
	StatusMessage   *discordgo.Message
	ArtistFilter    string
	CoverPage       int
	CachedCoverURLs []string
}

func BuildSongInfoCV2Card(song jw.Song) []map[string]interface{} {
	header := buildSongInfoHeader(song, "songinfo", "", 0)
	body := renderSongInfoSection(song)
	components := []map[string]interface{}{header}
	components = append(components, body...)
	return components
}


func RenderCommandResponse(ctx *bot.Context, opts RenderOptions) error {
	if len(opts.Songs) == 0 {
		return fmt.Errorf("no songs provided to render")
	}
	selectedIdx := opts.SelectedIndex
	if selectedIdx < 0 || selectedIdx >= len(opts.Songs) {
		selectedIdx = 0
	}
	song := opts.Songs[selectedIdx]
	titleName := song.GetName()
	if titleName == "" {
		titleName = "Unknown Title"
	}

	apiCtx, cancel := context.WithTimeout(ctx.Context(), jw.GetJuiceAPITimeout())
	defer cancel()

	var snippetMediaGallery []map[string]interface{}
	var fileSnipCount int
	if opts.CommandName == "snip" {
		snippetMediaGallery, fileSnipCount = loadSnippetMedia(apiCtx, titleName)
	}

	headerSection := buildSongInfoHeader(song, opts.CommandName, opts.ArtistFilter, fileSnipCount)
	cv2Components := []map[string]interface{}{headerSection}
	var extraRootComponents []map[string]interface{}

	var cachedBody []map[string]interface{}
	if opts.StatusMessage != nil && opts.CommandName != "cover" {
		searchSessionsMu.RLock()
		if sess := searchSessions[opts.StatusMessage.ID]; sess != nil && sess.CachedComponents != nil {
			cachedBody = sess.CachedComponents[selectedIdx]
		}
		searchSessionsMu.RUnlock()
	}

	var bodyComponents []map[string]interface{}
	if cachedBody != nil {
		bodyComponents = cachedBody
	} else {
		switch opts.CommandName {
		case "leak":
			bodyComponents = renderLeakSection(apiCtx, song)
		case "songinfo":
			bodyComponents = renderSongInfoSection(song)
		case "session":
			bodyComponents = renderSessionSection(apiCtx, song)
		case "sessioninfo":
			bodyComponents = renderSessionInfoSection(song)
		case "cover":
			sections, extra := renderCoverSection(apiCtx, song, opts.ArtistFilter, opts.CoverPage, opts.CachedCoverURLs)
			bodyComponents = sections
			extraRootComponents = append(extraRootComponents, extra...)
		case "snip":
			bodyComponents = renderSnipSection(snippetMediaGallery, titleName)
		case "instrumental":
			bodyComponents = renderInstrumentalSection(apiCtx, song)
		}

		if opts.StatusMessage != nil && opts.CommandName != "cover" {
			searchSessionsMu.Lock()
			if sess := searchSessions[opts.StatusMessage.ID]; sess != nil {
				if sess.CachedComponents == nil {
					sess.CachedComponents = make(map[int][]map[string]interface{})
				}
				sess.CachedComponents[selectedIdx] = bodyComponents
			}
			searchSessionsMu.Unlock()
		}
	}
	cv2Components = append(cv2Components, bodyComponents...)

	if len(opts.Songs) > 1 {
		customID := fmt.Sprintf("ver_sel_%s_%s", opts.CommandName, opts.StatusMessage.ID)
		extraRootComponents = append(extraRootComponents, buildVersionSelectMenu(opts.Songs, selectedIdx, customID))
	}

	return helpers.PatchCV2Message(ctx.Session, opts.StatusMessage.ChannelID, opts.StatusMessage.ID, cv2Components, extraRootComponents)
}

func buildSongInfoHeader(song jw.Song, commandName, artistFilter string, fileSnipCount int) map[string]interface{} {
	titleName := song.GetName()
	if titleName == "" {
		titleName = "Unknown Title"
	}
	thumbURL := jw.BuildFullImageURL(song.ImageURL)

	var altNames []string
	seenAlt := make(map[string]bool)
	addAlt := func(t string) {
		tClean := strings.TrimSpace(t)
		if tClean != "" && !strings.EqualFold(tClean, titleName) {
			lower := strings.ToLower(tClean)
			if !seenAlt[lower] {
				seenAlt[lower] = true
				altNames = append(altNames, tClean)
			}
		}
	}
	for _, t := range song.TrackTitles {
		addAlt(t)
	}
	for _, a := range song.AltNames {
		addAlt(a)
	}
	engineers := strings.TrimSpace(song.Engineers)
	producers := strings.TrimSpace(song.Producers)

	headerText := ""
	switch commandName {
	case "sessioninfo":
		headerText = fmt.Sprintf("### %s - Session Info", titleName)
	case "instrumental":
		headerText = fmt.Sprintf("### %s - Instrumental", titleName)
	case "snip":
		headerText = fmt.Sprintf("### %s", titleName)
	case "cover":
		if artistFilter != "" {
			headerText = fmt.Sprintf("**Covers for: %s (by %s)**", titleName, artistFilter)
		} else {
			headerText = fmt.Sprintf("**Covers for: %s**", titleName)
		}
	default:
		headerText = fmt.Sprintf("### %s", titleName)
	}

	if len(altNames) > 0 {
		headerText += fmt.Sprintf("\n-# Alt Name(s): **%s**", strings.Join(altNames, ", "))
	}
	if commandName != "snip" {
		if engineers != "" {
			headerText += fmt.Sprintf("\n-# Engineer(s): **%s**", engineers)
		}
		if producers != "" {
			headerText += fmt.Sprintf("\n-# Producer(s): **%s**", producers)
		}
	}

	if commandName == "snip" {
		if _, prevVal := jw.ParseHeaderVal(song.PreviewDate, "First Previewed"); prevVal != "" {
			cleanVal := strings.ReplaceAll(prevVal, "\r\n", "\n")
			cleanVal = strings.ReplaceAll(cleanVal, "**", "")
			lines := strings.Split(cleanVal, "\n")
			var validLines []string
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" {
					validLines = append(validLines, l)
				}
			}
			if len(validLines) > 0 {
				cleanDate := CleanHeaderBodyPrefix(validLines[0], "First Previewed")
				if cleanDate != "" {
					headerText += fmt.Sprintf("\n-# First Previewed: **%s**", cleanDate)
				}
			}
		}
		snipCount := song.SnippetCount()
		if fileSnipCount > snipCount {
			snipCount = fileSnipCount
		}
		headerText += fmt.Sprintf("\n-# Total Snippets: **%d**", snipCount)
	}

	section := map[string]interface{}{
		"type": cv2Section,
		"components": []map[string]interface{}{
			helpers.TextDisplay(headerText),
		},
	}
	if thumbURL != "" {
		section["accessory"] = helpers.MediaAccessory(thumbURL)
	}
	return section
}

func loadSnippetMedia(apiCtx context.Context, titleName string) ([]map[string]interface{}, int) {
	var snippetMediaGallery []map[string]interface{}
	var fileSnipCount int

	browseItems, err := jw.BrowseJuiceWRLDFiles(apiCtx, titleName, "")
	if err != nil {
		return nil, 0
	}
	seenPaths := make(map[string]bool)
	seenFileNames := make(map[string]bool)
	for _, item := range browseItems {
		nameLower := strings.ToLower(item.Name)
		if item.Type == "file" && !seenPaths[item.Path] && !seenFileNames[nameLower] && (strings.Contains(item.Path, "Snippets/") || strings.Contains(item.Path, "Snippets-Old/")) {
			seenPaths[item.Path] = true
			seenFileNames[nameLower] = true
			fileSnipCount++
			dlURL := jw.GetDownloadURL(item.Path)
			snippetMediaGallery = append(snippetMediaGallery, map[string]interface{}{
				"media": map[string]interface{}{
					"url": dlURL,
				},
			})
		}
	}
	return snippetMediaGallery, fileSnipCount
}

func renderLeakSection(apiCtx context.Context, song jw.Song) []map[string]interface{} {
	var components []map[string]interface{}
	taggedItem, ogItems, totalCandidates := jw.FindSongFiles(apiCtx, song)
	if taggedItem[1] != "" || len(ogItems) > 0 {
		components = append(components, map[string]interface{}{"type": cv2Divider, "divider": true})
	}
	if taggedItem[1] != "" {
		components = append(components, helpers.TextDisplay("**Tagged File(s)**"))
		components = append(components, helpers.ActionRow(
			helpers.LinkButton(taggedItem[0], taggedItem[1]),
		))
	}
	if len(ogItems) > 0 {
		components = append(components, helpers.TextDisplay("**Original File(s)**"))
		components = append(components, buildCV2ButtonRowsRaw(ogItems, 20)...)
		if totalCandidates > len(ogItems) {
			components = append(components, helpers.TextDisplay(
				fmt.Sprintf("-# Found %d candidate(s). Matched the best %d file(s).", totalCandidates, len(ogItems)),
			))
		}
	}
	return components
}

func renderSongInfoSection(song jw.Song) []map[string]interface{} {
	var components []map[string]interface{}
	components = append(components, map[string]interface{}{"type": cv2Divider, "divider": true})
	var fieldsParts []string

	if eraVal := jw.CleanEraDisplay(song.Era); eraVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Era**\n%s", eraVal))
	}
	if strings.TrimSpace(song.FileNames) != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**File Name**\n%s", strings.TrimSpace(song.FileNames)))
	}
	instr := strings.TrimSpace(song.Instrumentals)
	if instr == "" {
		instr = strings.TrimSpace(song.InstrumentalNames)
	}
	if instr != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Instrumentals**\n%s", instr))
	}
	if strings.TrimSpace(song.RecordingLocations) != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Recording Location**\n%s", strings.TrimSpace(song.RecordingLocations)))
	}
	if recTitle, recVal := jw.ParseHeaderVal(song.RecordDates, "Recorded"); recVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**%s**\n%s", recTitle, CleanHeaderBodyPrefix(recVal, recTitle)))
	}
	if prevTitle, prevVal := jw.ParseHeaderVal(song.PreviewDate, "Previewed"); prevVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**%s**\n%s", prevTitle, CleanHeaderBodyPrefix(prevVal, prevTitle)))
	}
	if song.Category == "released" {
		if relTitle, relVal := jw.ParseHeaderVal(song.ReleaseDate, "Released"); relVal != "" {
			fieldsParts = append(fieldsParts, fmt.Sprintf("**%s**\n%s", relTitle, CleanHeaderBodyPrefix(relVal, relTitle)))
		}
	}
	if surfTitle, surfVal := jw.ParseHeaderVal(song.DateLeaked, "Surfaced"); surfVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**%s**\n%s", surfTitle, CleanHeaderBodyPrefix(surfVal, surfTitle)))
	}
	if strings.TrimSpace(song.Length) != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Length**\n%s", strings.TrimSpace(song.Length)))
	}
	catVal := strings.TrimSpace(song.LeakType)
	if catVal == "" {
		catVal = jw.FormatCategory(strings.TrimSpace(song.Category))
	}
	if catVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Category**\n%s", catVal))
	}
	if bitTitle, bitVal := jw.ParseHeaderVal(song.Bitrate, "Available Files"); bitVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Available Files**\n%s\n%s", bitTitle, bitVal))
	}
	if strings.TrimSpace(song.AdditionalInformation) != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Additional Info**\n%s", strings.TrimSpace(song.AdditionalInformation)))
	}
	if count := song.SnippetCount(); count > 0 {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Snippets**\n%d", count))
	}

	for _, part := range fieldsParts {
		components = append(components, map[string]interface{}{"type": cv2Text, "content": part})
	}
	return components
}

func renderSessionSection(apiCtx context.Context, song jw.Song) []map[string]interface{} {
	var components []map[string]interface{}
	sessionDownloads, sessionEdits := jw.FindSessionFiles(apiCtx, song)
	if len(sessionDownloads) > 0 || len(sessionEdits) > 0 {
		components = append(components, map[string]interface{}{"type": cv2Divider, "divider": true})
	}
	if len(sessionDownloads) > 0 {
		components = append(components, map[string]interface{}{"type": cv2Text, "content": "**Session Download(s)**"})
		components = append(components, buildCV2ButtonRowsRaw(sessionDownloads, 20)...)
		if len(sessionDownloads) > 1 {
			components = append(components, helpers.TextDisplay(
				fmt.Sprintf("-# Multiple sessions matched (%d). Results may be inaccurate.", len(sessionDownloads)),
			))
		}
	}
	if len(sessionEdits) > 0 {
		components = append(components, map[string]interface{}{"type": cv2Text, "content": "**Session Edit(s)**"})
		components = append(components, buildCV2ButtonRowsRaw(sessionEdits, 20)...)
	}
	return components
}

func renderSessionInfoSection(song jw.Song) []map[string]interface{} {
	var components []map[string]interface{}
	components = append(components, map[string]interface{}{"type": cv2Divider, "divider": true})
	var fieldsParts []string

	if strings.TrimSpace(song.SessionTitles) != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Session Title(s)**\n%s", strings.TrimSpace(song.SessionTitles)))
	}
	if strings.TrimSpace(song.SessionTracking) != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Session Tracking**\n%s", strings.TrimSpace(song.SessionTracking)))
	}
	if eraVal := jw.CleanEraDisplay(song.Era); eraVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Era**\n%s", eraVal))
	}
	if strings.TrimSpace(song.RecordingLocations) != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Recording Location**\n%s", strings.TrimSpace(song.RecordingLocations)))
	}
	if recTitle, recVal := jw.ParseHeaderVal(song.RecordDates, "Recorded"); recVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**%s**\n%s", recTitle, CleanHeaderBodyPrefix(recVal, recTitle)))
	}
	if surfTitle, surfVal := jw.ParseHeaderVal(song.DateLeaked, "Surfaced"); surfVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**%s**\n%s", surfTitle, surfVal))
	}
	catVal := strings.TrimSpace(song.LeakType)
	if catVal == "" {
		catVal = jw.FormatCategory(strings.TrimSpace(song.Category))
	}
	if catVal != "" {
		fieldsParts = append(fieldsParts, fmt.Sprintf("**Category**\n%s", catVal))
	}

	for _, part := range fieldsParts {
		components = append(components, map[string]interface{}{"type": cv2Text, "content": part})
	}
	return components
}

func renderCoverSection(apiCtx context.Context, song jw.Song, artistFilter string, coverPage int, cachedCoverURLs []string) ([]map[string]interface{}, []map[string]interface{}) {
	var components []map[string]interface{}
	var extraRootComponents []map[string]interface{}
	components = append(components, map[string]interface{}{"type": cv2Divider, "divider": true})

	validCoverURLs := cachedCoverURLs
	if len(validCoverURLs) == 0 {
		validCoverURLs = jw.FindCoverArtFiles(apiCtx, song, artistFilter)
	}

	if len(validCoverURLs) > 0 {
		const coversPerPage = 10
		totalPages := (len(validCoverURLs) + coversPerPage - 1) / coversPerPage
		if totalPages <= 0 {
			totalPages = 1
		}

		page := coverPage
		if page < 0 {
			page = 0
		}
		if page >= totalPages {
			page = totalPages - 1
		}

		start := page * coversPerPage
		end := start + coversPerPage
		if end > len(validCoverURLs) {
			end = len(validCoverURLs)
		}

		pageCovers := validCoverURLs[start:end]
		var mediaGalleryItems []map[string]interface{}
		for _, u := range pageCovers {
			mediaGalleryItems = append(mediaGalleryItems, map[string]interface{}{
				"media": map[string]interface{}{
					"url": u,
				},
			})
		}

		if len(mediaGalleryItems) > 0 {
			components = append(components, map[string]interface{}{
				"type":  12,
				"items": mediaGalleryItems,
			})
		}

		components = append(components, map[string]interface{}{
			"type":    cv2Text,
			"content": fmt.Sprintf("-# Displaying %d-%d of %d covers (Page %d of %d) - (Covers may take time to embed) ", start+1, end, len(validCoverURLs), page+1, totalPages),
		})

		if totalPages > 1 {
			extraRootComponents = append(extraRootComponents, helpers.PaginationRow("jw_cover_prev", "jw_cover_next", page, totalPages))
		}
	} else if artistFilter != "" {
		components = append(components, map[string]interface{}{
			"type":    cv2Text,
			"content": fmt.Sprintf("-# No covers found matching artist `%s`.", artistFilter),
		})
	} else {
		components = append(components, map[string]interface{}{
			"type":    cv2Text,
			"content": "-# No covers found.",
		})
	}
	return components, extraRootComponents
}

func renderSnipSection(snippetMediaGallery []map[string]interface{}, titleName string) []map[string]interface{} {
	var components []map[string]interface{}
	components = append(components, map[string]interface{}{"type": cv2Divider, "divider": true})
	if len(snippetMediaGallery) > 0 {
		for i := 0; i < len(snippetMediaGallery); i += 10 {
			end := i + 10
			if end > len(snippetMediaGallery) {
				end = len(snippetMediaGallery)
			}
			components = append(components, map[string]interface{}{
				"type":  12,
				"items": snippetMediaGallery[i:end],
			})
		}
	} else {
		components = append(components, map[string]interface{}{
			"type":    cv2Text,
			"content": fmt.Sprintf("-# No snippets available for **%s**.", titleName),
		})
	}
	return components
}

func renderInstrumentalSection(apiCtx context.Context, song jw.Song) []map[string]interface{} {
	var components []map[string]interface{}
	components = append(components, map[string]interface{}{"type": cv2Divider, "divider": true})

	instr := strings.TrimSpace(song.Instrumentals)
	if instr == "" {
		instr = strings.TrimSpace(song.InstrumentalNames)
	}
	hasInstrInfo := instr != "" && !strings.EqualFold(instr, "n/a")
	if hasInstrInfo {
		components = append(components, helpers.TextDisplay(fmt.Sprintf("**Instrumentals**\n%s", instr)))
	}

	items := jw.FindInstrumentalFiles(apiCtx, song)
	if len(items) > 0 {
		components = append(components, helpers.TextDisplay("**Instrumental File(s)**"))
		components = append(components, buildCV2ButtonRowsRaw(items, 20)...)
	} else if hasInstrInfo {
		components = append(components, helpers.TextDisplay("-# No instrumental audio files found in archive."))
	} else {
		components = append(components, helpers.TextDisplay("-# No instrumental files found for this song."))
	}
	return components
}

func buildVersionSelectMenu(songs []jw.Song, selectedIdx int, customID string) map[string]interface{} {
	var selectOptions []map[string]interface{}
	limit := len(songs)
	if limit > 25 {
		limit = 25
	}
	for i := 0; i < limit; i++ {
		s := songs[i]
		name := helpers.TruncateString(s.GetName(), 100)
		desc := helpers.TruncateStringWithEllipsis(jw.GetDropdownDescription(s), 100)

		opt := map[string]interface{}{
			"label":   name,
			"value":   fmt.Sprintf("%d", i),
			"default": i == selectedIdx,
		}
		if desc != "" {
			opt["description"] = desc
		}
		selectOptions = append(selectOptions, opt)
	}

	selectMenu := map[string]interface{}{
		"type":        cv2SelectMenu,
		"custom_id":   customID,
		"placeholder": "Select a version...",
		"min_values":  1,
		"max_values":  1,
		"options":     selectOptions,
	}

	return map[string]interface{}{
		"type":       cv2ActionRow,
		"components": []map[string]interface{}{selectMenu},
	}
}

func ExecuteSearchCommand(ctx *bot.Context, commandName string, query string, category string) error {
	rawQuery := strings.TrimSpace(query)
	if rawQuery == "" {
		embed := &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Usage: `%s%s <song_name>`", ctx.Prefix, commandName),
			Color:       helpers.ColorDefault,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	searchQuery := rawQuery
	artistFilter := ""
	if commandName == "cover" {
		if first, second, ok := splitByCaseInsensitive(rawQuery, " by "); ok {
			searchQuery = strings.TrimSpace(first)
			artistFilter = strings.TrimSpace(second)
		}
	}

	tempComponents := []map[string]interface{}{
		helpers.TextDisplay("Searching..."),
	}
	statusMsg, err := helpers.SendCV2MessageAndReturn(ctx.Session, ctx.Message.ChannelID, tempComponents, nil)
	if err != nil {
		return err
	}

	apiCtx, cancel := context.WithTimeout(ctx.Context(), jw.GetJuiceAPITimeout())
	defer cancel()

	songs, err := jw.SearchJuiceWRLDSongs(apiCtx, searchQuery, category)
	if (err != nil || len(songs) == 0) && category != "" {
		fallbackSongs, fallbackErr := jw.SearchJuiceWRLDSongs(apiCtx, searchQuery, "")
		if fallbackErr == nil && len(fallbackSongs) > 0 {
			songs = fallbackSongs
			err = nil
		}
	}

	if err != nil {
		bot.Errorf("[JUICEWRLD] API error for query '%s': %v", searchQuery, err)
		_ = helpers.PatchCV2Message(ctx.Session, statusMsg.ChannelID, statusMsg.ID, []map[string]interface{}{
			helpers.TextDisplay(fmt.Sprintf("An error occurred while searching for `%s`. Please try again later.", searchQuery)),
		}, nil)
		return nil
	}

	songs = jw.FilterValidSongs(songs, commandName)

	if len(songs) == 0 {
		_ = helpers.PatchCV2Message(ctx.Session, statusMsg.ChannelID, statusMsg.ID, []map[string]interface{}{
			helpers.TextDisplay(fmt.Sprintf("No results found matching `%s`.", searchQuery)),
		}, nil)
		return nil
	}

	if commandName == "snip" && len(songs) > 0 {
		songs = filterSongsWithSnippets(apiCtx, songs)
	}

	songs = jw.SortBestMatch(songs, searchQuery, commandName)

	var initialCoverURLs []string
	if commandName == "cover" && len(songs) > 0 {
		initialCoverURLs = jw.FindCoverArtFiles(apiCtx, songs[0], artistFilter)
	}

	authorID := ctx.Message.Author.ID

	searchSessionsMu.Lock()
	pruneExpiredSessionsLocked(time.Now())
	searchSessions[statusMsg.ID] = &SearchSession{
		CommandName:      commandName,
		Songs:            songs,
		AuthorID:         authorID,
		StatusMsgID:      statusMsg.ID,
		ArtistFilter:     artistFilter,
		CurrentIndex:     0,
		CoverPage:        0,
		CoverURLs:        initialCoverURLs,
		CachedComponents: make(map[int][]map[string]interface{}),
		LastInteraction:  time.Now(),
		CreatedAt:        time.Now(),
	}
	searchSessionsMu.Unlock()

	return RenderCommandResponse(ctx, RenderOptions{
		CommandName:     commandName,
		Songs:           songs,
		StatusMessage:   statusMsg,
		ArtistFilter:    artistFilter,
		CachedCoverURLs: initialCoverURLs,
	})
}

func ExecuteRandomSongCommand(ctx *bot.Context, filter string) error {
	statusText := "Searching for a random song..."
	if filter != "" {
		statusText = fmt.Sprintf("Searching for a random song in `%s`...", filter)
	}

	tempComponents := []map[string]interface{}{
		helpers.TextDisplay(statusText),
	}
	statusMsg, err := helpers.SendCV2MessageAndReturn(ctx.Session, ctx.Message.ChannelID, tempComponents, nil)
	if err != nil {
		return err
	}

	apiCtx, cancel := context.WithTimeout(ctx.Context(), 10*time.Second)
	defer cancel()

	song, err := jw.GetRandomSong(apiCtx, filter)
	if err != nil || song == nil {
		bot.Errorf("[JUICEWRLD] Random song error: %v", err)
		errText := "Could not fetch a random song. Please try again."
		if filter != "" {
			errText = fmt.Sprintf("Could not fetch a random song for filter `%s`. Please check era name or try again.", filter)
		}
		_ = helpers.PatchCV2Message(ctx.Session, statusMsg.ChannelID, statusMsg.ID, []map[string]interface{}{
			helpers.TextDisplay(errText),
		}, nil)
		return nil
	}

	authorID := ctx.Message.Author.ID
	searchSessionsMu.Lock()
	pruneExpiredSessionsLocked(time.Now())
	searchSessions[statusMsg.ID] = &SearchSession{
		CommandName:      "songinfo",
		Songs:            []jw.Song{*song},
		AuthorID:         authorID,
		StatusMsgID:      statusMsg.ID,
		CurrentIndex:     0,
		CoverPage:        0,
		CachedComponents: make(map[int][]map[string]interface{}),
		LastInteraction:  time.Now(),
		CreatedAt:        time.Now(),
	}
	searchSessionsMu.Unlock()

	return RenderCommandResponse(ctx, RenderOptions{
		CommandName:   "songinfo",
		Songs:         []jw.Song{*song},
		StatusMessage: statusMsg,
	})
}

func filterSongsWithSnippets(apiCtx context.Context, songs []jw.Song) []jw.Song {
	var withSnippets []jw.Song
	for _, s := range songs {
		if s.SnippetCount() > 0 {
			withSnippets = append(withSnippets, s)
		}
	}
	if len(withSnippets) > 0 {
		return withSnippets
	}

	limit := len(songs)
	if limit > 3 {
		limit = 3
	}
	for i := 0; i < limit; i++ {
		s := songs[i]
		if browseItems, err := jw.BrowseJuiceWRLDFiles(apiCtx, s.GetName(), ""); err == nil {
			for _, item := range browseItems {
				if item.Type == "file" && (strings.Contains(item.Path, "Snippets/") || strings.Contains(item.Path, "Snippets-Old/")) {
					withSnippets = append(withSnippets, s)
					break
				}
			}
		}
	}
	if len(withSnippets) > 0 {
		return withSnippets
	}
	return songs
}

func CleanHeaderBodyPrefix(body string, title string) string {
	clean := strings.TrimSpace(body)
	if clean == "" {
		return ""
	}

	titleClean := strings.TrimSpace(title)
	if titleClean == "" {
		return clean
	}

	lowerClean := strings.ToLower(clean)
	lowerTitle := strings.ToLower(titleClean)

	if lowerClean == lowerTitle {
		return ""
	}

	patterns := []string{
		strings.ToLower(fmt.Sprintf("%s:", titleClean)),
		strings.ToLower(fmt.Sprintf("**%s:**", titleClean)),
		strings.ToLower(fmt.Sprintf("**%s**:", titleClean)),
		strings.ToLower(fmt.Sprintf("**%s**", titleClean)),
		strings.ToLower(fmt.Sprintf("%s -", titleClean)),
		strings.ToLower(fmt.Sprintf("%s-", titleClean)),
	}

	for _, pat := range patterns {
		if strings.HasPrefix(lowerClean, pat) {
			clean = strings.TrimSpace(clean[len(pat):])
			break
		}
	}

	return clean
}

func splitByCaseInsensitive(s, sep string) (string, string, bool) {
	lowerS := strings.ToLower(s)
	lowerSep := strings.ToLower(sep)
	idx := strings.Index(lowerS, lowerSep)
	if idx == -1 {
		return "", "", false
	}
	return s[:idx], s[idx+len(sep):], true
}
