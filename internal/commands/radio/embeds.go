package radio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"gobot/internal/helpers"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

var (
	prefixMu         sync.RWMutex
	defaultBotPrefix = ","
	guildPrefixFunc  func(guildID string) string
)

func SetDefaultBotPrefix(prefix string) {
	prefixMu.Lock()
	defer prefixMu.Unlock()
	if prefix != "" {
		defaultBotPrefix = prefix
	}
}

func SetPrefixResolver(fn func(guildID string) string) {
	prefixMu.Lock()
	defer prefixMu.Unlock()
	guildPrefixFunc = fn
}

func GetGuildOrBotPrefix(guildID string) string {
	prefixMu.RLock()
	defer prefixMu.RUnlock()
	if guildID != "" && guildPrefixFunc != nil {
		if p := guildPrefixFunc(guildID); p != "" {
			return p
		}
	}
	return defaultBotPrefix
}

func getBotPrefix() string {
	return GetGuildOrBotPrefix("")
}

func FormatTime(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return "00:00"
	}
	totalSecs := int(seconds)
	hours := totalSecs / 3600
	mins := (totalSecs % 3600) / 60
	secs := totalSecs % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

func GenerateProgressBar(current, total float64) string {
	const totalBlocks = 16
	if total <= 0 {
		return "▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬"
	}
	if current <= 0 {
		return "░░░░░░░░░░░░░░░░"
	}

	percent := current / total
	if percent > 1.0 {
		percent = 1.0
	}

	filledBlocks := int(percent * float64(totalBlocks))
	var sb strings.Builder
	sb.Grow(48)

	for i := 0; i < totalBlocks; i++ {
		if i < filledBlocks {
			sb.WriteString("█")
		} else {
			sb.WriteString("░")
		}
	}

	return sb.String()
}

// GenerateTimelineProgressBar renders a 10-segment playback bar with elapsed and total time.
func GenerateTimelineProgressBar(elapsed, duration float64) string {
	const totalSegments = 10
	if duration <= 0 {
		var sb strings.Builder
		for i := 0; i < totalSegments; i++ {
			sb.WriteString(GetFormattedEmoji(fmt.Sprintf("bar_f%d", i)))
		}
		return fmt.Sprintf("`%s` %s `Live`", FormatTime(elapsed), sb.String())
	}

	fraction := elapsed / duration
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}

	pos := int(fraction * float64(totalSegments))
	if pos < 0 {
		pos = 0
	}
	if pos > totalSegments {
		pos = totalSegments
	}

	var sb strings.Builder
	for i := 0; i < totalSegments; i++ {
		if pos >= totalSegments {
			sb.WriteString(GetFormattedEmoji(fmt.Sprintf("bar_f%d", i)))
		} else if i < pos {
			sb.WriteString(GetFormattedEmoji(fmt.Sprintf("bar_f%d", i)))
		} else if i == pos {
			sb.WriteString(GetFormattedEmoji(fmt.Sprintf("bar_k%d", i)))
		} else {
			if i == totalSegments-1 {
				sb.WriteString(GetFormattedEmoji("bar_er"))
			} else {
				sb.WriteString(GetFormattedEmoji("bar_em"))
			}
		}
	}

	displayElapsed := elapsed
	if duration > 0 && displayElapsed > duration {
		displayElapsed = duration
	}
	return fmt.Sprintf("`%s` %s `%s`", FormatTime(displayElapsed), sb.String(), FormatTime(duration))
}

func isRadioRelatedMessage(message *discordgo.Message, botID string) bool {
	if message == nil {
		return false
	}
	if _, ok := TempNotifMsgs.Load(message.ID); ok {
		return true
	}
	p := getBotPrefix()
	content := strings.ToLower(strings.TrimSpace(message.Content))
	commandPrefixes := []string{
		p + "r ", p + "r", p + "radio ", p + "radio", p + "999fm ", p + "999fm",
		p + "play ", p + "p ", p + "playfile", p + "playlist",
		p + "skip", p + "s", p + "pause", p + "resume", p + "loop",
		p + "queue", p + "q", p + "np", p + "shuffle",
		p + "stop", p + "leave", p + "prev", p + "previous",
		p + "247", p + "24/7", p + "mode", p + "restriction", p + "setchannel",
		",r ", ",r", ",radio ", ",radio", ",999fm ", ",999fm",
		",play ", ",p ", ",playfile", ",playlist",
		",skip", ",s", ",pause", ",resume", ",loop",
		",queue", ",q", ",np", ",shuffle",
		",stop", ",leave", ",prev", ",previous",
		",247", ",24/7", ",mode", ",restriction", ",setchannel",
		"?r ", "?r", "?radio ", "?radio", "?999fm ", "?999fm",
		"?play ", "?p ", "?playfile", "?playlist",
		"?skip", "?s", "?pause", "?resume", "?loop",
		"?queue", "?q", "?np", "?shuffle",
		"?stop", "?leave", "?prev", "?previous",
		"?247", "?24/7", "?mode", "?restriction", "?setchannel",
	}
	for _, pref := range commandPrefixes {
		if strings.HasPrefix(content, pref) || content == strings.TrimSpace(pref) {
			return true
		}
	}

	if len(message.Embeds) > 0 && message.Embeds[0] != nil {
		title := message.Embeds[0].Title
		description := message.Embeds[0].Description
		embedPrefixes := []string{
			"Skipped by", "Returned to", "Queued", "Added", "Paused", "Resumed",
			"Stopped", "Loop mode set to", "24/7 Mode", "Connected to voice",
			"Searching for", "Loading track", "No track playing", "Radio Queue",
			"Session Track History", "Disconnected radio", "Shuffled", "Cleared",
			"Radio Mode", "Restriction", "Disabled autojoin", "Set autojoin", "Set radio mode",
			"Updated radio restriction",
		}
		for _, ep := range embedPrefixes {
			if strings.HasPrefix(description, ep) || strings.HasPrefix(title, ep) {
				return true
			}
		}
		if botID != "" && message.Author != nil && message.Author.ID == botID {
			if len(message.Components) > 0 {
				return true
			}
		}
	}
	return false
}

func getSnapshotElapsed(snap coordinator.Snapshot) float64 {
	if snap.Current == nil || snap.TrackStartTime.IsZero() {
		return 0
	}
	if snap.Phase == coordinator.PhasePaused && !snap.PauseStartTime.IsZero() {
		return snap.PauseStartTime.Sub(snap.TrackStartTime).Seconds() - snap.PausedDuration.Seconds()
	}
	return time.Since(snap.TrackStartTime).Seconds() - snap.PausedDuration.Seconds()
}

func BuildRadioEmbedFromSnapshot(snap coordinator.Snapshot) *discordgo.MessageEmbed {
	p := getBotPrefix()
	if snap.Phase == coordinator.PhaseRecovering {
		description := "Voice connection lost. Reconnecting to the voice channel..."
		if snap.Current != nil {
			description = fmt.Sprintf("Voice connection lost while playing **%s**. Reconnecting to the voice channel...", snap.Current.Title)
		}
		return &discordgo.MessageEmbed{
			Title:       "Voice connection lost",
			Description: description,
			Color:       helpers.ColorPending,
		}
	}
	if snap.Current == nil {
		desc := fmt.Sprintf("Use `%sr play <song>` or attach an audio file to queue tracks.", p)
		title := "No track playing"
		if snap.Phase == coordinator.PhaseLoading {
			title = "Loading track..."
			if !snap.LoadingSince.IsZero() && time.Since(snap.LoadingSince) >= 5*time.Second {
				title = "Still downloading..."
			}
			if len(snap.Queue) > 0 {
				desc = fmt.Sprintf("Preparing **%s** from queue...", snap.Queue[0].Title)
				if title == "Still downloading..." {
					desc = fmt.Sprintf("Still downloading **%s**...", snap.Queue[0].Title)
				}
			} else {
				desc = "Finding a track..."
				if title == "Still downloading..." {
					desc = "Still downloading..."
				}
			}
		}
		return &discordgo.MessageEmbed{
			Title:       title,
			Description: desc,
			Color:       helpers.ColorDefault,
		}
	}

	track := *snap.Current
	isPaused := snap.Phase == coordinator.PhasePaused
	elapsed := getSnapshotElapsed(snap)

	barStr := GenerateProgressBar(elapsed, float64(track.Duration))

	title := track.Title
	if isPaused {
		title = "(Paused) " + title
	}

	fields := []*discordgo.MessageEmbedField{}

	if track.Era != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Era",
			Value:  track.Era,
			Inline: true,
		})
	}

	catVal := track.Category
	if catVal == "" {
		if strings.Contains(strings.ToLower(track.Uploader), "juice") {
			catVal = "Unreleased"
		} else {
			catVal = "Stream"
		}
	}
	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "Category",
		Value:  helpers.TitleCase(catVal),
		Inline: true,
	})

	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "Queue",
		Value:  fmt.Sprintf("**%d** song(s) up next", len(snap.Queue)),
		Inline: true,
	})

	if snap.Mode != "" && snap.Mode != "all" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Radio Mode",
			Value:  fmt.Sprintf("`%s`", snap.Mode),
			Inline: true,
		})
	}

	timelineVal := fmt.Sprintf("`%s` %s `%s`", FormatTime(elapsed), barStr, FormatTime(float64(track.Duration)))
	if track.Duration <= 0 {
		timelineVal = fmt.Sprintf("`%s` %s `Live`", FormatTime(elapsed), barStr)
	}

	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "Timeline",
		Value:  timelineVal,
		Inline: false,
	})

	embed := &discordgo.MessageEmbed{
		Title:  title,
		Color:  helpers.ColorDefault,
		Fields: fields,
	}

	defaultBackupCover := helpers.DefaultRadioCoverURL

	if len(track.CoverData) > 0 && track.CoverFilename != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: "attachment://" + track.CoverFilename,
		}
	} else if track.Thumbnail != "" && (strings.HasPrefix(track.Thumbnail, "http://") || strings.HasPrefix(track.Thumbnail, "https://")) {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: track.Thumbnail,
		}
	} else {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: defaultBackupCover,
		}
	}

	return embed
}

func BuildRadioComponentsFromSnapshot(snap coordinator.Snapshot) []discordgo.MessageComponent {
	isPaused := snap.Phase == coordinator.PhasePaused

	playPauseLabel := "Pause"
	playPauseStyle := discordgo.PrimaryButton
	if isPaused {
		playPauseLabel = "Resume"
		playPauseStyle = discordgo.SuccessButton
	}

	loopLabel := "Loop: Off"
	loopStyle := discordgo.SecondaryButton
	if snap.LoopMode == coordinator.LoopTrack {
		loopLabel = "Loop: Track"
		loopStyle = discordgo.SuccessButton
	} else if snap.LoopMode == coordinator.LoopQueue {
		loopLabel = "Loop: Queue"
		loopStyle = discordgo.SuccessButton
	}

	row := discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{Style: discordgo.SecondaryButton, Label: "Previous", CustomID: "r_previous"},
			discordgo.Button{Style: playPauseStyle, Label: playPauseLabel, CustomID: "r_pause_resume"},
			discordgo.Button{Style: discordgo.SecondaryButton, Label: "Skip", CustomID: "r_skip"},
			discordgo.Button{Style: loopStyle, Label: loopLabel, CustomID: "r_loop"},
			discordgo.Button{Style: discordgo.DangerButton, Label: "Stop", CustomID: "r_stop"},
		},
	}

	return []discordgo.MessageComponent{row}
}

func formatCategoryDisplay(cat, uploader string) string {
	clean := strings.ToLower(strings.TrimSpace(cat))
	clean = strings.ReplaceAll(clean, "_", " ")
	if clean == "" {
		if strings.Contains(strings.ToLower(uploader), "juice") {
			return "Unreleased"
		}
		return "Stream"
	}
	if clean == "recording session" || clean == "session" || clean == "sessions" {
		return "Studio Session"
	}
	return helpers.TitleCase(clean)
}

func BuildRadioCV2PlayerCard(snap coordinator.Snapshot) []map[string]interface{} {
	titleIcon := GetFormattedEmoji("playing")
	if snap.Phase == coordinator.PhasePaused {
		titleIcon = GetFormattedEmoji("paused")
	}

	trackTitle := "No track playing"
	if snap.Current != nil && snap.Current.Title != "" {
		trackTitle = snap.Current.Title
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("## %s %s", titleIcon, trackTitle))

	if snap.Current != nil {
		era := snap.Current.Era
		cat := formatCategoryDisplay(snap.Current.Category, snap.Current.Uploader)
		if era != "" {
			lines = append(lines, fmt.Sprintf("-# **%s · %s**", era, cat))
		} else {
			lines = append(lines, fmt.Sprintf("-# **%s**", cat))
		}
	}

	if len(snap.Queue) > 0 {
		lines = append(lines, fmt.Sprintf("-# Next: **%s**", snap.Queue[0].Title))
		lines = append(lines, fmt.Sprintf("-# Queue: **%d tracks**", len(snap.Queue)))
	} else {
		lines = append(lines, "-# Next: **Autoplay**")
		lines = append(lines, "-# Queue: **Empty**")
	}

	headerText := strings.Join(lines, "\n")

	coverURL := helpers.DefaultRadioCoverURL
	if snap.Current != nil {
		if snap.Current.Thumbnail != "" && (strings.HasPrefix(snap.Current.Thumbnail, "http://") || strings.HasPrefix(snap.Current.Thumbnail, "https://")) {
			coverURL = snap.Current.Thumbnail
		} else if snap.Current.CoverURL != "" && (strings.HasPrefix(snap.Current.CoverURL, "http://") || strings.HasPrefix(snap.Current.CoverURL, "https://")) {
			coverURL = snap.Current.CoverURL
		}
	}

	headerSection := helpers.SectionWithAccessory(
		helpers.MediaAccessory(coverURL),
		helpers.TextDisplay(headerText),
	)

	separator := helpers.Separator()

	elapsed := getSnapshotElapsed(snap)
	duration := 0.0
	if snap.Current != nil {
		duration = float64(snap.Current.Duration)
	}
	timelineDisplay := helpers.TextDisplay(GenerateTimelineProgressBar(elapsed, duration))

	pauseBtnStyle := helpers.ButtonStyleSecondary
	pauseBtnKey := "pause"
	if snap.Phase == coordinator.PhasePaused {
		pauseBtnStyle = helpers.ButtonStyleSuccess
		pauseBtnKey = "play"
	}

	loopBtnStyle := helpers.ButtonStyleSecondary
	loopBtnKey := "loop"
	if snap.LoopMode == coordinator.LoopTrack {
		loopBtnStyle = helpers.ButtonStyleSuccess
		loopBtnKey = "loop_track"
	} else if snap.LoopMode == coordinator.LoopQueue {
		loopBtnStyle = helpers.ButtonStylePrimary
		loopBtnKey = "loop_queue"
	}

	controlsRow := helpers.ActionRow(
		map[string]interface{}{
			"type":      helpers.ComponentTypeButton,
			"style":     helpers.ButtonStyleSecondary,
			"custom_id": "r_download",
			"emoji":     GetEmoji("download"),
		},
		map[string]interface{}{
			"type":      helpers.ComponentTypeButton,
			"style":     helpers.ButtonStyleSecondary,
			"custom_id": "r_prev",
			"disabled":  len(snap.History) == 0,
			"emoji":     GetEmoji("previous"),
		},
		map[string]interface{}{
			"type":      helpers.ComponentTypeButton,
			"style":     pauseBtnStyle,
			"custom_id": "r_pause",
			"emoji":     GetEmoji(pauseBtnKey),
		},
		map[string]interface{}{
			"type":      helpers.ComponentTypeButton,
			"style":     helpers.ButtonStyleSecondary,
			"custom_id": "r_skip",
			"emoji":     GetEmoji("skip"),
		},
		map[string]interface{}{
			"type":      helpers.ComponentTypeButton,
			"style":     loopBtnStyle,
			"custom_id": "r_loop",
			"emoji":     GetEmoji(loopBtnKey),
		},
	)

	optionsRow := helpers.ActionRow(map[string]interface{}{
		"type":        helpers.ComponentTypeSelectMenu,
		"custom_id":   "r_player_options",
		"placeholder": "Options",
		"options": []map[string]interface{}{
			{
				"label":       "View Queue",
				"value":       "opt_queue",
				"description": "View upcoming tracks in queue",
				"default":     false,
			},
			{
				"label":       "Track Info & Metadata",
				"value":       "opt_info",
				"description": "Bitrate, release date, and tags",
				"default":     false,
			},
			{
				"label":       "Lyrics",
				"value":       "opt_lyrics",
				"description": "View lyrics for the playing track",
				"default":     false,
			},
			{
				"label":       "Radio Mode",
				"value":       "opt_mode",
				"description": "Switch between eras or studio only",
				"default":     false,
			},
			{
				"label":       "Commands & Help",
				"value":       "opt_commands",
				"description": "View available radio commands and syntax",
				"default":     false,
			},
			{
				"label":       "Stop Radio",
				"value":       "opt_stop",
				"description": "Disconnect bot from voice channel",
				"default":     false,
			},
		},
	})

	return []map[string]interface{}{
		headerSection,
		separator,
		timelineDisplay,
		controlsRow,
		optionsRow,
	}
}

const QueuePageSize = 20

func BuildRadioCV2QueueCard(snap coordinator.Snapshot, page int) []map[string]interface{} {
	totalTracks := len(snap.Queue)
	totalPages := (totalTracks + QueuePageSize - 1) / QueuePageSize
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	queueHeader := "## Queue · Empty"
	if totalTracks > 0 {
		queueHeader = fmt.Sprintf("## Queue · %d tracks", totalTracks)
	}

	var sublines []string
	if snap.Current != nil {
		sublines = append(sublines, "-# Now playing:")
		sublines = append(sublines, fmt.Sprintf("-# **%s**", snap.Current.Title))
		era := snap.Current.Era
		cat := formatCategoryDisplay(snap.Current.Category, snap.Current.Uploader)
		if era != "" {
			sublines = append(sublines, fmt.Sprintf("-# %s · %s", era, cat))
		} else {
			sublines = append(sublines, fmt.Sprintf("-# %s", cat))
		}
	} else {
		sublines = append(sublines, "-# No track currently playing")
	}

	headerText := queueHeader + "\n" + strings.Join(sublines, "\n")

	coverURL := helpers.DefaultRadioCoverURL
	if snap.Current != nil {
		if snap.Current.Thumbnail != "" && (strings.HasPrefix(snap.Current.Thumbnail, "http://") || strings.HasPrefix(snap.Current.Thumbnail, "https://")) {
			coverURL = snap.Current.Thumbnail
		} else if snap.Current.CoverURL != "" && (strings.HasPrefix(snap.Current.CoverURL, "http://") || strings.HasPrefix(snap.Current.CoverURL, "https://")) {
			coverURL = snap.Current.CoverURL
		}
	}

	headerSection := helpers.SectionWithAccessory(
		helpers.MediaAccessory(coverURL),
		helpers.TextDisplay(headerText),
	)

	separator := helpers.Separator()

	var bodyText string
	if totalTracks == 0 {
		bodyText = "-# Queue is empty. Autoplay will keep the music going."
	} else {
		start := (page - 1) * QueuePageSize
		end := start + QueuePageSize
		if end > totalTracks {
			end = totalTracks
		}
		var trackLines []string
		for i := start; i < end; i++ {
			t := snap.Queue[i]
			trackDisplay := t.Title
			if t.Era != "" {
				trackDisplay = fmt.Sprintf("%s · %s", t.Title, t.Era)
			}
			if i == 0 {
				trackLines = append(trackLines, fmt.Sprintf("-# %d. **%s**", i+1, trackDisplay))
			} else {
				trackLines = append(trackLines, fmt.Sprintf("-# %d. %s", i+1, trackDisplay))
			}
		}
		bodyText = strings.Join(trackLines, "\n")
	}

	bodyDisplay := helpers.TextDisplay(bodyText)

	components := []map[string]interface{}{
		headerSection,
		separator,
		bodyDisplay,
	}

	if totalPages > 1 {
		paginationRow := helpers.ActionRow(
			map[string]interface{}{
				"type":      helpers.ComponentTypeButton,
				"style":     helpers.ButtonStyleSecondary,
				"custom_id": "r_q_prev",
				"disabled":  page <= 1,
				"emoji":     GetEmoji("previous"),
			},
			map[string]interface{}{
				"type":      helpers.ComponentTypeButton,
				"style":     helpers.ButtonStyleSecondary,
				"custom_id": "r_q_page",
				"label":     fmt.Sprintf("Page %d/%d", page, totalPages),
				"disabled":  true,
			},
			map[string]interface{}{
				"type":      helpers.ComponentTypeButton,
				"style":     helpers.ButtonStyleSecondary,
				"custom_id": "r_q_next",
				"disabled":  page >= totalPages,
				"emoji":     GetEmoji("skip"),
			},
		)
		components = append(components, paginationRow)
	}

	actionsRow := helpers.ActionRow(
		map[string]interface{}{
			"type":      helpers.ComponentTypeButton,
			"style":     helpers.ButtonStylePrimary,
			"custom_id": "r_q_back",
			"label":     "Back",
		},
		map[string]interface{}{
			"type":      helpers.ComponentTypeButton,
			"style":     helpers.ButtonStyleSecondary,
			"custom_id": "r_q_shuffle",
			"label":     "Shuffle",
			"disabled":  totalTracks <= 1,
		},
		map[string]interface{}{
			"type":      helpers.ComponentTypeButton,
			"style":     helpers.ButtonStyleDanger,
			"custom_id": "r_q_clear",
			"label":     "Clear Queue",
			"disabled":  totalTracks == 0,
		},
	)
	components = append(components, actionsRow)

	return components
}

func BuildRadioCV2LoadingCard(snap coordinator.Snapshot) []map[string]interface{} {
	title := fmt.Sprintf("## %s Loading track...", GetFormattedEmoji("playing"))
	subtext := "-# Finding a track from library..."
	if len(snap.Queue) > 0 {
		subtext = fmt.Sprintf("-# Preparing **%s** from queue...", snap.Queue[0].Title)
	}

	headerSection := helpers.SectionWithAccessory(
		helpers.MediaAccessory(helpers.DefaultRadioCoverURL),
		helpers.TextDisplay(title+"\n"+subtext),
	)

	stopRow := helpers.ActionRow(map[string]interface{}{
		"type":      helpers.ComponentTypeButton,
		"style":     helpers.ButtonStyleDanger,
		"custom_id": "r_stop",
		"label":     "Stop Radio",
	})

	return []map[string]interface{}{
		headerSection,
		helpers.Separator(),
		stopRow,
	}
}

func BuildRadioCV2IdleCard() []map[string]interface{} {
	title := fmt.Sprintf("## %s Radio Idle", GetFormattedEmoji("paused"))
	subtext := "-# Use `,r play <song>` or attach an audio file to start listening."

	headerSection := helpers.SectionWithAccessory(
		helpers.MediaAccessory(helpers.DefaultRadioCoverURL),
		helpers.TextDisplay(title+"\n"+subtext),
	)

	stopRow := helpers.ActionRow(map[string]interface{}{
		"type":      helpers.ComponentTypeButton,
		"style":     helpers.ButtonStyleDanger,
		"custom_id": "r_stop",
		"label":     "Stop Radio",
	})

	return []map[string]interface{}{
		headerSection,
		helpers.Separator(),
		stopRow,
	}
}

func BuildRadioCV2RecoveringCard(snap coordinator.Snapshot) []map[string]interface{} {
	title := fmt.Sprintf("## %s Reconnecting...", GetFormattedEmoji("paused"))
	subtext := "-# Voice connection lost. Reconnecting to the voice channel..."
	if snap.Current != nil {
		subtext = fmt.Sprintf("-# Voice connection lost while playing **%s**. Reconnecting...", snap.Current.Title)
	}

	headerSection := helpers.SectionWithAccessory(
		helpers.MediaAccessory(helpers.DefaultRadioCoverURL),
		helpers.TextDisplay(title+"\n"+subtext),
	)

	stopRow := helpers.ActionRow(map[string]interface{}{
		"type":      helpers.ComponentTypeButton,
		"style":     helpers.ButtonStyleDanger,
		"custom_id": "r_stop",
		"label":     "Stop Radio",
	})

	return []map[string]interface{}{
		headerSection,
		helpers.Separator(),
		stopRow,
	}
}

type DiscordControllerPort struct {
	mu             sync.Mutex
	Session        *discordgo.Session
	attachmentURLs map[string]string
}

func NewDiscordControllerPort(s *discordgo.Session) *DiscordControllerPort {
	return &DiscordControllerPort{Session: s, attachmentURLs: make(map[string]string)}
}

func (p *DiscordControllerPort) CheckControllerPermissions(ctx context.Context, ref coordinator.ControllerRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	session := p.Session
	p.mu.Unlock()
	if session == nil || session.State == nil || session.State.User == nil || ref.ChannelID == "" {
		return coordinator.ErrMissingContext
	}
	permissions, err := session.State.UserChannelPermissions(session.State.User.ID, ref.ChannelID)
	if err != nil {
		return err
	}
	if permissions&discordgo.PermissionAdministrator != 0 {
		return nil
	}
	const required = discordgo.PermissionViewChannel | discordgo.PermissionSendMessages | discordgo.PermissionEmbedLinks | discordgo.PermissionReadMessageHistory
	if permissions&required != required {
		return errors.New("controller channel permissions are incomplete")
	}
	return nil
}

func (p *DiscordControllerPort) CreateOrUpdate(ctx context.Context, ref coordinator.ControllerRef, snap coordinator.Snapshot) (string, error) {
	p.mu.Lock()
	session := p.Session
	var cachedURL string
	if snap.Current != nil && snap.Current.Title != "" {
		cachedURL = p.attachmentURLs[snap.Current.Title]
	}
	p.mu.Unlock()
	if session == nil {
		return "", coordinator.ErrMissingContext
	}

	if snap.Current != nil && cachedURL != "" {
		track := snap.Current.Clone()
		track.Thumbnail = cachedURL
		track.CoverData = nil
		track.CoverFilename = ""
		snap.Current = &track
	}

	var cv2Components []map[string]interface{}
	switch snap.Phase {
	case coordinator.PhaseRecovering:
		cv2Components = BuildRadioCV2RecoveringCard(snap)
	case coordinator.PhaseLoading:
		cv2Components = BuildRadioCV2LoadingCard(snap)
	default:
		if snap.Current == nil {
			cv2Components = BuildRadioCV2IdleCard()
		} else if snap.ControllerView == coordinator.ControllerViewQueue {
			page := snap.QueuePage
			if page < 1 {
				page = 1
			}
			cv2Components = BuildRadioCV2QueueCard(snap, page)
		} else {
			cv2Components = BuildRadioCV2PlayerCard(snap)
		}
	}

	file := prepareCoverFile(snap.Current)
	shouldResend := false
	if ref.MessageID != "" {
		botID := ""
		if session.State != nil && session.State.User != nil {
			botID = session.State.User.ID
		}
		if messages, err := session.ChannelMessages(ref.ChannelID, 8, "", "", ""); err == nil {
			for _, message := range messages {
				if message.ID == ref.MessageID {
					break
				}
				if !isRadioRelatedMessage(message, botID) {
					shouldResend = true
					break
				}
			}
		}
	}

	if ref.MessageID != "" && !shouldResend {
		errPatch := helpers.PatchCV2Message(session, ref.ChannelID, ref.MessageID, cv2Components, nil)
		if errPatch == nil {
			if snap.PlayerChannelID != "" && ref.ChannelID == snap.PlayerChannelID {
				go purgeStrayMessagesInPlayerChannel(session, ref.ChannelID, ref.MessageID)
			}
			return ref.MessageID, nil
		}
		if !coordinator.IsDiscordNotFound(errPatch) {
			return "", errPatch
		}
	}

	if file != nil {
		msg, err := helpers.SendCV2MessageWithFilesAndReturn(session, ref.ChannelID, cv2Components, []*discordgo.File{file})
		if err != nil {
			return "", err
		}
		if snap.Current != nil && snap.Current.Title != "" {
			p.rememberControllerAttachment(snap.Current.Title, msg)
		}
		if snap.PlayerChannelID != "" && ref.ChannelID == snap.PlayerChannelID {
			go purgeStrayMessagesInPlayerChannel(session, ref.ChannelID, msg.ID)
		}
		return msg.ID, nil
	}

	msg, err := helpers.SendCV2MessageAndReturn(session, ref.ChannelID, cv2Components, nil)
	if err != nil {
		return "", err
	}
	if snap.PlayerChannelID != "" && ref.ChannelID == snap.PlayerChannelID {
		go purgeStrayMessagesInPlayerChannel(session, ref.ChannelID, msg.ID)
	}
	return msg.ID, nil
}

func purgeStrayMessagesInPlayerChannel(session *discordgo.Session, channelID string, keepMessageID string) {
	if session == nil || channelID == "" {
		return
	}
	messages, err := session.ChannelMessages(channelID, 25, "", "", "")
	if err != nil {
		return
	}
	for _, m := range messages {
		if m != nil && m.ID != keepMessageID {
			_ = session.ChannelMessageDelete(channelID, m.ID)
		}
	}
}

func (p *DiscordControllerPort) rememberControllerAttachment(title string, message *discordgo.Message) {
	if message == nil || len(message.Attachments) == 0 || title == "" {
		return
	}
	p.mu.Lock()
	if p.attachmentURLs == nil {
		p.attachmentURLs = make(map[string]string)
	}
	p.attachmentURLs[title] = message.Attachments[0].URL
	p.mu.Unlock()
}

func (p *DiscordControllerPort) Delete(ctx context.Context, ref coordinator.ControllerRef) error {
	p.mu.Lock()
	session := p.Session
	p.mu.Unlock()
	if session == nil || ref.ChannelID == "" || ref.MessageID == "" {
		return nil
	}
	return session.ChannelMessageDelete(ref.ChannelID, ref.MessageID)
}

func prepareCoverFile(track *Track) *discordgo.File {
	if track != nil && len(track.CoverData) > 0 && track.CoverFilename != "" {
		return &discordgo.File{
			Name:        track.CoverFilename,
			ContentType: "image/jpeg",
			Reader:      bytes.NewReader(track.CoverData),
		}
	}
	return nil
}

func BuildRadioCommandsCV2Card(prefix string, thumbURL ...string) []map[string]interface{} {
	if prefix == "" {
		prefix = getBotPrefix()
	}

	headerDisplay := helpers.TextDisplay(fmt.Sprintf("## Radio Commands\n-# Server Prefix: `%s` · Commands: `%[1]sr <command>` or `%[1]splay <song>`", prefix))

	playbackSection := helpers.TextDisplay(fmt.Sprintf(
		"### Playback\n"+
			"> `%[1]sr play <song>` · Stream from library, YouTube, or SoundCloud\n"+
			"> `%[1]sr session <song>` · Interactive studio sessions & stems selector\n"+
			"> `%[1]sr playnext <song>` · Queue track to play immediately next\n"+
			"> `%[1]sr playfile` · Play attached audio file or `.txt` playlist\n"+
			"> `%[1]sr pause` · `%[1]sr resume` · Toggle voice playback\n"+
			"> `%[1]sr skip` · `%[1]sr previous` · Advance or replay tracks",
		prefix,
	))

	queueSection := helpers.TextDisplay(fmt.Sprintf(
		"### Queue & Modes\n"+
			"> `%[1]sr queue` · View upcoming songs with page navigation\n"+
			"> `%[1]sr loop [track|queue|off]` · Cycle or toggle loop mode\n"+
			"> `%[1]sr shuffle` · `%[1]sr clearqueue` · Reorder or clear queue\n"+
			"> `%[1]sr mode <era>` · Filter autoplay (`gbgr`, `drfl`, `jw3`, `sessions`)",
		prefix,
	))

	settingsSection := helpers.TextDisplay(fmt.Sprintf(
		"### Voice & Settings\n"+
			"> `%[1]sr search <query>` · Search YouTube and SoundCloud\n"+
			"> `%[1]sr join` · `%[1]sr leave` · Summon or disconnect bot from voice\n"+
			"> `%[1]sr 247` · Toggle 24/7 continuous voice presence\n"+
			"> `%[1]sr restriction [vc|all|mods]` · Control playback permissions",
		prefix,
	))

	return []map[string]interface{}{
		headerDisplay,
		helpers.Separator(),
		playbackSection,
		queueSection,
		settingsSection,
		helpers.Separator(),
	}
}

func BuildRadioCommandsFallbackText(prefix string) string {
	if prefix == "" {
		prefix = getBotPrefix()
	}
	return fmt.Sprintf(
		"**Radio Commands** (Prefix: `%[1]s`)\n\n"+
			"**Playback**\n"+
			"> `%[1]sr play <song>` · Stream from library, YouTube, or SoundCloud\n"+
			"> `%[1]sr session <song>` · Interactive studio sessions selector\n"+
			"> `%[1]sr playnext <song>` · Queue track to play next\n"+
			"> `%[1]sr playfile` · Play attached audio file or playlist\n"+
			"> `%[1]sr pause` · `%[1]sr resume` · Toggle playback\n"+
			"> `%[1]sr skip` · `%[1]sr previous` · Advance or replay tracks\n\n"+
			"**Queue & Modes**\n"+
			"> `%[1]sr queue` · View upcoming songs\n"+
			"> `%[1]sr loop [track|queue|off]` · Set loop mode\n"+
			"> `%[1]sr shuffle` · `%[1]sr clearqueue` · Reorder or clear queue\n"+
			"> `%[1]sr mode <era>` · Filter autoplay (`gbgr`, `drfl`, `jw3`, `sessions`)\n\n"+
			"**Voice & Settings**\n"+
			"> `%[1]sr search <query>` · Search YouTube and SoundCloud\n"+
			"> `%[1]sr join` · `%[1]sr leave` · Voice connection\n"+
			"> `%[1]sr 247` · Toggle 24/7 continuous voice\n"+
			"> `%[1]sr restriction [vc|all|mods]` · Control permissions\n\n"+
			"-# Tip: Interactive buttons on the player card can also control playback.",
		prefix,
	)
}
