package radio

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gobot/internal/helpers"
	"gobot/internal/juicewrld"
	coordinator "gobot/internal/radio"
)

func resolveTrackDuration(lengthStr string) int {
	d := coordinator.ParseMediaTimestamp(lengthStr)
	if d > 0 {
		return d
	}
	return 180
}

func fetchJuiceWRLDCandidates(ctx context.Context, query string) ([]*Track, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	cleanQuery := strings.TrimSpace(query)

	songs, err := juicewrld.SearchJuiceWRLDSongs(timeoutCtx, cleanQuery, "")
	if err != nil || len(songs) == 0 {
		return nil, fmt.Errorf("no Juice WRLD song found for '%s'", cleanQuery)
	}

	sorted := juicewrld.SortBestMatch(songs, cleanQuery, "radio")
	filtered := juicewrld.FilterValidSongs(sorted, "radio")
	if len(filtered) == 0 {
		filtered = sorted
	}

	if len(filtered) <= 1 {
		return nil, fmt.Errorf("single match")
	}

	var candidates []*Track
	for i, song := range filtered {
		if i >= 10 {
			break
		}
		if song.Path == "" {
			continue
		}
		thumbnail := ""
		if song.ImageURL != "" {
			thumbnail = juicewrld.BuildFullImageURL(song.ImageURL)
		}
		eraStr := ""
		if song.Era != nil {
			eraStr = song.Era.Name
		}
		downloadURL := juicewrld.GetDownloadURL(song.Path)
		candidates = append(candidates, &Track{
			Title:      helpers.CleanTrackTitle(song.GetName()),
			URL:        downloadURL,
			WebpageURL: downloadURL,
			Uploader:   "Juice WRLD 999",
			Thumbnail:  thumbnail,
			Duration:   resolveTrackDuration(song.Length),
			Era:        eraStr,
			Category:   song.Category,
		})
	}

	if len(candidates) <= 1 {
		return nil, fmt.Errorf("single candidate")
	}

	return candidates, nil
}

func FetchJuiceWRLDSong(ctx context.Context, query string) (*coordinator.Track, error) {
	return fetchJuiceWRLDSongContext(ctx, query)
}

func fetchJuiceWRLDSongContext(parentCtx context.Context, query string) (*Track, error) {
	cleanQuery := strings.TrimSpace(query)
	if cleanQuery == "" {
		return nil, fmt.Errorf("empty query")
	}

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, 8*time.Second)
	defer cancel()

	songs, err := juicewrld.SearchJuiceWRLDSongs(ctx, cleanQuery, "")
	if err != nil || len(songs) == 0 {
		return nil, fmt.Errorf("no Juice WRLD song found for '%s'", cleanQuery)
	}

	sorted := juicewrld.SortBestMatch(songs, cleanQuery, "radio")
	filtered := juicewrld.FilterValidSongs(sorted, "radio")
	if len(filtered) == 0 {
		filtered = sorted
	}

	var chosenSong *juicewrld.Song
	for _, s := range filtered {
		if s.Path != "" {
			sCopy := s
			chosenSong = &sCopy
			break
		}
	}

	if chosenSong == nil && len(filtered) > 0 {
		bestCandidate := filtered[0]
		tagged, ogs, _ := juicewrld.FindSongFiles(ctx, bestCandidate)
		if tagged[1] != "" {
			return buildJWTrack(&bestCandidate, tagged[1]), nil
		}
		if len(ogs) > 0 && ogs[0][1] != "" {
			return buildJWTrack(&bestCandidate, ogs[0][1]), nil
		}
	}

	if chosenSong == nil {
		return nil, fmt.Errorf("no valid Juice WRLD song found for '%s'", cleanQuery)
	}

	return buildJWTrack(chosenSong, ""), nil
}

func buildJWTrack(s *juicewrld.Song, directURL string) *Track {
	if s == nil {
		return nil
	}
	thumbnail := ""
	if s.ImageURL != "" {
		thumbnail = juicewrld.BuildFullImageURL(s.ImageURL)
	}
	eraStr := ""
	if s.Era != nil {
		eraStr = juicewrld.GetShortEra(*s)
		if eraStr == "" {
			eraStr = s.Era.Name
		}
	}
	targetURL := directURL
	if targetURL == "" && s.Path != "" {
		targetURL = juicewrld.GetDownloadURL(s.Path)
	}
	dur := resolveTrackDuration(s.Length)
	if dur <= 0 {
		dur = 180
	}
	return &Track{
		Title:      helpers.CleanTrackTitle(s.GetName()),
		URL:        targetURL,
		WebpageURL: targetURL,
		Uploader:   "Juice WRLD",
		Thumbnail:  thumbnail,
		Duration:   dur,
		Era:        eraStr,
		Category:   s.Category,
	}
}

func FetchRandomRadioSong(ctx context.Context, mode string, playedHistory []string) (*Track, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	excludeMap := make(map[string]bool, len(playedHistory))
	for _, h := range playedHistory {
		clean := strings.ToLower(strings.TrimSpace(h))
		if clean != "" {
			excludeMap[clean] = true
		}
	}

	for attempt := 0; attempt < 6; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		song, err := juicewrld.GetRandomSong(reqCtx, mode)
		cancel()

		if err != nil || song == nil {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}

		track := buildJWTrack(song, "")
		if track == nil {
			continue
		}
		titleClean := strings.ToLower(strings.TrimSpace(track.Title))
		if !excludeMap[titleClean] && coordinator.MatchesMode(track, mode) {
			return track, nil
		}
	}

	lastCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if song, err := juicewrld.GetRandomSong(lastCtx, mode); err == nil && song != nil {
		if track := buildJWTrack(song, ""); track != nil {
			if mode == "" || coordinator.MatchesMode(track, mode) {
				return track, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to fetch playable track")
}
