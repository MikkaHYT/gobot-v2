package radio

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/database"
	"gobot/internal/helpers"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

type Track = coordinator.Track
type LoopMode = coordinator.LoopMode

func resolveQuery(ctx *bot.Context, query string) ([]*coordinator.Track, error) {
	if ctx != nil && ctx.Radio != nil {
		return ctx.Radio.ResolveQuery(ctx.Context(), query)
	}
	return coordinator.ResolveQuery(ctx.Context(), query)
}

type RadioGroupCmd struct{}

func requireRadio(ctx *bot.Context) (*coordinator.Module, error) {
	if ctx == nil || ctx.Radio == nil {
		return nil, fmt.Errorf("radio is currently unavailable")
	}
	return ctx.Radio, nil
}

func (c *RadioGroupCmd) Name() string      { return "radio" }
func (c *RadioGroupCmd) Aliases() []string { return []string{"r", "999fm", "play", "music", "rad"} }
func (c *RadioGroupCmd) Category() string  { return "Juice WRLD" }
func (c *RadioGroupCmd) Description() string {
	return "Music streaming and Juice WRLD radio"
}
func (c *RadioGroupCmd) Usage() string {
	return "[join|play|playfile|skip|pause|resume|loop|queue|np|previous|clearqueue|history|mode|restriction|setchannel|leave] [args]"
}
func (c *RadioGroupCmd) Example() string {
	return "join | play Rental | skip | loop track | restriction vc"
}

func (c *RadioGroupCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "join", Description: "Connect bot to your current voice channel", Usage: "", Example: ""},
		{Name: "play", Description: "Stream a Juice WRLD song or local track by name", Usage: "<song_name>", Example: "Rental"},
		{Name: "playnext", Description: "Queue a song or file to play immediately next", Usage: "<song_name>", Example: "Armed and Dangerous"},
		{Name: "playlist", Description: "Queue tracks from an attached .txt playlist file", Usage: "[attached_file.txt]", Example: ""},
		{Name: "playfile", Description: "Play an attached audio or .txt playlist file", Usage: "[attached_file]", Example: ""},
		{Name: "skip", Description: "Skip the currently playing track", Usage: "", Example: ""},
		{Name: "previous", Description: "Play the previous track from history", Usage: "", Example: ""},
		{Name: "pause", Description: "Pause radio playback", Usage: "", Example: ""},
		{Name: "resume", Description: "Resume paused radio playback", Usage: "", Example: ""},
		{Name: "loop", Description: "Toggle or set loop mode", Usage: "[off|track|queue]", Example: "track"},
		{Name: "queue", Description: "Display upcoming songs in the voice queue", Usage: "", Example: ""},
		{Name: "shuffle", Description: "Randomly shuffle upcoming songs in the queue", Usage: "", Example: ""},
		{Name: "np", Description: "Display currently playing track details", Usage: "", Example: ""},
		{Name: "clearqueue", Description: "Clear all pending tracks from queue", Usage: "", Example: ""},
		{Name: "history", Description: "View recently played tracks", Usage: "", Example: ""},
		{Name: "mode", Description: "Set streaming mode or era", Usage: "[GBGR|DRFL|LND|unreleased]", Example: "unreleased"},
		{Name: "restriction", Description: "Set control permissions", Usage: "[vc|all|mods]", Example: "vc"},
		{Name: "info", Description: "Display detailed metadata for currently playing track", Usage: "", Example: ""},
		{Name: "lyrics", Description: "Display lyrics for currently playing track", Usage: "", Example: ""},
		{Name: "setchannel", Description: "Configure auto-join voice channel", Usage: "<#channel|clear>", Example: "#radio"},
		{Name: "setplayerchannel", Description: "Configure dedicated channel for hands-free player controller", Usage: "<#channel|clear>", Example: "#radio-player"},
		{Name: "247", Description: "Toggle 24/7 continuous voice channel stay mode", Usage: "", Example: ""},
		{Name: "leave", Description: "Disconnect bot from voice channel", Usage: "", Example: ""},
		{Name: "session", Description: "Play session edits for a Juice WRLD song with interactive selection", Usage: "<song_name>", Example: "Sometimes"},
		{Name: "search", Description: "Search SoundCloud/YouTube and pick a track to play", Usage: "<query>", Example: "Armed and Dangerous"},
		{Name: "sc", Description: "Search SoundCloud and pick a track to play", Usage: "<query>", Example: "Rental"},
		{Name: "yt", Description: "Search YouTube and pick a track to play", Usage: "<query>", Example: "Lucid Dreams"},
		{Name: "forceleave", Description: "Forcibly disconnect bot and purge voice session (Admin)", Usage: "", Example: ""},
		{Name: "reset", Description: "Forcibly reset radio state and kill playback processes (Admin)", Usage: "", Example: ""},
	}
}

func (c *RadioGroupCmd) ensureConnected(ctx *bot.Context, suppressAutoplay ...bool) (*discordgo.VoiceState, error) {
	radio, err := requireRadio(ctx)
	if err != nil {
		return nil, err
	}

	voiceState, err := GetUserVoiceState(ctx.Session, ctx.Message.GuildID, ctx.Message.Author.ID)
	if err != nil || voiceState == nil || voiceState.ChannelID == "" {
		return nil, fmt.Errorf("please connect to a voice channel before using this command")
	}

	if err := checkBotVoicePermissions(ctx.Session, ctx.Message.GuildID, voiceState.ChannelID); err != nil {
		return nil, err
	}

	suppress := false
	if len(suppressAutoplay) > 0 {
		suppress = suppressAutoplay[0]
	}

	if _, errConnect := radio.Connect(ctx.Context(), coordinator.ConnectRequest{
		GuildID:          ctx.Message.GuildID,
		VoiceChannelID:   voiceState.ChannelID,
		TextChannelID:    ctx.Message.ChannelID,
		SuppressAutoplay: suppress,
	}); errConnect != nil {
		return nil, fmt.Errorf("failed to join voice channel: %w", errConnect)
	}

	return voiceState, nil
}

func checkBotVoicePermissions(s *discordgo.Session, guildID, channelID string) error {
	if s == nil || s.State == nil || s.State.User == nil || s.State.User.ID == "" {
		return fmt.Errorf("cannot verify bot voice permissions right now")
	}

	channel, err := s.State.Channel(channelID)
	if err != nil || channel == nil || channel.GuildID != guildID {
		return fmt.Errorf("cannot verify permissions for that voice channel")
	}

	permissions, err := s.State.UserChannelPermissions(s.State.User.ID, channelID)
	if err != nil {
		return fmt.Errorf("cannot verify voice permissions for <#%s>", channelID)
	}

	missing := make([]string, 0, 2)
	if permissions&discordgo.PermissionVoiceConnect == 0 {
		missing = append(missing, "Connect")
	}
	if permissions&discordgo.PermissionVoiceSpeak == 0 {
		missing = append(missing, "Speak")
	}
	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("bot needs the %s permission%s in <#%s> before joining", strings.Join(missing, " and "), pluralSuffix(len(missing)), channelID)
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func (c *RadioGroupCmd) cleanAttachmentMessage(ctx *bot.Context) {
	if ctx.Message.GuildID != "" && len(ctx.Message.Attachments) > 0 {
		if mode, err := ctx.DB.GetGuildSettingString(ctx.Message.GuildID, database.SettingAntiMP3Mode); err == nil && mode != "" && mode != "disable" {
			time.AfterFunc(helpers.DurationFeedbackShort, func() {
				_ = ctx.Session.ChannelMessageDelete(ctx.Message.ChannelID, ctx.Message.ID)
			})
		}
	}
}

func (c *RadioGroupCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}

	if len(ctx.Args) == 0 {
		if len(ctx.Message.Attachments) > 0 {
			return c.handlePlayFile(ctx)
		}
		return c.handleNowPlaying(ctx)
	}

	subcommand := strings.ToLower(ctx.Args[0])
	args := ctx.Args[1:]

	switch subcommand {
	case "join", "connect", "j", "start":
		return c.handleJoin(ctx)
	case "play", "p":
		query := strings.Join(args, " ")
		lowerQuery := strings.ToLower(query)
		if strings.HasPrefix(lowerQuery, "session ") {
			return c.handlePlaySession(ctx, strings.TrimSpace(query[8:]))
		}
		if strings.HasPrefix(lowerQuery, "sess ") {
			return c.handlePlaySession(ctx, strings.TrimSpace(query[5:]))
		}
		if lowerQuery == "session" || lowerQuery == "sess" {
			return c.handlePlaySession(ctx, "")
		}
		return c.handlePlay(ctx, query)
	case "session", "sess", "sessionedit", "sessionedits":
		return c.handlePlaySession(ctx, strings.Join(args, " "))
	case "playnext", "pn":
		return c.handlePlayNext(ctx, strings.Join(args, " "))
	case "playlist", "pl", "playfile":
		return c.handlePlayFile(ctx)
	case "skip", "s", "next":
		return c.handleSkip(ctx)
	case "previous", "prev", "back":
		return c.handlePrevious(ctx)
	case "pause":
		return c.handlePause(ctx)
	case "resume":
		return c.handleResume(ctx)
	case "loop", "l":
		loopTarget := ""
		if len(args) > 0 {
			loopTarget = args[0]
		}
		return c.handleLoop(ctx, loopTarget)
	case "queue", "q":
		return c.handleQueue(ctx)
	case "shuffle", "sh", "shuf", "randomize":
		return c.handleShuffle(ctx)
	case "nowplaying", "np", "current":
		return c.handleNowPlaying(ctx)
	case "info", "songinfo", "i":
		return c.handleInfo(ctx)
	case "lyrics", "lyric", "lyr":
		return c.handleLyrics(ctx)
	case "clearqueue", "cq", "clear":
		return c.handleClearQueue(ctx)
	case "history", "hist":
		return c.handleHistory(ctx)
	case "mode", "era", "eras":
		modeName := ""
		if len(args) > 0 {
			modeName = args[0]
		}
		return c.handleMode(ctx, modeName)
	case "restriction", "perm", "permissions":
		mode := ""
		if len(args) > 0 {
			mode = args[0]
		}
		return c.handleRestriction(ctx, mode)
	case "setchannel", "setup", "autojoin":
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
		return c.handleSetChannel(ctx, target)
	case "setplayerchannel", "playerchannel", "setcontrollerchannel", "spc", "pc":
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
		return c.handleSetPlayerChannel(ctx, target)
	case "247", "24/7", "nonstop", "stay", "continuous":
		return c.handle247(ctx)
	case "leave", "stop", "disconnect", "dc":
		return c.handleLeave(ctx)
	case "forceleave", "fl", "reset", "leaveforce", "forcequit":
		return c.handleForceLeave(ctx)
	case "search":
		if len(args) == 0 {
			_, err := ctx.SendUsage(c)
			return err
		}
		rawQuery := strings.Join(args, " ")
		if clean, isURL := helpers.CleanMediaURL(rawQuery); isURL {
			return c.handlePlay(ctx, clean)
		}
		return c.handleInteractiveSearch(ctx, "soundcloud", rawQuery)
	case "sc", "scsearch", "soundcloud":
		if len(args) == 0 {
			_, err := ctx.SendUsage(c)
			return err
		}
		rawQuery := strings.Join(args, " ")
		if clean, isURL := helpers.CleanMediaURL(rawQuery); isURL {
			return c.handlePlay(ctx, clean)
		}
		return c.handleInteractiveSearch(ctx, "soundcloud", rawQuery)
	case "yt", "ytsearch", "youtube":
		if len(args) == 0 {
			_, err := ctx.SendUsage(c)
			return err
		}
		rawQuery := strings.Join(args, " ")
		if clean, isURL := helpers.CleanMediaURL(rawQuery); isURL {
			return c.handlePlay(ctx, clean)
		}
		return c.handleInteractiveSearch(ctx, "youtube", rawQuery)
	default:
		if len(ctx.Args) >= 1 {
			if sug, isTypo := isSubcommandTypo(subcommand); isTypo {
				return ctx.SendError(fmt.Sprintf("Unknown subcommand `%s`. Did you mean **%s**?\nTo search for a song, use `%sr play <query>`.", subcommand, sug, ctx.Prefix))
			}
		}
		return c.handlePlay(ctx, strings.Join(ctx.Args, " "))
	}
}

func isSubcommandTypo(sub string) (string, bool) {
	sub = strings.ToLower(sub)
	if len(sub) < 3 {
		return "", false
	}
	known := []string{
		"join", "connect", "play", "playnext", "playlist", "playfile",
		"skip", "previous", "pause", "resume", "loop",
		"queue", "nowplaying", "clearqueue", "history", "mode",
		"restriction", "setchannel", "leave", "forceleave", "reset", "session",
		"search", "soundcloud", "youtube",
	}
	var bestMatch string
	minDiff := 999
	for _, cmd := range known {
		if strings.HasPrefix(cmd, sub) {
			diff := len(cmd) - len(sub)
			if diff < minDiff {
				minDiff = diff
				bestMatch = cmd
			}
		}
	}
	if bestMatch != "" {
		return bestMatch, true
	}
	return "", false
}

func GetUserVoiceState(s *discordgo.Session, guildID, userID string) (*discordgo.VoiceState, error) {
	if s == nil || guildID == "" || userID == "" {
		return nil, fmt.Errorf("invalid guild or user ID")
	}

	if s.State != nil {
		if vs, err := s.State.VoiceState(guildID, userID); err == nil && vs != nil && vs.ChannelID != "" {
			return vs, nil
		}
		guild, err := s.State.Guild(guildID)
		if err == nil && guild != nil {
			for _, vs := range guild.VoiceStates {
				if vs != nil && vs.UserID == userID && vs.ChannelID != "" {
					return vs, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("user not in voice channel")
}

func CheckUserAccess(ctx *bot.Context) (bool, string) {
	isAdmin, _ := ctx.HasPermission(discordgo.PermissionAdministrator)
	canManageServer, _ := ctx.HasPermission(discordgo.PermissionManageGuild)
	isGuildOwner := ctx.IsGuildOwner()
	canManageMessages, _ := ctx.HasPermission(discordgo.PermissionManageMessages)
	isPrivileged := isAdmin || canManageServer || ctx.IsOwner() || isGuildOwner || canManageMessages

	voiceState, _ := GetUserVoiceState(ctx.Session, ctx.Message.GuildID, ctx.Message.Author.ID)
	userVoiceChannelID := ""
	if voiceState != nil {
		userVoiceChannelID = voiceState.ChannelID
	}

	if ctx.Radio != nil {
		return ctx.Radio.CanControl(ctx.Message.GuildID, userVoiceChannelID, isPrivileged)
	}

	if isPrivileged {
		return true, ""
	}
	if userVoiceChannelID == "" {
		return false, "You must be connected to a voice channel to use radio controls."
	}
	return true, ""
}

var TempNotifMsgs sync.Map

func SendSelfDeletingEmbed(ctx *bot.Context, text string, color int, delay ...time.Duration) error {
	d := helpers.DurationFeedbackShort
	if len(delay) > 0 && delay[0] > 0 {
		d = delay[0]
	}
	embed := &discordgo.MessageEmbed{
		Description: text,
		Color:       color,
	}
	msg, err := ctx.ReplyEmbed(embed)
	if err == nil && msg != nil {
		TempNotifMsgs.Store(msg.ID, time.Now())
		time.AfterFunc(d, func() {
			_ = ctx.Session.ChannelMessageDelete(msg.ChannelID, msg.ID)
			TempNotifMsgs.Delete(msg.ID)
		})
	}
	return err
}

func SendSelfDeletingEmbedFromSession(s *discordgo.Session, channelID, text string, color int, delay ...time.Duration) {
	d := helpers.DurationFeedbackShort
	if len(delay) > 0 && delay[0] > 0 {
		d = delay[0]
	}
	embed := &discordgo.MessageEmbed{
		Description: text,
		Color:       color,
	}
	msg, err := s.ChannelMessageSendEmbed(channelID, embed)
	if err == nil && msg != nil {
		TempNotifMsgs.Store(msg.ID, time.Now())
		time.AfterFunc(d, func() {
			_ = s.ChannelMessageDelete(msg.ChannelID, msg.ID)
			TempNotifMsgs.Delete(msg.ID)
		})
	}
}
