package helpers

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

const (
	Day   = 24 * time.Hour
	Week  = 7 * Day
	Month = 30 * Day
	Year  = 365 * Day
)

// ParseDuration parses duration strings with support for s, m, h, d, w, mo, y (return: 1d12h).
func ParseDuration(str string) (time.Duration, error) {
	str = strings.TrimSpace(strings.ToLower(str))
	if str == "" {
		return 0, fmt.Errorf("empty duration string")
	}
	if d, err := time.ParseDuration(str); err == nil {
		if d <= 0 {
			return 0, fmt.Errorf("duration must be greater than zero")
		}
		return d, nil
	}
	var total time.Duration
	var currentNum strings.Builder
	var currentUnit strings.Builder
	for _, r := range str {
		if r >= '0' && r <= '9' {
			if currentUnit.Len() > 0 {
				unitStr := currentUnit.String()
				numVal, err := strconv.Atoi(currentNum.String())
				if err != nil {
					return 0, fmt.Errorf("invalid number in duration: %s", currentNum.String())
				}
				unitDur, err := unitToDuration(unitStr)
				if err != nil {
					return 0, err
				}
				total, err = addCheckedDuration(total, numVal, unitDur)
				if err != nil {
					return 0, err
				}
				currentNum.Reset()
				currentUnit.Reset()
			}
			currentNum.WriteRune(r)
		} else {
			if currentNum.Len() == 0 {
				return 0, fmt.Errorf("invalid duration format: %s", str)
			}
			currentUnit.WriteRune(r)
		}
	}
	if currentNum.Len() > 0 && currentUnit.Len() > 0 {
		numVal, err := strconv.Atoi(currentNum.String())
		if err != nil {
			return 0, fmt.Errorf("invalid number in duration: %s", currentNum.String())
		}
		unitDur, err := unitToDuration(currentUnit.String())
		if err != nil {
			return 0, err
		}
		total, err = addCheckedDuration(total, numVal, unitDur)
		if err != nil {
			return 0, err
		}
	} else {
		return 0, fmt.Errorf("invalid duration format: %s", str)
	}
	if total <= 0 {
		return 0, fmt.Errorf("duration must be greater than zero")
	}
	return total, nil
}

func addCheckedDuration(total time.Duration, numVal int, unitDur time.Duration) (time.Duration, error) {
	if numVal < 0 {
		return 0, fmt.Errorf("negative duration unit")
	}
	if numVal > 0 && unitDur > math.MaxInt64/time.Duration(numVal) {
		return 0, fmt.Errorf("duration value exceeds maximum representable span")
	}
	segment := time.Duration(numVal) * unitDur
	if segment > math.MaxInt64-total {
		return 0, fmt.Errorf("duration value exceeds maximum representable span")
	}
	res := total + segment
	return res, nil
}

func unitToDuration(unit string) (time.Duration, error) {
	switch unit {
	case "s", "sec", "second", "seconds":
		return time.Second, nil
	case "m", "min", "minute", "minutes":
		return time.Minute, nil
	case "h", "hr", "hour", "hours":
		return time.Hour, nil
	case "d", "day", "days":
		return Day, nil
	case "w", "wk", "week", "weeks":
		return Week, nil
	case "mo", "mon", "month", "months":
		return Month, nil
	case "y", "yr", "year", "years":
		return Year, nil
	default:
		return 0, fmt.Errorf("unknown duration unit: %s", unit)
	}
}

func ParseBool(str string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(str)) {
	case "true", "on", "yes", "1", "enable", "enabled":
		return true, true
	case "false", "off", "no", "0", "disable", "disabled":
		return false, true
	default:
		return false, false
	}
}

// FormatDuration formats a duration into readable parts (max 3 units).
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0 seconds"
	}
	if d < time.Minute {
		secs := int(d.Seconds() + 0.999)
		if secs <= 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", secs)
	}
	var parts []string
	units := []struct {
		size time.Duration
		name string
	}{
		{Year, "year"},
		{Month, "month"},
		{Week, "week"},
		{Day, "day"},
		{time.Hour, "hour"},
		{time.Minute, "minute"},
	}
	for _, u := range units {
		if len(parts) == 3 {
			break
		}
		if val := d / u.size; val > 0 {
			parts = append(parts, formatUnit(int(val), u.name))
			d %= u.size
		}
	}
	return joinWithAnd(parts)
}

func formatUnit(val int, unit string) string {
	if val == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", val, unit)
}

func joinWithAnd(parts []string) string {
	n := len(parts)
	if n == 0 {
		return ""
	}
	if n == 1 {
		return parts[0]
	}
	return strings.Join(parts[:n-1], ", ") + " and " + parts[n-1]
}

func ParseHexColor(colorStr string) int {
	if colorStr == "" {
		return 0
	}
	clean := strings.ToLower(strings.TrimSpace(colorStr))
	clean = strings.TrimPrefix(clean, "#")
	clean = strings.TrimPrefix(clean, "0x")
	if clean == "default" || clean == "none" || clean == "no" || clean == "clear" || clean == "reset" || clean == "remove" {
		return 0
	}
	colorMap := map[string]int{
		"red":    0xFF0000,
		"blue":   0x0000FF,
		"green":  0x00FF00,
		"yellow": 0xFFFF00,
		"purple": 0x800080,
		"black":  0x000001,
		"white":  0xFFFFFF,
		"orange": 0xFF8C00,
		"pink":   0xFF69B4,
		"cyan":   0x00FFFF,
	}
	if val, ok := colorMap[clean]; ok {
		return val
	}
	if val, err := strconv.ParseInt(clean, 16, 64); err == nil {
		if val == 0 {
			return 0x000001
		}
		return int(val)
	}
	return 0
}

func TruncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

func TruncateStringWithEllipsis(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func FormatNumber(n int) string {
	return FormatNumber64(int64(n))
}

func FormatNumber64(n int64) string {
	in := strconv.FormatInt(n, 10)
	isNeg := false
	if n < 0 {
		isNeg = true
		in = in[1:]
	}
	out := make([]byte, 0, len(in)+(len(in)-1)/3+1)
	if isNeg {
		out = append(out, '-')
	}
	for i, c := range in {
		if i > 0 && (len(in)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}

func FormatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	b := float64(bytes)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.2f TB", b/TB)
	case b >= GB:
		return fmt.Sprintf("%.2f GB", b/GB)
	case b >= MB:
		return fmt.Sprintf("%.2f MB", b/MB)
	case b >= KB:
		return fmt.Sprintf("%.2f KB", b/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func FormatBool(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func FormatEnabled(b bool) string {
	if b {
		return "Enabled"
	}
	return "Disabled"
}

func TitleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			r := []rune(w)
			r[0] = unicode.ToUpper(r[0])
			words[i] = string(r)
		}
	}
	return strings.Join(words, " ")
}

func FormatPermissionsList(perms int64) []string {
	if perms&discordgo.PermissionAdministrator != 0 {
		return []string{"Administrator"}
	}
	var names []string
	if perms&discordgo.PermissionManageGuild != 0 {
		names = append(names, "Manage Server")
	}
	if perms&discordgo.PermissionManageRoles != 0 {
		names = append(names, "Manage Roles")
	}
	if perms&discordgo.PermissionManageChannels != 0 {
		names = append(names, "Manage Channels")
	}
	if perms&discordgo.PermissionKickMembers != 0 {
		names = append(names, "Kick Members")
	}
	if perms&discordgo.PermissionBanMembers != 0 {
		names = append(names, "Ban Members")
	}
	if perms&discordgo.PermissionModerateMembers != 0 {
		names = append(names, "Timeout Members")
	}
	if perms&discordgo.PermissionManageMessages != 0 {
		names = append(names, "Manage Messages")
	}
	if perms&discordgo.PermissionMentionEveryone != 0 {
		names = append(names, "Mention Everyone")
	}
	if perms&discordgo.PermissionManageWebhooks != 0 {
		names = append(names, "Manage Webhooks")
	}
	if perms&discordgo.PermissionViewAuditLogs != 0 {
		names = append(names, "View Audit Logs")
	}
	if len(names) == 0 {
		return []string{"Standard Permissions"}
	}
	return names
}

func FormatPermissions(perms int64) string {
	return strings.Join(FormatPermissionsList(perms), ", ")
}

// CleanTrackTitle removes file extensions and duplicated parenthetical titles from track names.
func CleanTrackTitle(title string) string {
	clean := strings.TrimSpace(title)
	if clean == "" {
		return ""
	}

	for {
		ext := strings.ToLower(filepath.Ext(clean))
		if ext == ".mp3" || ext == ".wav" || ext == ".flac" || ext == ".ogg" || ext == ".m4a" {
			clean = strings.TrimSpace(clean[:len(clean)-len(ext)])
			continue
		}
		break
	}

	for {
		trimmed := false
		lower := strings.ToLower(clean)
		for _, audExt := range []string{".mp3)", ".wav)", ".flac)", ".ogg)", ".m4a)"} {
			if strings.HasSuffix(lower, audExt) {
				clean = strings.TrimSpace(clean[:len(clean)-len(audExt)] + ")")
				trimmed = true
				break
			}
		}
		if trimmed {
			continue
		}
		for _, audExt := range []string{".mp3]", ".wav]", ".flac]", ".ogg]", ".m4a]"} {
			if strings.HasSuffix(lower, audExt) {
				clean = strings.TrimSpace(clean[:len(clean)-len(audExt)] + "]")
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}

	if strings.HasSuffix(clean, ")") {
		openParen := strings.LastIndex(clean, "(")
		if openParen > 0 {
			outer := strings.TrimSpace(clean[:openParen])
			inner := strings.TrimSpace(clean[openParen+1 : len(clean)-1])

			if isTitlePrefix(inner, outer) {
				return CleanTrackTitle(inner)
			}

			if strings.HasSuffix(outer, ")") {
				subOpen := strings.LastIndex(outer, "(")
				if subOpen > 0 {
					altName := strings.TrimSpace(outer[subOpen+1 : len(outer)-1])
					if altName != "" && isTitlePrefix(inner, altName) {
						tag := strings.TrimSpace(inner[len(altName):])
						if tag != "" {
							return CleanTrackTitle(fmt.Sprintf("%s %s", outer, tag))
						}
						return outer
					}
				}
			}
		}
	}

	return clean
}

func isTitlePrefix(full, prefix string) bool {
	if !strings.HasPrefix(strings.ToLower(full), strings.ToLower(prefix)) {
		return false
	}
	if len(full) == len(prefix) {
		return true
	}
	nextChar := full[len(prefix)]
	return nextChar == ' ' || nextChar == '[' || nextChar == '(' || nextChar == '-' || nextChar == '_'
}

