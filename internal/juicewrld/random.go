package juicewrld

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"time"

	"gobot/internal/helpers"
)

var EraDirectories = map[string]string{
	"gbgr":                      "Compilation/2. Unreleased Discography/7. Goodbye & Good Riddance (Sessions)",
	"gb&gr":                     "Compilation/2. Unreleased Discography/7. Goodbye & Good Riddance (Sessions)",
	"goodbye":                   "Compilation/2. Unreleased Discography/7. Goodbye & Good Riddance (Sessions)",
	"goodbye & good riddance":   "Compilation/2. Unreleased Discography/7. Goodbye & Good Riddance (Sessions)",
	"goodbye and good riddance": "Compilation/2. Unreleased Discography/7. Goodbye & Good Riddance (Sessions)",
	"drfl":                      "Compilation/2. Unreleased Discography/9. Death Race For Love (Sessions)",
	"death race":                "Compilation/2. Unreleased Discography/9. Death Race For Love (Sessions)",
	"death race for love":       "Compilation/2. Unreleased Discography/9. Death Race For Love (Sessions)",
	"jw3":                       "Compilation/2. Unreleased Discography/10. Outsiders (Sessions)",
	"juice wrld 3":              "Compilation/2. Unreleased Discography/10. Outsiders (Sessions)",
	"outsiders":                 "Compilation/2. Unreleased Discography/10. Outsiders (Sessions)",
	"the outsiders":             "Compilation/2. Unreleased Discography/10. Outsiders (Sessions)",
	"out":                       "Compilation/2. Unreleased Discography/10. Outsiders (Sessions)",
	"tpne":                      "Compilation/2. Unreleased Discography/10. Outsiders (Sessions)",
	"the party never ends":      "Compilation/2. Unreleased Discography/10. Outsiders (Sessions)",
	"wod":                       "Compilation/2. Unreleased Discography/8. WRLD ON DRUGS (Sessions)",
	"wrld on drugs":             "Compilation/2. Unreleased Discography/8. WRLD ON DRUGS (Sessions)",
	"world on drugs":            "Compilation/2. Unreleased Discography/8. WRLD ON DRUGS (Sessions)",
	"pre":                       "Compilation/2. Unreleased Discography/6. NOTHING'S DIFFERENT 3 (Sessions)",
	"pre-gbgr":                  "Compilation/2. Unreleased Discography/6. NOTHING'S DIFFERENT 3 (Sessions)",
	"pre gbgr":                  "Compilation/2. Unreleased Discography/6. NOTHING'S DIFFERENT 3 (Sessions)",
	"pre-gb&gr":                 "Compilation/2. Unreleased Discography/6. NOTHING'S DIFFERENT 3 (Sessions)",
	"post":                      "Compilation/2. Unreleased Discography/11. Posthumous",
	"posthumous":                "Compilation/2. Unreleased Discography/11. Posthumous",
}

func GetRandomSong(ctx context.Context, eraOrCategory string) (*Song, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cleanMode := strings.ToLower(strings.TrimSpace(eraOrCategory))
	if cleanMode == "all" || cleanMode == "any" || cleanMode == "auto" || cleanMode == "off" || cleanMode == "reset" || cleanMode == "clear" {
		cleanMode = ""
	}

	if dirPath, ok := EraDirectories[cleanMode]; ok {
		return getRandomSongFromEra(ctx, dirPath, cleanMode)
	}

	if catQuery, isCategory := resolveCategoryQuery(cleanMode); isCategory {
		return getRandomSongFromCategory(ctx, catQuery)
	}

	if cleanMode != "" {
		return nil, fmt.Errorf("unknown era or category filter %q", eraOrCategory)
	}

	return getRandomSongFromAPI(ctx)
}

func resolveCategoryQuery(cleanMode string) (string, bool) {
	switch cleanMode {
	case "unreleased", "leak", "leaks":
		return "unreleased", true
	case "released":
		return "released", true
	case "sessions", "session", "recording_session":
		return "recording_session", true
	default:
		return "", false
	}
}

func getRandomSongFromEra(ctx context.Context, dirPath, cleanMode string) (*Song, error) {
	browseCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	items, err := BrowseJuiceWRLDFiles(browseCtx, "", dirPath)
	cancel()

	if err != nil {
		return nil, fmt.Errorf("failed to browse era %q: %w", cleanMode, err)
	}

	type candidateItem struct {
		nameClean string
		path      string
	}
	var validCandidates []candidateItem
	for _, item := range items {
		if item.Type != "file" || item.Path == "" {
			continue
		}
		if !helpers.IsAudioExtension(filepath.Ext(item.Name)) {
			continue
		}
		nameClean := strings.TrimSuffix(item.Name, filepath.Ext(item.Name))
		validCandidates = append(validCandidates, candidateItem{nameClean: nameClean, path: item.Path})
	}

	if len(validCandidates) == 0 {
		return nil, fmt.Errorf("no playable audio files found for era %q", cleanMode)
	}

	picked := validCandidates[rand.Intn(len(validCandidates))]
	song := &Song{
		Name:     picked.nameClean,
		Path:     picked.path,
		Category: "unreleased",
		Era:      &Era{Name: strings.ToUpper(cleanMode)},
	}

	searchCtx, searchCancel := context.WithTimeout(ctx, 2*time.Second)
	if songs, errS := SearchJuiceWRLDSongs(searchCtx, picked.nameClean, ""); errS == nil && len(songs) > 0 {
		matched := songs[0]
		if matched.Era != nil && matched.Era.Name != "" {
			song.Era = matched.Era
		}
		if matched.ImageURL != "" {
			song.ImageURL = matched.ImageURL
		}
		if matched.Category != "" {
			song.Category = matched.Category
		}
		if matched.Length != "" {
			song.Length = matched.Length
		}
	}
	searchCancel()
	return song, nil
}

func getRandomSongFromCategory(ctx context.Context, catQuery string) (*Song, error) {
	catCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	songs, err := SearchJuiceWRLDSongs(catCtx, "", catQuery)
	cancel()

	if err != nil {
		return nil, fmt.Errorf("failed to search category %q: %w", catQuery, err)
	}
	if len(songs) == 0 {
		return nil, fmt.Errorf("no songs found for category %q", catQuery)
	}

	if catQuery == "recording_session" {
		perm := rand.Perm(len(songs))
		for _, idx := range perm {
			s := songs[idx]
			if s.Path != "" && helpers.IsAudioExtension(s.Path) {
				return &s, nil
			}
			editsCtx, editsCancel := context.WithTimeout(ctx, 3*time.Second)
			edits := FindSessionEditItems(editsCtx, s)
			editsCancel()
			if len(edits) > 0 {
				pickedEdit := edits[rand.Intn(len(edits))]
				s.Path = pickedEdit.Path
				return &s, nil
			}
		}
		return nil, fmt.Errorf("no playable session tracks found for category %q", catQuery)
	}

	var playable []Song
	for _, s := range songs {
		if s.Path != "" && helpers.IsAudioExtension(s.Path) {
			playable = append(playable, s)
		}
	}
	if len(playable) == 0 {
		return nil, fmt.Errorf("no playable songs found for category %q", catQuery)
	}
	picked := playable[rand.Intn(len(playable))]
	return &picked, nil
}

func getRandomSongFromAPI(ctx context.Context) (*Song, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	resp, err := GetRandomRadioSong(reqCtx)
	cancel()

	if err != nil || resp == nil || resp.Path == "" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("empty random song response from API")
	}

	if !helpers.IsAudioExtension(resp.Path) {
		return nil, fmt.Errorf("returned random track is not an audio file: %s", resp.Path)
	}

	song := resp.Song
	if song == nil {
		song = &Song{
			Name:     resp.Title,
			Path:     resp.Path,
			Category: "unreleased",
		}
	} else if song.Path == "" {
		song.Path = resp.Path
	}

	if song.Length == "" || song.ImageURL == "" || song.Era == nil {
		searchCtx, searchCancel := context.WithTimeout(ctx, 2*time.Second)
		title := song.GetName()
		if title == "" {
			title = resp.Title
		}
		if songs, errS := SearchJuiceWRLDSongs(searchCtx, title, ""); errS == nil && len(songs) > 0 {
			matched := songs[0]
			if song.Era == nil && matched.Era != nil {
				song.Era = matched.Era
			}
			if song.ImageURL == "" && matched.ImageURL != "" {
				song.ImageURL = matched.ImageURL
			}
			if song.Category == "" && matched.Category != "" {
				song.Category = matched.Category
			}
			if song.Length == "" && matched.Length != "" {
				song.Length = matched.Length
			}
		}
		searchCancel()
	}

	return song, nil
}
