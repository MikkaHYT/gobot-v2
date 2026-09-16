package juicewrld

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var contractionMap = map[string]string{
	"ill":      "i'll",
	"dont":     "don't",
	"cant":     "can't",
	"wont":     "won't",
	"im":       "i'm",
	"its":      "it's",
	"aint":     "ain't",
	"thats":    "that's",
	"whats":    "what's",
	"youre":    "you're",
	"theyre":   "they're",
	"weve":     "we've",
	"ive":      "i've",
	"youd":     "you'd",
	"hed":      "he'd",
	"shes":     "she's",
	"isnt":     "isn't",
	"havent":   "haven't",
	"hasnt":    "hasn't",
	"hadnt":    "hadn't",
	"couldnt":  "couldn't",
	"wouldnt":  "wouldn't",
	"shouldnt": "shouldn't",
	"didnt":    "didn't",
	"doesnt":   "doesn't",
}

func addContractionApostrophes(q string) string {
	words := strings.Fields(q)
	changed := false
	for i, w := range words {
		lower := strings.ToLower(w)
		if repl, ok := contractionMap[lower]; ok {
			words[i] = repl
			changed = true
		}
	}
	if !changed {
		return ""
	}
	return strings.Join(words, " ")
}

func FilterValidSongs(results []Song, commandName string) []Song {
	var validSongs []Song
	seen := make(map[string]bool)

	hasNonSession := make(map[string]bool)
	if commandName == "leak" || commandName == "songinfo" || commandName == "snip" {
		for _, s := range results {
			cat := strings.TrimSpace(s.Category)
			name := strings.ToLower(strings.TrimSpace(s.GetName()))
			if cat != "recording_session" && name != "" {
				hasNonSession[name] = true
			}
		}
	}

	for _, s := range results {
		path := strings.TrimSpace(s.Path)
		name := strings.ToLower(strings.TrimSpace(s.GetName()))
		bitrate := strings.TrimSpace(s.Bitrate)
		fileNames := strings.TrimSpace(s.FileNames)
		addInfo := strings.TrimSpace(s.AdditionalInformation)
		cat := strings.TrimSpace(s.Category)

		if (commandName == "leak" || commandName == "songinfo" || commandName == "snip") && cat == "recording_session" {
			if path == "" || hasNonSession[name] {
				continue
			}
		}

		hasSubstance := path != "" || bitrate != "" || addInfo != "" || strings.TrimSpace(s.Lyrics) != "" || strings.TrimSpace(s.SessionTitles) != "" || strings.TrimSpace(s.SessionTracking) != "" || strings.TrimSpace(s.Instrumentals) != "" || strings.TrimSpace(s.InstrumentalNames) != ""
		sig := fmt.Sprintf("%s|%s|%s|%s", name, path, fileNames, cat)

		if hasSubstance && !seen[sig] {
			seen[sig] = true
			validSongs = append(validSongs, s)
		}
	}

	return validSongs
}

func levenshteinRatio(s1, s2 string) float64 {
	r1, r2 := []rune(s1), []rune(s2)
	if len(r1) == 0 && len(r2) == 0 {
		return 1.0
	}
	if len(r1) == 0 || len(r2) == 0 {
		return 0.0
	}

	if len(r1) > len(r2) {
		r1, r2 = r2, r1
	}

	col := make([]int, len(r1)+1)
	for i := range col {
		col[i] = i
	}

	for j := 1; j <= len(r2); j++ {
		prevDiag := col[0]
		col[0] = j
		for i := 1; i <= len(r1); i++ {
			oldDiag := col[i]
			if r1[i-1] == r2[j-1] {
				col[i] = prevDiag
			} else {
				m := col[i] + 1
				if col[i-1]+1 < m {
					m = col[i-1] + 1
				}
				if prevDiag+1 < m {
					m = prevDiag + 1
				}
				col[i] = m
			}
			prevDiag = oldDiag
		}
	}

	return 1.0 - float64(col[len(r1)])/float64(len(r2))
}

var normPunctRegex = regexp.MustCompile(`[^a-z0-9\s]`)

func normalizeForMatch(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "’", "")
	s = strings.ReplaceAll(s, "&", "and")
	s = normPunctRegex.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func tokenSimilarity(query, target string) float64 {
	qNorm := normalizeForMatch(query)
	tNorm := normalizeForMatch(target)
	if qNorm == "" || tNorm == "" {
		return 0.0
	}
	if qNorm == tNorm {
		return 1.0
	}

	qTokens := strings.Fields(qNorm)
	tTokens := strings.Fields(tNorm)
	qLen := len(qTokens)
	tLen := len(tTokens)
	if qLen == 0 || tLen == 0 {
		return 0.0
	}

	bestQ := make([]float64, qLen)
	bestT := make([]float64, tLen)

	for i, qt := range qTokens {
		for j, tt := range tTokens {
			if qt == tt {
				bestQ[i] = 1.0
				bestT[j] = 1.0
				continue
			}
			if len(qt) > 2 && len(tt) > 2 {
				if ratio := levenshteinRatio(qt, tt); ratio >= 0.70 {
					if ratio > bestQ[i] {
						bestQ[i] = ratio
					}
					if ratio > bestT[j] {
						bestT[j] = ratio
					}
				}
			}
		}
	}

	var qScore, tScore float64
	for _, s := range bestQ {
		qScore += s
	}
	for _, s := range bestT {
		tScore += s
	}

	qCov := qScore / float64(qLen)
	tCov := tScore / float64(tLen)

	coverage := qCov
	if tCov > coverage {
		coverage = tCov
	}

	if strings.Contains(tNorm, qNorm) || strings.Contains(qNorm, tNorm) {
		coverage += 0.05
		if coverage > 1.0 {
			coverage = 1.0
		}
	}

	qRunes := float64(utf8.RuneCountInString(qNorm))
	tRunes := float64(utf8.RuneCountInString(tNorm))
	lengthRatio := qRunes / tRunes
	if lengthRatio > 1.0 {
		lengthRatio = 1.0 / lengthRatio
	}
	return coverage * (0.85 + 0.15*lengthRatio)
}

func matchScore(song Song, query string, commandName string) float64 {
	primaryScore := tokenSimilarity(query, song.GetName())
	bestScore := primaryScore

	for _, title := range song.TrackTitles {
		if s := tokenSimilarity(query, title); s > bestScore {
			bestScore = s
		}
	}

	for _, alt := range song.AltNames {
		if s := tokenSimilarity(query, alt); s > bestScore {
			bestScore = s
		}
	}

	if primaryScore == 1.0 {
		bestScore += 0.02
	}

	if commandName == "session" || commandName == "sessioninfo" {
		if song.Category == "recording_session" {
			bestScore += 0.05
		} else if song.Category != "unreleased" {
			bestScore -= 0.10
		}
	} else if commandName == "leak" {
		if song.Category == "unreleased" {
			bestScore += 0.05
		} else if song.Category == "recording_session" {
			bestScore -= 0.10
		} else {
			bestScore -= 0.05
		}
	} else if commandName == "songinfo" || commandName == "snip" {
		if song.Category == "recording_session" {
			bestScore -= 0.10
		}
	}
	return bestScore
}

type scoredSong struct {
	song  Song
	score float64
}

func SortBestMatch(results []Song, query string, commandName string) []Song {
	if len(results) <= 1 {
		return results
	}
	qClean := strings.TrimSpace(strings.ToLower(query))
	qNorm := normalizeForMatch(qClean)

	scored := make([]scoredSong, len(results))
	for i, s := range results {
		scored[i] = scoredSong{song: s, score: matchScore(s, qClean, commandName)}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		iExact := normalizeForMatch(scored[i].song.GetName()) == qNorm
		jExact := normalizeForMatch(scored[j].song.GetName()) == qNorm
		if iExact != jExact {
			return iExact
		}
		return len(scored[i].song.GetName()) < len(scored[j].song.GetName())
	})

	sorted := make([]Song, len(scored))
	for i, ss := range scored {
		sorted[i] = ss.song
	}
	return sorted
}

func isFilenameMatch(itemFileName string, term string, useFileNamesOnly bool) bool {
	itemBase := strings.TrimSpace(itemFileName)
	ext := filepath.Ext(itemBase)
	nameNoExt := strings.TrimSpace(strings.TrimSuffix(itemBase, ext))
	tClean := strings.TrimSpace(term)
	if tClean == "" {
		return false
	}
	tExt := filepath.Ext(tClean)
	tNoExt := strings.TrimSpace(strings.TrimSuffix(tClean, tExt))

	itemLower := strings.ToLower(itemFileName)
	nameNoExtLower := strings.ToLower(nameNoExt)
	tLower := strings.ToLower(tClean)
	tNoExtLower := strings.ToLower(tNoExt)

	if itemLower == tLower || nameNoExtLower == tNoExtLower || nameNoExtLower == tLower {
		return true
	}

	if useFileNamesOnly {
		if strings.HasPrefix(itemLower, tNoExtLower+".l.") || strings.HasPrefix(itemLower, tNoExtLower+".r.") ||
			strings.HasPrefix(itemLower, tNoExtLower+"_l.") || strings.HasPrefix(itemLower, tNoExtLower+"_r.") ||
			strings.HasPrefix(itemLower, tNoExtLower+".l.wav") || strings.HasPrefix(itemLower, tNoExtLower+".r.wav") ||
			nameNoExtLower == tNoExtLower+".l" || nameNoExtLower == tNoExtLower+".r" ||
			nameNoExtLower == tNoExtLower+"_l" || nameNoExtLower == tNoExtLower+"_r" {
			return true
		}
		if strings.HasPrefix(nameNoExtLower, tNoExtLower) {
			rest := nameNoExtLower[len(tNoExtLower):]
			if len(rest) > 0 {
				firstChar := rest[0]
				if firstChar == ' ' || firstChar == '-' || firstChar == '_' || firstChar == '(' || firstChar == '[' || firstChar == '.' {
					return true
				}
			}
		}
		return false
	}

	return hasWordBoundary(nameNoExtLower, tNoExtLower) || hasWordBoundary(nameNoExtLower, tLower)
}

func isCoverArtMatch(itemFileName string, song Song) bool {
	itemBase := strings.TrimSpace(itemFileName)
	ext := filepath.Ext(itemBase)
	nameNoExt := strings.TrimSpace(strings.TrimSuffix(itemBase, ext))
	nameNoExtLower := strings.ToLower(nameNoExt)

	rawName := strings.TrimSpace(song.GetName())
	cleanName := strings.TrimSpace(versionRegex.ReplaceAllString(rawName, ""))
	var validTitles []string
	if cleanName != "" {
		validTitles = append(validTitles, cleanName)
	}
	if rawName != "" && rawName != cleanName {
		validTitles = append(validTitles, rawName)
	}

	for _, t := range validTitles {
		tLower := strings.ToLower(t)
		if tLower == "" {
			continue
		}

		if nameNoExtLower == tLower {
			return true
		}

		if strings.HasPrefix(nameNoExtLower, tLower) {
			rest := nameNoExtLower[len(tLower):]
			if len(rest) > 0 {
				c := rest[0]
				if c == ' ' || c == '-' || c == '_' || c == '(' || c == '[' || c == '.' {
					return true
				}
			}
		}

		if strings.Contains(nameNoExtLower, "("+tLower+")") ||
			strings.Contains(nameNoExtLower, "["+tLower+"]") {
			return true
		}
	}

	return false
}

func hasWordBoundary(s, term string) bool {
	if term == "" || len(s) < len(term) {
		return false
	}
	idx := 0
	for {
		i := strings.Index(s[idx:], term)
		if i == -1 {
			return false
		}
		matchStart := idx + i
		matchEnd := matchStart + len(term)

		leftOK := matchStart == 0 || !isWordByte(s[matchStart-1])
		rightOK := matchEnd == len(s) || !isWordByte(s[matchEnd])

		if leftOK && rightOK {
			return true
		}
		idx = matchStart + 1
		if idx >= len(s) {
			return false
		}
	}
}

func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
