package radio

import (
	"fmt"
	"net/url"
	"strings"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

func formatTrackMarkdown(t *Track) string {
	if t == nil {
		return "Unknown Track"
	}
	webURL := t.WebpageURL
	if webURL == "" || !strings.HasPrefix(webURL, "http") {
		webURL = fmt.Sprintf("https://juicewrldapi.com/song/%s", url.QueryEscape(t.Title))
	}
	return fmt.Sprintf("[%s](%s)", t.Title, webURL)
}

func (c *RadioGroupCmd) handleSkip(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	skipped, msg, err := ProcessVoteSkip(radio, ctx.Session, ctx.Message.GuildID, ctx.Message.ChannelID, ctx.Message.Author.ID, ctx.Message.Author.Username)
	if err != nil {
		return ctx.SendError(msg)
	}
	if skipped {
		return SendSelfDeletingEmbed(ctx, msg, helpers.ColorPending, helpers.DurationFeedbackShort)
	}
	return SendSelfDeletingEmbed(ctx, msg, helpers.ColorDefault, helpers.DurationFeedbackShort)
}

func (c *RadioGroupCmd) handlePrevious(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	res, err := radio.Control(ctx.Context(), coordinator.ControlRequest{
		GuildID: ctx.Message.GuildID,
		Action:  coordinator.ControlPrevious,
	})
	if err != nil {
		return ctx.SendError("No previous tracks found in history.")
	}
	if res.Snapshot.Current != nil {
		return SendSelfDeletingEmbed(ctx, fmt.Sprintf("Returned to previous track **%s** by **%s**.", res.Snapshot.Current.Title, ctx.Message.Author.Username), helpers.ColorSuccess, helpers.DurationFeedbackShort)
	}
	return SendSelfDeletingEmbed(ctx, fmt.Sprintf("Returned to previous track by **%s**.", ctx.Message.Author.Username), helpers.ColorSuccess, helpers.DurationFeedbackShort)
}

func (c *RadioGroupCmd) handlePause(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	if snap, ok := radio.Snapshot(ctx.Message.GuildID); !ok || snap.Current == nil {
		return ctx.SendError("There is no track currently playing.")
	} else if snap.Phase == coordinator.PhasePaused {
		return ctx.SendError("Radio is already paused.")
	}
	if _, err = radio.Control(ctx.Context(), coordinator.ControlRequest{
		GuildID: ctx.Message.GuildID,
		Action:  coordinator.ControlPause,
	}); err != nil {
		return ctx.SendError("There is no track currently playing.")
	}
	return SendSelfDeletingEmbed(ctx, fmt.Sprintf("Paused by **%s**.", ctx.Message.Author.Username), helpers.ColorWarn, helpers.DurationFeedbackShort)
}

func (c *RadioGroupCmd) handleResume(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	if snap, ok := radio.Snapshot(ctx.Message.GuildID); !ok || snap.Current == nil {
		return ctx.SendError("There is no track currently playing.")
	} else if snap.Phase != coordinator.PhasePaused {
		return ctx.SendError("Radio is not paused.")
	}
	if _, err = radio.Control(ctx.Context(), coordinator.ControlRequest{
		GuildID: ctx.Message.GuildID,
		Action:  coordinator.ControlResume,
	}); err != nil {
		return ctx.SendError("Radio is not paused.")
	}
	return SendSelfDeletingEmbed(ctx, fmt.Sprintf("Resumed by **%s**.", ctx.Message.Author.Username), helpers.ColorSuccess, helpers.DurationFeedbackShort)
}

func (c *RadioGroupCmd) handleLoop(ctx *bot.Context, target string) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	var newMode coordinator.LoopMode = coordinator.LoopOff
	action := coordinator.ControlLoop
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "track", "song", "current", "1", "single":
		action = coordinator.ControlLoopTrack
		newMode = coordinator.LoopTrack
	case "queue", "all", "q":
		action = coordinator.ControlLoopQueue
		newMode = coordinator.LoopQueue
	case "off", "disable", "stop", "reset", "none", "0":
		action = coordinator.ControlLoopOff
		newMode = coordinator.LoopOff
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	res, err := radio.Control(ctx.Context(), coordinator.ControlRequest{
		GuildID: ctx.Message.GuildID,
		Action:  action,
	})
	if err != nil {
		return ctx.SendError("Radio is not currently active.")
	}
	newMode = res.Snapshot.LoopMode

	return SendSelfDeletingEmbed(ctx, fmt.Sprintf("%s set loop mode to **%s**.", newMode, ctx.Message.Author.Username), helpers.ColorInfo, helpers.DurationFeedbackShort)
}

func (c *RadioGroupCmd) handleQueue(ctx *bot.Context) error {
	var snap coordinator.Snapshot
	if ctx.Radio != nil {
		if s, ok := ctx.Radio.Snapshot(ctx.Message.GuildID); ok {
			snap = s
		}
	}

	components := BuildRadioCV2QueueCard(snap, 1)
	return helpers.SendCV2Message(ctx.Session, ctx.Message.ChannelID, components, nil)
}

func (c *RadioGroupCmd) handleNowPlaying(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	snap, ok := radio.Snapshot(ctx.Message.GuildID)

	if !ok || snap.Current == nil {
		embed := &discordgo.MessageEmbed{
			Description: "No song is currently playing on the radio.",
			Color:       helpers.ColorDefault,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}
	_, err = radio.ShowController(ctx.Context(), ctx.Message.GuildID, ctx.Message.ChannelID)
	return err
}

func (c *RadioGroupCmd) handleShuffle(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	queueLen := 0
	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	res, err := radio.Control(ctx.Context(), coordinator.ControlRequest{
		GuildID: ctx.Message.GuildID,
		Action:  coordinator.ControlShuffle,
	})
	if err != nil {
		return ctx.SendError("The queue is empty. Add songs to the queue before shuffling.")
	}
	queueLen = len(res.Snapshot.Queue)

	if queueLen <= 1 {
		if queueLen == 0 {
			return ctx.SendError("The queue is empty. Add songs to the queue before shuffling.")
		}
		return ctx.SendError("The queue contains only one track. Cannot shuffle.")
	}

	return SendSelfDeletingEmbed(ctx, fmt.Sprintf("Shuffled **%d** tracks in the queue.", queueLen), helpers.ColorSuccess, helpers.DurationFeedbackShort)
}

func (c *RadioGroupCmd) handleClearQueue(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	if _, err = radio.Control(ctx.Context(), coordinator.ControlRequest{
		GuildID: ctx.Message.GuildID,
		Action:  coordinator.ControlClearQueue,
	}); err != nil {
		return ctx.SendError("The queue is already empty or radio is not active.")
	}

	embed := &discordgo.MessageEmbed{
		Description: "Cleared all upcoming tracks from queue.",
		Color:       helpers.ColorError,
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func (c *RadioGroupCmd) handleHistory(ctx *bot.Context) error {
	var snap coordinator.Snapshot
	if ctx.Radio != nil {
		if s, ok := ctx.Radio.Snapshot(ctx.Message.GuildID); ok {
			snap = s
		}
	}

	if len(snap.History) == 0 {
		embed := &discordgo.MessageEmbed{
			Description: "No track history recorded for this session.",
			Color:       helpers.ColorDefault,
		}
		_, err := ctx.ReplyEmbed(embed)
		return err
	}

	var sb strings.Builder
	sb.WriteString("**RECENTLY PLAYED TRACKS:**\n")
	start := 0
	if len(snap.History) > 15 {
		start = len(snap.History) - 15
	}
	for i, track := range snap.History[start:] {
		sb.WriteString(fmt.Sprintf("`%02d.` **%s**\n", i+1, formatTrackMarkdown(&track)))
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Session Track History",
		Description: sb.String(),
		Color:       helpers.ColorDefault,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}

func (c *RadioGroupCmd) handleLeave(ctx *bot.Context) error {
	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}

	radio, err := requireRadio(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	if _, err = radio.Control(ctx.Context(), coordinator.ControlRequest{
		GuildID: ctx.Message.GuildID,
		Action:  coordinator.ControlLeave,
	}); err != nil {
		return ctx.SendError("Radio is not currently connected to a voice channel.")
	}

	embed := &discordgo.MessageEmbed{
		Description: "Disconnected radio from voice channel.",
		Color:       helpers.ColorError,
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}
