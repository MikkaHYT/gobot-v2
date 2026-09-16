package juicewrld

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"gobot/internal/helpers"
)

var (
	specialPunctRegex  = regexp.MustCompile(`(?i)</3|<3|\(.*?\)|\[.*?\]`)
	versionRegex       = regexp.MustCompile(`(?i)\s*\(v\d+\)`)
	fileNameRegex      = regexp.MustCompile(`(?i)File Name(?:\s*\(\d+\))?\s*:\s*`)
	sessionTitleRegex  = regexp.MustCompile(`(?i)Session Title(?:\s*\(\d+\))?\s*:\s*`)
	parenRegex         = regexp.MustCompile(`\s*\([^)]*\)`)
	eraSuffixRegex     = regexp.MustCompile(`(?i)\s+era$`)
	trackPrefixRegex   = regexp.MustCompile(`^(?:\d{1,3}[\.\-_\s]+)`)
	artistPrefixRegex  = regexp.MustCompile(`^(?i)(?:juice\s*wrld\s*[-_–]\s*|juice\s*[-_–]\s*)`)
	bracketBlockRegex  = regexp.MustCompile(`\s*[\(\[\{][^\)\]\}]*[\)\]\}]`)
	versionSuffixRegex = regexp.MustCompile(`(?i)\s*(?:v\d+(?:\.\d+)?|take\s*\d+|rough(?:\s*v\d+)?|demo|session\s*edit|studio\s*session|protools|logic|ableton|stems).*$`)
	channelSuffixRegex = regexp.MustCompile(`(?i)[\._][lr]$`)
	instrumentalPrefixRegex     = regexp.MustCompile(`(?i)^(?:Instrumental(?:\s*\(\d+\))?|Loop(?:\s*\(\d+\))?|MIDI(?:\s*\(\d+\))?|File\s*Name(?:\s*\(\d+\))?)\s*:\s*`)
	skipInstrumentalPrefixRegex = regexp.MustCompile(`(?i)^(?:Beat\s*Pack|Loop\s*Kit|Sample|Session\s*Title|Folder\s*Title)\s*:`)
	genericInstrumentalRegex    = regexp.MustCompile(`(?i)^(?:instrumental|inst|beat|main|master|rough|tv\s*mix|remix|audio|track\s*\d+)(?:\s*(?:v\d+|demo|\(\d+\)))?$`)
	instrumentalTagRegex        = regexp.MustCompile(`(?i)(?:[\s\-_]+|^)(?:instrumental|inst)(?:[\s\-_]+|$)`)
)

func getExtLabel(path string) string {
	ext := filepath.Ext(path)
	if ext != "" {
		return strings.ToUpper(strings.TrimPrefix(ext, "."))
	}
	return "FILE"
}

func GetCleanExtLabel(path string) string {
	return getExtLabel(extractPathFromURL(path))
}

func IsEmbeddableImageFormat(pathOrURL string) bool {
	cleanPath := extractPathFromURL(pathOrURL)
	ext := strings.ToLower(filepath.Ext(cleanPath))
	return ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" || ext == ".gif"
}

func GetFileButtonLabel(urlOrPath string, isOG bool) string {
	pathClean := extractPathFromURL(urlOrPath)
	extClean := getExtLabel(pathClean)

	if strings.Contains(pathClean, "Studio Sessions") || extClean == "ZIP" {
		return fmt.Sprintf("Session %s", extClean)
	}
	if isOG {
		return fmt.Sprintf("OG %s", extClean)
	}
	return extClean
}

var lineBreakReplacer = strings.NewReplacer("\r\n", "\n", "\r", "\n")

func splitLines(s string) []string {
	return strings.Split(lineBreakReplacer.Replace(s), "\n")
}

func parseFileNames(fileNames string) []string {
	if fileNames == "" {
		return nil
	}
	var result []string
	for _, line := range splitLines(fileNames) {
		clean := strings.TrimSpace(fileNameRegex.ReplaceAllString(line, ""))
		if clean != "" && !strings.EqualFold(clean, "n/a") {
			result = append(result, clean)
		}
	}
	return result
}

func parseSessionTitles(sessionTitles string) []string {
	if sessionTitles == "" {
		return nil
	}
	var result []string
	for _, line := range splitLines(sessionTitles) {
		clean := strings.TrimSpace(sessionTitleRegex.ReplaceAllString(line, ""))
		if clean != "" {
			result = append(result, clean)
		}
	}
	return result
}

func hasArchiveExt(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".rar") || strings.HasSuffix(lower, ".7z")
}

func FormatCategory(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			r, size := utf8.DecodeRuneInString(w)
			words[i] = string(unicode.ToUpper(r)) + w[size:]
		}
	}
	return strings.Join(words, " ")
}

func ParseHeaderVal(text string, fallbackTitle string) (string, string) {
	if strings.TrimSpace(text) == "" {
		return "", ""
	}
	lines := splitLines(strings.TrimSpace(text))
	if len(lines) > 1 && len(lines[0]) < 25 && !strings.Contains(lines[0], ":") {
		return strings.TrimSpace(lines[0]), strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	return fallbackTitle, strings.TrimSpace(strings.Join(lines, "\n"))
}

func CleanEraDisplay(era *Era) string {
	if era == nil {
		return ""
	}
	val := era.Description
	if val == "" {
		val = era.Name
	}
	if val == "" {
		return ""
	}

	val = parenRegex.ReplaceAllString(val, "")
	val = eraSuffixRegex.ReplaceAllString(val, "")
	return strings.TrimSpace(val)
}

var shortEraMap = map[string]string{
	"gb&gr":                     "GBGR",
	"gb & gr":                   "GBGR",
	"goodbye & good riddance":   "GBGR",
	"goodbye and good riddance": "GBGR",
	"drfl":                      "DRFL",
	"death race for love":       "DRFL",
	"jw3":                       "JW3",
	"juice wrld 3":              "JW3",
	"post":                      "POST",
	"posthumous":                "POST",
	"wod":                       "WOD",
	"world on drugs":            "WOD",
	"wrld on drugs":             "WOD",
	"pre-gbgr":                  "PRE-GBGR",
	"pre gbgr":                  "PRE-GBGR",
}

var shortEraContains = []struct{ key, value string }{
	{"goodbye & good riddance", "GBGR"},
	{"goodbye and good riddance", "GBGR"},
	{"death race for love", "DRFL"},
	{"world on drugs", "WOD"},
	{"wrld on drugs", "WOD"},
	{"juice wrld 3", "JW3"},
	{"posthumous", "POST"},
	{"pre-gbgr", "PRE-GBGR"},
	{"pre gbgr", "PRE-GBGR"},
	{"gb & gr", "GBGR"},
	{"gb&gr", "GBGR"},
	{"drfl", "DRFL"},
	{"jw3", "JW3"},
	{"post", "POST"},
	{"wod", "WOD"},
}

func GetShortEra(song Song) string {
	if song.Era == nil {
		return ""
	}
	rawEra := song.Era.Name
	if rawEra == "" {
		rawEra = song.Era.Description
	}

	rawClean := strings.TrimSpace(strings.ToLower(rawEra))
	if mapped, ok := shortEraMap[rawClean]; ok {
		return mapped
	}

	for _, entry := range shortEraContains {
		if strings.Contains(rawClean, entry.key) {
			return entry.value
		}
	}

	return strings.TrimSpace(strings.ToUpper(strings.ReplaceAll(rawEra, "&", "")))
}

func GetFirstAltName(song Song) string {
	mainName := strings.TrimSpace(strings.ToLower(song.GetName()))

	for _, t := range song.TrackTitles {
		cleanT := strings.TrimSpace(t)
		if cleanT != "" && strings.ToLower(cleanT) != mainName {
			return cleanT
		}
	}

	for _, a := range song.AltNames {
		cleanA := strings.TrimSpace(a)
		if cleanA != "" && strings.ToLower(cleanA) != mainName {
			return cleanA
		}
	}

	return ""
}

func GetDropdownDescription(song Song) string {
	shortEra := GetShortEra(song)
	altName := GetFirstAltName(song)

	if shortEra != "" && altName != "" {
		return fmt.Sprintf("%s | %s", shortEra, strings.ToUpper(altName))
	} else if shortEra != "" {
		return shortEra
	} else if altName != "" {
		return strings.ToUpper(altName)
	}
	return ""
}

func ParseAvailableFiles(bitrate string) AvailableFiles {
	lower := strings.ToLower(bitrate)
	m := AvailableFiles{}

	if strings.Contains(lower, "original file") || strings.Contains(lower, "original files") || strings.Contains(lower, ".l/.r") || strings.Contains(lower, "trackout") {
		m.HasOriginalFiles = true
	}

	if strings.Contains(lower, ".l/.r") || strings.Contains(lower, ".l/.r 1536kbps") {
		m.HasLRWav = true
		m.HasWav = true
	} else if strings.Contains(lower, ".wav") {
		m.HasWav = true
	}

	if strings.Contains(lower, "320kbps") && strings.Contains(lower, ".mp3") {
		m.HasMp3 = true
	} else if strings.Contains(lower, "original file") && strings.Contains(lower, ".mp3") && !strings.Contains(lower, "160kbps") {
		m.HasMp3 = true
	} else if !m.HasWav && strings.Contains(lower, ".mp3") && m.HasOriginalFiles {
		m.HasMp3 = true
	}

	return m
}

func acquireSem(ctx context.Context, sem chan struct{}) bool {
	select {
	case sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func FindCoverArtFiles(ctx context.Context, song Song, artistFilter string) []string {
	var searchTerms []string
	seenTerms := make(map[string]bool)

	addTerm := func(t string) {
		tClean := strings.TrimSpace(t)
		tLower := strings.ToLower(tClean)
		if tClean != "" && !seenTerms[tLower] {
			seenTerms[tLower] = true
			searchTerms = append(searchTerms, tClean)
		}
	}

	rawName := song.GetName()
	cleanName := strings.TrimSpace(versionRegex.ReplaceAllString(rawName, ""))
	if cleanName != "" {
		addTerm(cleanName)
	}
	if rawName != "" && rawName != cleanName {
		addTerm(rawName)
	}

	seenPaths := make(map[string]bool)
	var coverURLs []string

	for _, term := range searchTerms {
		browseItems, err := BrowseJuiceWRLDFiles(ctx, term, "Cover Arts")
		if err != nil {
			continue
		}

		for _, item := range browseItems {
			path := item.Path
			if item.Type != "file" || seenPaths[path] || !strings.Contains(path, "Cover Arts") {
				continue
			}
			if !IsEmbeddableImageFormat(path) {
				continue
			}

			if artistFilter != "" {
				lowerPath := strings.ToLower(path)
				lowerName := strings.ToLower(item.Name)
				lowerFilter := strings.ToLower(artistFilter)
				if !strings.Contains(lowerPath, lowerFilter) && !strings.Contains(lowerName, lowerFilter) {
					continue
				}
			}

			if !isCoverArtMatch(item.Name, song) {
				continue
			}

			seenPaths[path] = true
			coverURLs = append(coverURLs, GetDownloadURL(path))
		}
	}

	return coverURLs
}

type originalFileCandidate struct {
	label string
	url   string
	path  string
	name  string
	score int
}

func FindSongFiles(ctx context.Context, song Song) ([2]string, [][2]string, int) {
	var taggedItem [2]string
	if song.Path != "" && !hasArchiveExt(song.Path) && !strings.Contains(strings.ToLower(song.Path), "studio sessions") {
		label := GetFileButtonLabel(song.Path, false)
		taggedItem = [2]string{label, GetDownloadURL(song.Path)}
	}

	fnList := parseFileNames(song.FileNames)
	useFileNamesOnly := len(fnList) > 0
	manifest := ParseAvailableFiles(song.Bitrate)

	if !useFileNamesOnly && !manifest.HasOriginalFiles {
		return taggedItem, nil, 0
	}

	searchTerms := deriveCandidateSearchTerms(song, fnList, useFileNamesOnly)
	sessionFolder := extractSessionFolder(song.Path)

	rawCandidates := queryOriginalFileCandidates(ctx, searchTerms, sessionFolder, useFileNamesOnly)
	totalCandidates := len(rawCandidates)

	ogItems := filterCandidatesAgainstManifest(rawCandidates, manifest, song.Bitrate)
	return taggedItem, ogItems, totalCandidates
}

func deriveCandidateSearchTerms(song Song, fnList []string, useFileNamesOnly bool) []string {
	if useFileNamesOnly {
		return fnList
	}
	var searchTerms []string
	songName := song.GetName()
	baseName := strings.TrimSpace(versionRegex.ReplaceAllString(songName, ""))
	if baseName != "" {
		searchTerms = append(searchTerms, baseName)
		cleanBase := strings.TrimSpace(specialPunctRegex.ReplaceAllString(baseName, ""))
		if cleanBase != "" && cleanBase != baseName {
			searchTerms = append(searchTerms, cleanBase)
		}
	}
	return searchTerms
}

func extractSessionFolder(path string) string {
	if path == "" {
		return ""
	}
	normPath := strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(normPath, "/")
	if len(parts) >= 2 {
		return strings.ToLower(parts[len(parts)-2])
	}
	return ""
}

func queryOriginalFileCandidates(ctx context.Context, terms []string, sessionFolder string, useFileNamesOnly bool) []originalFileCandidate {
	termSet := make(map[string]bool)
	for _, t := range terms {
		if t != "" {
			termSet[t] = true
		}
	}

	ogDirs := []string{"Original Files", "Compilation/Original Files"}
	sem := make(chan struct{}, maxConcurrentRequests)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seenPaths := make(map[string]bool)
	seenFileNames := make(map[string]bool)
	var rawCandidates []originalFileCandidate

outer:
	for term := range termSet {
		for _, dirPath := range ogDirs {
			if !acquireSem(ctx, sem) {
				break outer
			}
			t := term
			dir := dirPath
			wg.Add(1)
			helpers.Spawn(func() {
				defer wg.Done()
				defer func() { <-sem }()

				items, err := BrowseJuiceWRLDFiles(ctx, t, dir)
				if err != nil || len(items) == 0 {
					return
				}

				mu.Lock()
				for _, item := range items {
					path := item.Path
					nameLower := strings.ToLower(item.Name)
					if path == "" || item.Type != "file" || seenPaths[path] || seenFileNames[nameLower] {
						continue
					}
					if !strings.Contains(path, "Original Files") {
						continue
					}

					if !isFilenameMatch(item.Name, t, useFileNamesOnly) {
						continue
					}

					seenPaths[path] = true
					seenFileNames[nameLower] = true
					label := GetFileButtonLabel(path, true)

					score := 0
					nameNoExt := strings.TrimSpace(strings.TrimSuffix(item.Name, filepath.Ext(item.Name)))
					if strings.EqualFold(nameNoExt, t) {
						score += 100
					}
					if sessionFolder != "" && strings.Contains(strings.ToLower(path), sessionFolder) {
						score += 50
					}

					rawCandidates = append(rawCandidates, originalFileCandidate{
						label: label,
						url:   GetDownloadURL(path),
						path:  path,
						name:  item.Name,
						score: score,
					})
				}
				mu.Unlock()
			})
		}
	}

	wg.Wait()
	return rawCandidates
}

func filterCandidatesAgainstManifest(rawCandidates []originalFileCandidate, manifest AvailableFiles, bitrate string) [][2]string {
	sort.SliceStable(rawCandidates, func(i, j int) bool {
		return rawCandidates[i].score > rawCandidates[j].score
	})

	var ogItems [][2]string
	if manifest.HasLRWav {
		var lWav, rWav *originalFileCandidate
		var mp3Cand *originalFileCandidate

		for i := range rawCandidates {
			c := &rawCandidates[i]
			nameLower := strings.ToLower(c.name)
			if strings.HasSuffix(nameLower, ".l.wav") || strings.Contains(nameLower, ".l.wav") {
				if lWav == nil {
					lWav = c
				}
			} else if strings.HasSuffix(nameLower, ".r.wav") || strings.Contains(nameLower, ".r.wav") {
				if rWav == nil {
					rWav = c
				}
			} else if strings.HasSuffix(nameLower, ".mp3") {
				if mp3Cand == nil {
					mp3Cand = c
				}
			}
		}

		if lWav != nil {
			ogItems = append(ogItems, [2]string{lWav.label, lWav.url})
		}
		if manifest.HasMp3 && mp3Cand != nil {
			ogItems = append(ogItems, [2]string{mp3Cand.label, mp3Cand.url})
		}
		if rWav != nil {
			ogItems = append(ogItems, [2]string{rWav.label, rWav.url})
		}

		if len(ogItems) == 0 {
			for _, c := range rawCandidates {
				ogItems = append(ogItems, [2]string{c.label, c.url})
			}
		}
	} else {
		for _, c := range rawCandidates {
			cExt := strings.ToLower(filepath.Ext(c.name))
			if cExt == ".wav" && !manifest.HasWav {
				continue
			}
			if cExt == ".mp3" && !manifest.HasMp3 && manifest.HasWav {
				continue
			}
			ogItems = append(ogItems, [2]string{c.label, c.url})
		}

		lowerBitrate := strings.ToLower(bitrate)
		if strings.Contains(lowerBitrate, "original file") && !strings.Contains(lowerBitrate, "original files") && len(ogItems) > 1 {
			ogItems = ogItems[:1]
		}
	}

	return ogItems
}

func FindSessionFiles(ctx context.Context, song Song) ([][2]string, [][2]string) {
	var primaryTerms []string
	var secondaryTerms []string
	seenNormalized := make(map[string]bool)

	addTerm := func(t string, isPrimary bool) {
		t = strings.TrimSpace(t)
		if t == "" {
			return
		}
		norm := normalizeForMatch(t)
		if norm == "" || seenNormalized[norm] {
			return
		}
		seenNormalized[norm] = true
		if isPrimary {
			primaryTerms = append(primaryTerms, t)
		} else {
			secondaryTerms = append(secondaryTerms, t)
		}
	}

	if n := song.GetName(); n != "" {
		addTerm(n, true)
	}
	for i, tt := range song.TrackTitles {
		addTerm(tt, i == 0 && len(primaryTerms) == 0)
	}
	for _, fn := range parseFileNames(song.FileNames) {
		addTerm(fn, false)
	}
	for _, st := range parseSessionTitles(song.SessionTitles) {
		addTerm(st, false)
	}
	for _, a := range song.AltNames {
		addTerm(a, false)
	}

	sem := make(chan struct{}, maxConcurrentRequests)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seenPaths := make(map[string]bool)
	seenFileNames := make(map[string]bool)
	var sessionDownloads [][2]string
	var sessionEdits [][2]string

	queryTerms := func(terms []string) {
		for _, term := range terms {
			if !acquireSem(ctx, sem) {
				break
			}
			targetTerm := term
			wg.Add(1)
			helpers.Spawn(func() {
				defer wg.Done()
				defer func() { <-sem }()

				items, err := BrowseJuiceWRLDFiles(ctx, targetTerm, "")
				if err != nil {
					return
				}

				mu.Lock()
				for _, item := range items {
					path := item.Path
					nameLower := strings.ToLower(item.Name)
					if path == "" || item.Type != "file" || seenPaths[path] || seenFileNames[nameLower] {
						continue
					}

					if isSessionEditPath(path) {
						if !helpers.IsAudioExtension(filepath.Ext(item.Name)) || !isSessionFileMatch(item.Name, song) {
							continue
						}
						seenPaths[path] = true
						seenFileNames[nameLower] = true
						label := GetCleanExtLabel(path)
						dlURL := GetDownloadURL(path)
						sessionEdits = append(sessionEdits, [2]string{label, dlURL})
					} else if hasArchiveExt(path) {
						if !hasSessionEvidence(path) || !isSessionFileMatch(item.Name, song) {
							continue
						}
						seenPaths[path] = true
						seenFileNames[nameLower] = true
						label := GetSessionArchiveLabel(path)
						dlURL := GetDownloadURL(path)
						sessionDownloads = append(sessionDownloads, [2]string{label, dlURL})
					}
				}
				mu.Unlock()
			})
		}
		wg.Wait()
	}

	queryTerms(primaryTerms)
	if len(sessionDownloads) == 0 && len(sessionEdits) == 0 && len(secondaryTerms) > 0 && ctx.Err() == nil {
		queryTerms(secondaryTerms)
	}

	return sessionDownloads, sessionEdits
}

func GetSessionArchiveLabel(path string) string {
	ext := GetCleanExtLabel(path)
	lower := strings.ToLower(path)
	if strings.Contains(lower, "protools") || strings.Contains(lower, "pro tools") {
		return fmt.Sprintf("ProTools %s", ext)
	}
	if strings.Contains(lower, "stems") || strings.Contains(lower, "multi-track") {
		return fmt.Sprintf("Stems %s", ext)
	}
	if strings.Contains(lower, "logic") {
		return fmt.Sprintf("Logic %s", ext)
	}
	if strings.Contains(lower, "ableton") {
		return fmt.Sprintf("Ableton %s", ext)
	}
	if strings.Contains(lower, "fl studio") || strings.Contains(lower, "flstudio") {
		return fmt.Sprintf("FL Studio %s", ext)
	}
	return ext
}

func cleanTitleForSessionMatch(raw string) string {
	raw = strings.TrimSpace(raw)
	ext := filepath.Ext(raw)
	raw = strings.TrimSuffix(raw, ext)
	raw = channelSuffixRegex.ReplaceAllString(raw, "")
	raw = trackPrefixRegex.ReplaceAllString(raw, "")
	raw = artistPrefixRegex.ReplaceAllString(raw, "")
	for bracketBlockRegex.MatchString(raw) {
		raw = bracketBlockRegex.ReplaceAllString(raw, " ")
	}
	raw = versionSuffixRegex.ReplaceAllString(raw, "")
	return normalizeForMatch(raw)
}

func hasSessionEvidence(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "studio session") || strings.Contains(lower, "studio sessions") ||
		strings.Contains(lower, "/session") || strings.Contains(lower, "\\session") ||
		strings.Contains(lower, "sessions/") || strings.Contains(lower, "sessions\\") ||
		strings.Contains(lower, "(session") || strings.Contains(lower, "[session") ||
		strings.Contains(lower, "protools") || strings.Contains(lower, "pro tools") ||
		strings.Contains(lower, "stems") || strings.Contains(lower, "multitrack")
}

func isSessionFileMatch(itemFileName string, song Song) bool {
	candClean := cleanTitleForSessionMatch(itemFileName)
	if candClean == "" {
		return false
	}

	itemBase := strings.TrimSpace(itemFileName)
	ext := filepath.Ext(itemBase)
	nameNoExt := strings.TrimSpace(strings.TrimSuffix(itemBase, ext))

	var validTitles []string
	if name := strings.TrimSpace(song.GetName()); name != "" {
		validTitles = append(validTitles, name)
	}
	for _, t := range song.TrackTitles {
		if tClean := strings.TrimSpace(t); tClean != "" {
			validTitles = append(validTitles, tClean)
		}
	}
	for _, a := range song.AltNames {
		if aClean := strings.TrimSpace(a); aClean != "" {
			validTitles = append(validTitles, aClean)
		}
	}
	for _, st := range parseSessionTitles(song.SessionTitles) {
		if stClean := strings.TrimSpace(st); stClean != "" {
			validTitles = append(validTitles, stClean)
		}
	}
	for _, fn := range parseFileNames(song.FileNames) {
		if fnClean := strings.TrimSpace(fn); fnClean != "" {
			validTitles = append(validTitles, fnClean)
		}
	}

	for _, t := range validTitles {
		tNoExt := strings.TrimSpace(strings.TrimSuffix(t, filepath.Ext(t)))
		if strings.EqualFold(nameNoExt, t) || strings.EqualFold(nameNoExt, tNoExt) {
			return true
		}

		targetClean := cleanTitleForSessionMatch(t)
		if targetClean == "" {
			continue
		}

		if candClean == targetClean {
			return true
		}

		if strings.HasPrefix(candClean, targetClean+" + ") ||
			strings.HasPrefix(candClean, targetClean+" and ") ||
			strings.HasPrefix(candClean, targetClean+" / ") ||
			strings.HasPrefix(candClean, targetClean+" x ") {
			return true
		}
		if strings.HasSuffix(candClean, " + "+targetClean) ||
			strings.HasSuffix(candClean, " and "+targetClean) ||
			strings.HasSuffix(candClean, " / "+targetClean) ||
			strings.HasSuffix(candClean, " x "+targetClean) {
			return true
		}
	}

	return false
}

func isSessionEditPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "session edits/") || strings.Contains(lower, "session edit/") ||
		strings.Contains(lower, "session edits\\") || strings.Contains(lower, "session edit\\") ||
		strings.Contains(lower, "[session edit]") || strings.Contains(lower, "(session edit)") ||
		strings.HasPrefix(lower, "session edits/") || strings.HasPrefix(lower, "session edit/")
}

func FindSessionEditItems(ctx context.Context, song Song) []SessionEditItem {
	var primaryTerms []string
	var secondaryTerms []string
	seenNormalized := make(map[string]bool)

	addTerm := func(t string, isPrimary bool) {
		t = strings.TrimSpace(t)
		if t == "" {
			return
		}
		norm := normalizeForMatch(t)
		if norm == "" || seenNormalized[norm] {
			return
		}
		seenNormalized[norm] = true
		if isPrimary {
			primaryTerms = append(primaryTerms, t)
		} else {
			secondaryTerms = append(secondaryTerms, t)
		}
	}

	if n := song.GetName(); n != "" {
		addTerm(n, true)
	}
	for i, tt := range song.TrackTitles {
		addTerm(tt, i == 0 && len(primaryTerms) == 0)
	}
	for _, fn := range parseFileNames(song.FileNames) {
		addTerm(fn, false)
	}
	for _, st := range parseSessionTitles(song.SessionTitles) {
		addTerm(st, false)
	}
	for _, a := range song.AltNames {
		addTerm(a, false)
	}

	sem := make(chan struct{}, maxConcurrentRequests)
	var wg sync.WaitGroup
	var mu sync.Mutex
	seenPaths := make(map[string]bool)
	seenFileNames := make(map[string]bool)
	var edits []SessionEditItem

	queryTerms := func(terms []string) {
		for _, term := range terms {
			if !acquireSem(ctx, sem) {
				break
			}
			targetTerm := term
			wg.Add(1)
			helpers.Spawn(func() {
				defer wg.Done()
				defer func() { <-sem }()

				items, err := BrowseJuiceWRLDFiles(ctx, targetTerm, "")
				if err != nil {
					return
				}

				mu.Lock()
				for _, item := range items {
					path := item.Path
					nameLower := strings.ToLower(item.Name)
					if path == "" || item.Type != "file" || seenPaths[path] || seenFileNames[nameLower] {
						continue
					}
					if isSessionEditPath(path) {
						if !helpers.IsAudioExtension(filepath.Ext(item.Name)) || !isSessionFileMatch(item.Name, song) {
							continue
						}
						seenPaths[path] = true
						seenFileNames[nameLower] = true
						label := GetCleanExtLabel(path)
						dlURL := GetDownloadURL(path)
						edits = append(edits, SessionEditItem{
							Name: item.Name,
							Path: path,
							URL:  dlURL,
							Ext:  label,
						})
					}
				}
				mu.Unlock()
			})
		}
		wg.Wait()
	}

	queryTerms(primaryTerms)
	if len(edits) == 0 && len(secondaryTerms) > 0 && ctx.Err() == nil {
		queryTerms(secondaryTerms)
	}

	return edits
}

func FormatSessionEditOption(song Song, edit SessionEditItem, index int) (string, string) {
	rawName := edit.Name
	ext := filepath.Ext(rawName)
	cleanName := strings.TrimSuffix(rawName, ext)
	cleanName = strings.TrimSpace(cleanName)

	label := fmt.Sprintf("%d. %s", index+1, cleanName)
	label = helpers.TruncateString(label, 100)

	return label, ""
}

// GetSongInstrumentalDirs returns folder paths under Instrumentals for the song era.
func GetSongInstrumentalDirs(song Song) []string {
	var dirs []string

	if song.Era != nil {
		switch song.Era.ID {
		case 101:
			dirs = append(dirs, "Instrumentals/1. JUICED UP THE EP")
		case 104:
			dirs = append(dirs, "Instrumentals/3. Heartbroken In Hollywood 999")
		case 105:
			dirs = append(dirs, "Instrumentals/4. JuiceWRLD 9 9 9")
		case 103:
			dirs = append(dirs, "Instrumentals/5. Afflictions")
		case 106:
			dirs = append(dirs, "Instrumentals/6. BINGEDRINKINGMUSIC")
		case 107:
			dirs = append(dirs, "Instrumentals/7. NOTHINGS DIFFERENT 3")
		case 108:
			dirs = append(dirs, "Instrumentals/9. Goodbye & Good Riddance", "Instrumentals/Others/Goodbye & Good Riddance (Anniversary Edition)")
		case 109:
			dirs = append(dirs, "Instrumentals/10. WRLD ON DRUGS")
		case 110:
			dirs = append(dirs, "Instrumentals/11. Death Race For Love", "Instrumentals/Others/Death Race For Love (TV Mix)")
		case 111:
			dirs = append(dirs, "Instrumentals/12. Outsiders")
		case 102, 119:
			dirs = append(dirs, "Instrumentals/13. Legends Never Die", "Instrumentals/Others/Legends Never Die (Extended Outro Versions)")
		case 115, 116:
			dirs = append(dirs, "Instrumentals/14. Fighting Demons", "Instrumentals/Others/Fighting Demons (Digital Deluxe Edition)", "Instrumentals/Others/Fighting Demons (Complete Edition)")
		case 113, 114:
			dirs = append(dirs, "Instrumentals/15. The Pre-Party")
		case 117, 118:
			dirs = append(dirs, "Instrumentals/16. The Party Never Ends")
		case 112:
			dirs = append(dirs, "Instrumentals/17. Posthumous")
		}
	}

	if len(dirs) > 0 {
		return dirs
	}

	var textBuilder strings.Builder
	if song.Era != nil {
		textBuilder.WriteString(song.Era.Name)
		textBuilder.WriteString(" ")
		textBuilder.WriteString(song.Era.Description)
		textBuilder.WriteString(" ")
	}
	textBuilder.WriteString(song.Path)
	text := strings.ToLower(textBuilder.String())

	eraName := ""
	if song.Era != nil {
		eraName = strings.ToLower(strings.TrimSpace(song.Era.Name))
	}

	switch {
	case strings.Contains(text, "juiced up") || eraName == "jute":
		dirs = append(dirs, "Instrumentals/1. JUICED UP THE EP")
	case strings.Contains(text, "heartbroken in hollywood") || eraName == "hih 999" || eraName == "hih":
		dirs = append(dirs, "Instrumentals/3. Heartbroken In Hollywood 999")
	case strings.Contains(text, "juicewrld 9 9 9") || strings.Contains(text, "juice wrld 999") || eraName == "jw 999" || eraName == "jw":
		dirs = append(dirs, "Instrumentals/4. JuiceWRLD 9 9 9")
	case strings.Contains(text, "affliction") || eraName == "afflictions":
		dirs = append(dirs, "Instrumentals/5. Afflictions")
	case strings.Contains(text, "bingedrinkingmusic") || eraName == "bdm":
		dirs = append(dirs, "Instrumentals/6. BINGEDRINKINGMUSIC")
	case strings.Contains(text, "nothings different") || strings.Contains(text, "nothing's different") || eraName == "nd":
		dirs = append(dirs, "Instrumentals/7. NOTHINGS DIFFERENT 3")
	case strings.Contains(text, "kill's wrld") || strings.Contains(text, "kills wrld"):
		dirs = append(dirs, "Instrumentals/8. Kill's WRLD")
	case strings.Contains(text, "goodbye & good riddance") || strings.Contains(text, "goodbye and good riddance") || eraName == "gb&gr" || eraName == "gbgr":
		dirs = append(dirs, "Instrumentals/9. Goodbye & Good Riddance", "Instrumentals/Others/Goodbye & Good Riddance (Anniversary Edition)")
	case strings.Contains(text, "wrld on drugs") || strings.Contains(text, "world on drugs") || eraName == "wod":
		dirs = append(dirs, "Instrumentals/10. WRLD ON DRUGS")
	case strings.Contains(text, "death race for love") || eraName == "drfl":
		dirs = append(dirs, "Instrumentals/11. Death Race For Love", "Instrumentals/Others/Death Race For Love (TV Mix)")
	case strings.Contains(text, "outsiders") || eraName == "out":
		dirs = append(dirs, "Instrumentals/12. Outsiders")
	case strings.Contains(text, "legends never die") || eraName == "lnd":
		dirs = append(dirs, "Instrumentals/13. Legends Never Die", "Instrumentals/Others/Legends Never Die (Extended Outro Versions)")
	case strings.Contains(text, "fighting demons") || eraName == "fd":
		dirs = append(dirs, "Instrumentals/14. Fighting Demons", "Instrumentals/Others/Fighting Demons (Digital Deluxe Edition)", "Instrumentals/Others/Fighting Demons (Complete Edition)")
	case strings.Contains(text, "the pre-party") || strings.Contains(text, "pre party") || eraName == "tpp":
		dirs = append(dirs, "Instrumentals/15. The Pre-Party")
	case strings.Contains(text, "the party never ends") || eraName == "tpne":
		dirs = append(dirs, "Instrumentals/16. The Party Never Ends")
	case strings.Contains(text, "posthumous") || eraName == "post":
		dirs = append(dirs, "Instrumentals/17. Posthumous")
	default:
		dirs = append(dirs, "Instrumentals")
	}

	return dirs
}

// FindInstrumentalFiles searches the era directory for audio files matching the song.
func FindInstrumentalFiles(ctx context.Context, song Song) [][2]string {
	if ctx == nil {
		ctx = context.Background()
	}

	eraDirs := GetSongInstrumentalDirs(song)
	knownInstrumentals := extractKnownInstrumentals(song)

	var primaryTerms []string
	var secondaryTerms []string
	seenTerms := make(map[string]bool)

	addTerm := func(t string, isPrimary bool) {
		tClean := strings.TrimSpace(t)
		if tClean == "" || strings.EqualFold(tClean, "n/a") {
			return
		}
		norm := normalizeForMatch(cleanInstrumentalTerm(tClean))
		if norm == "" || seenTerms[norm] {
			return
		}
		seenTerms[norm] = true
		if isPrimary {
			primaryTerms = append(primaryTerms, tClean)
		} else {
			secondaryTerms = append(secondaryTerms, tClean)
		}
	}

	if n := song.GetName(); n != "" {
		addTerm(n, true)
		baseName := strings.TrimSpace(versionRegex.ReplaceAllString(n, ""))
		if baseName != "" {
			addTerm(baseName, true)
			cleanBase := strings.TrimSpace(specialPunctRegex.ReplaceAllString(baseName, ""))
			if cleanBase != "" {
				addTerm(cleanBase, true)
			}
		}
	}
	if song.Title != "" && song.Title != song.GetName() {
		addTerm(song.Title, true)
	}

	for _, tt := range song.TrackTitles {
		addTerm(tt, false)
	}
	for _, ki := range knownInstrumentals {
		addTerm(ki, false)
	}
	for _, a := range song.AltNames {
		addTerm(a, false)
	}

	queryDirs := func(terms []string) [][2]string {
		sem := make(chan struct{}, maxConcurrentRequests)
		var wg sync.WaitGroup
		var mu sync.Mutex
		seenPaths := make(map[string]bool)
		seenNames := make(map[string]bool)
		var foundItems [][2]string

		for _, term := range terms {
			for _, eraDir := range eraDirs {
				if !acquireSem(ctx, sem) {
					break
				}
				t := term
				d := eraDir
				wg.Add(1)
				helpers.Spawn(func() {
					defer wg.Done()
					defer func() { <-sem }()

					browseItems, err := BrowseJuiceWRLDFiles(ctx, t, d)
					if err != nil || len(browseItems) == 0 {
						return
					}

					for _, item := range browseItems {
						if item.Type == "file" {
							if !helpers.IsAudioExtension(filepath.Ext(item.Name)) {
								continue
							}
							parentDir := filepath.Base(filepath.Dir(item.Path))
							if !isInstrumentalMatch(item.Name, parentDir, song, t) {
								continue
							}
							mu.Lock()
							if !seenPaths[item.Path] {
								seenPaths[item.Path] = true
								nameLower := strings.ToLower(item.Name)
								if !seenNames[nameLower] {
									seenNames[nameLower] = true
									label := GetInstrumentalButtonLabel(item.Path)
									foundItems = append(foundItems, [2]string{label, GetDownloadURL(item.Path)})
								}
							}
							mu.Unlock()
						} else if item.Type == "directory" {
							if !isInstrumentalDirMatch(item.Name, song, t, knownInstrumentals) {
								continue
							}
							dirFiles, errDir := BrowseJuiceWRLDFiles(ctx, "", item.Path)
							if errDir != nil || len(dirFiles) == 0 {
								continue
							}

							var audioFiles []BrowseItem
							for _, f := range dirFiles {
								if f.Type == "file" && helpers.IsAudioExtension(filepath.Ext(f.Name)) {
									audioFiles = append(audioFiles, f)
								}
							}
							if len(audioFiles) == 0 {
								continue
							}

							var targetFiles []BrowseItem
							if len(audioFiles) == 1 {
								targetFiles = audioFiles
							} else {
								for _, f := range audioFiles {
									if isInstrumentalAudioMatch(f.Name, item.Name, song, knownInstrumentals) {
										targetFiles = append(targetFiles, f)
									}
								}
								if len(targetFiles) == 0 {
									targetFiles = audioFiles
								}
							}

							mu.Lock()
							for _, f := range targetFiles {
								if !seenPaths[f.Path] {
									seenPaths[f.Path] = true
									nameLower := strings.ToLower(f.Name)
									if !seenNames[nameLower] {
										seenNames[nameLower] = true
										label := GetInstrumentalButtonLabel(f.Path)
										foundItems = append(foundItems, [2]string{label, GetDownloadURL(f.Path)})
									}
								}
							}
							mu.Unlock()
						}
					}
				})
			}
		}
		wg.Wait()
		return foundItems
	}

	items := queryDirs(primaryTerms)
	if len(items) == 0 && len(secondaryTerms) > 0 && ctx.Err() == nil {
		items = queryDirs(secondaryTerms)
	}

	return deduplicateLabels(items)
}

func cleanInstrumentalTerm(s string) string {
	s = strings.TrimSpace(s)
	s = instrumentalPrefixRegex.ReplaceAllString(s, "")
	s = trackPrefixRegex.ReplaceAllString(s, "")
	s = strings.Trim(s, "!-_ ")
	return strings.TrimSpace(s)
}

func GetInstrumentalButtonLabel(path string) string {
	ext := GetCleanExtLabel(path)
	parent := filepath.Base(filepath.Dir(path))
	lower := strings.ToLower(parent)
	if idx := strings.Index(lower, "(v"); idx != -1 {
		end := strings.Index(lower[idx:], ")")
		if end != -1 {
			tag := parent[idx+1 : idx+end]
			return fmt.Sprintf("%s %s", strings.ToUpper(tag), ext)
		}
	}
	if strings.Contains(lower, "tv mix") {
		return fmt.Sprintf("TV Mix %s", ext)
	}
	if strings.Contains(lower, "remix") {
		return fmt.Sprintf("Remix %s", ext)
	}
	return ext
}

func deduplicateLabels(items [][2]string) [][2]string {
	if len(items) <= 1 {
		return items
	}
	counts := make(map[string]int)
	for _, it := range items {
		counts[it[0]]++
	}
	hasDup := false
	for _, c := range counts {
		if c > 1 {
			hasDup = true
			break
		}
	}
	if !hasDup {
		return items
	}
	seen := make(map[string]int)
	res := make([][2]string, len(items))
	for i, it := range items {
		lbl := it[0]
		if counts[lbl] > 1 {
			seen[lbl]++
			res[i] = [2]string{fmt.Sprintf("%s (%d)", lbl, seen[lbl]), it[1]}
		} else {
			res[i] = it
		}
	}
	return res
}

func extractKnownInstrumentals(song Song) []string {
	var known []string
	addKnown := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.EqualFold(raw, "n/a") {
			return
		}
		for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || skipInstrumentalPrefixRegex.MatchString(line) {
				continue
			}
			clean := cleanInstrumentalTerm(line)
			if clean != "" && !strings.EqualFold(clean, "n/a") {
				known = append(known, clean)
			}
		}
	}

	addKnown(song.Instrumentals)
	addKnown(song.InstrumentalNames)

	for _, fn := range parseFileNames(song.FileNames) {
		clean := cleanInstrumentalTerm(fn)
		if clean != "" {
			known = append(known, clean)
			matches := parenRegex.FindAllString(fn, -1)
			for _, m := range matches {
				inner := strings.Trim(m, "()")
				cleanInner := cleanInstrumentalTerm(inner)
				if cleanInner != "" && !strings.EqualFold(cleanInner, "n/a") {
					known = append(known, cleanInner)
				}
			}
		}
	}
	return known
}

func containsPhrase(haystack, needle string) bool {
	if haystack == needle {
		return true
	}
	if len(needle) == 0 {
		return false
	}
	return strings.HasPrefix(haystack, needle+" ") ||
		strings.HasSuffix(haystack, " "+needle) ||
		strings.Contains(haystack, " "+needle+" ")
}

func matchesAnyInstrumental(name string, known []string) bool {
	base := strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	norm := normalizeForMatch(cleanInstrumentalTerm(base))
	if norm == "" {
		return false
	}
	for _, k := range known {
		kNorm := normalizeForMatch(cleanInstrumentalTerm(k))
		if kNorm == "" {
			continue
		}
		if containsPhrase(norm, kNorm) || containsPhrase(kNorm, norm) {
			return true
		}
	}
	return false
}

func isInstrumentalDirMatch(dirName string, song Song, term string, known []string) bool {
	if isSessionFileMatch(dirName, song) {
		return true
	}
	if matchesAnyInstrumental(dirName, known) {
		return true
	}
	tNorm := normalizeForMatch(cleanInstrumentalTerm(term))
	if tNorm != "" {
		dNorm := cleanTitleForSessionMatch(dirName)
		if dNorm == tNorm {
			return true
		}
		if len(tNorm) >= 4 && (strings.HasPrefix(dNorm, tNorm+" ") || strings.HasSuffix(dNorm, " "+tNorm)) {
			return true
		}
	}
	return false
}

func isInstrumentalMatch(fileName, parentDir string, song Song, term string) bool {
	known := extractKnownInstrumentals(song)

	if fileName == "" {
		return isInstrumentalDirMatch(parentDir, song, term, known)
	}

	if !isInstrumentalDirMatch(parentDir, song, term, known) {
		return false
	}

	return isInstrumentalAudioMatch(fileName, parentDir, song, known)
}

func cleanInstrumentalFileName(raw string) string {
	ext := filepath.Ext(raw)
	base := strings.TrimSuffix(raw, ext)
	base = instrumentalTagRegex.ReplaceAllString(base, " ")
	return strings.TrimSpace(base) + ext
}

func isInstrumentalAudioMatch(fileName, parentDir string, song Song, known []string) bool {
	cleanName := cleanInstrumentalFileName(fileName)
	if isSessionFileMatch(fileName, song) || isSessionFileMatch(cleanName, song) {
		return true
	}
	if matchesAnyInstrumental(fileName, known) || matchesAnyInstrumental(cleanName, known) {
		return true
	}

	parentMatches := isSessionFileMatch(parentDir, song) || matchesAnyInstrumental(parentDir, known)
	if parentMatches {
		nameNoExt := strings.TrimSpace(strings.TrimSuffix(fileName, filepath.Ext(fileName)))
		if genericInstrumentalRegex.MatchString(nameNoExt) {
			return true
		}
		if len(known) == 0 {
			return true
		}
	}
	return false
}
