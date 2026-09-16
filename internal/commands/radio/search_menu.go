package radio

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/juicewrld"
	"gobot/internal/policy"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

func respondEnqueueError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	if s == nil || i == nil || i.Interaction == nil {
		return
	}
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{{Description: message, Color: helpers.ColorDarkRed}},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})
}

func enqueueSelectedTrackWithStatus(ctx *bot.Context, selectedTrack *Track) (bool, error) {
	if selectedTrack == nil {
		return false, fmt.Errorf("radio is not available")
	}
	return enqueueRadioTracksWithDuplicateConfirmation(ctx, []coordinator.Track{selectedTrack.Clone()}, coordinator.EnqueueBack)
}

type selectMenuItem struct {
	label       string
	description string
	track       *Track
}

func showInteractiveTrackSelector(ctx *bot.Context, title, placeholder string, thumbnail string, items []selectMenuItem) error {
	options := buildTrackSelectOptions(items)
	selectMenuID := fmt.Sprintf("radio_sel_%s_%d", ctx.Message.ID, time.Now().UnixNano())
	selectMenu := discordgo.SelectMenu{
		CustomID:    selectMenuID,
		Placeholder: placeholder,
		Options:     options,
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: fmt.Sprintf("Found **%d** matches. Please select which track to queue below:", len(items)),
		Color:       helpers.ColorDefault,
	}
	if thumbnail != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: thumbnail}
	}

	msg, err := ctx.Session.ChannelMessageSendComplex(ctx.Message.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{Components: []discordgo.MessageComponent{selectMenu}},
		},
	})
	if err != nil {
		return err
	}

	var removeOnce sync.Once
	var removeHandler func()
	cleanup := func() {
		removeOnce.Do(func() {
			if removeHandler != nil {
				removeHandler()
			}
		})
	}

	removeHandler = ctx.Session.AddHandler(newTrackSelectInteractionHandler(ctx, msg.ID, selectMenuID, items, cleanup))

	time.AfterFunc(60*time.Second, func() {
		cleanup()
		_ = ctx.Session.ChannelMessageDelete(msg.ChannelID, msg.ID)
	})

	return nil
}

func buildTrackSelectOptions(items []selectMenuItem) []discordgo.SelectMenuOption {
	limit := len(items)
	if limit > 25 {
		limit = 25
	}
	options := make([]discordgo.SelectMenuOption, 0, limit)
	for i := 0; i < limit; i++ {
		item := items[i]
		options = append(options, discordgo.SelectMenuOption{
			Label:       helpers.TruncateString(item.label, 90),
			Value:       strconv.Itoa(i),
			Description: helpers.TruncateString(item.description, 90),
		})
	}
	return options
}

func newTrackSelectInteractionHandler(
	ctx *bot.Context,
	msgID, selectMenuID string,
	items []selectMenuItem,
	cleanup func(),
) func(*discordgo.Session, *discordgo.InteractionCreate) {
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionMessageComponent || i.Message == nil || i.Message.ID != msgID {
			return
		}

		var userID string
		var memberRoles []string
		if i.Member != nil {
			memberRoles = i.Member.Roles
			if i.Member.User != nil {
				userID = i.Member.User.ID
			}
		}
		if userID == "" && i.User != nil {
			userID = i.User.ID
		}
		if userID == "" || userID != ctx.Message.Author.ID {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Only the command author can select a track.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		res := ctx.Policy.CanExecute(policy.ExecutionRequest{
			GuildID:       i.GuildID,
			ChannelID:     i.ChannelID,
			UserID:        userID,
			MemberRoleIDs: memberRoles,
		})
		if !res.Allowed() {
			return
		}

		data := i.MessageComponentData()
		if data.CustomID == selectMenuID && len(data.Values) > 0 {
			idx, err := strconv.Atoi(data.Values[0])
			if err == nil && idx >= 0 && idx < len(items) {
				cleanup()
				handleSelectedTrackExecution(ctx, s, i, msgID, items[idx].track)
			}
		}
	}
}

func handleSelectedTrackExecution(ctx *bot.Context, s *discordgo.Session, i *discordgo.InteractionCreate, msgID string, selectedTrack *Track) {
	if errDur := coordinator.ValidateTrackDuration(selectedTrack); errDur != nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{
					{
						Description: errDur.Error(),
						Color:       helpers.ColorDarkRed,
					},
				},
				Flags: discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	prompted, err := enqueueSelectedTrackWithStatus(ctx, selectedTrack)
	if err != nil {
		respondEnqueueError(s, i, queueEnqueueErrorMessage("Failed to queue track", err))
		return
	}
	if prompted {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{{
					Description: "Duplicate track detected. Use the **Queue anyway** confirmation sent below to continue.",
					Color:       helpers.ColorPending,
				}},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{
				{
					Description: fmt.Sprintf("Queued **[%s](%s)** (by **%s**).", selectedTrack.Title, selectedTrack.WebpageURL, ctx.Message.Author.Username),
					Color:       helpers.ColorDefault,
				},
			},
			Components: []discordgo.MessageComponent{},
		},
	})
	time.AfterFunc(helpers.DurationFeedbackShort, func() {
		_ = s.ChannelMessageDelete(i.ChannelID, msgID)
	})
}

func handleMultiSongSelection(ctx *bot.Context, candidates []*Track) error {
	items := make([]selectMenuItem, 0, len(candidates))
	for _, c := range candidates {
		desc := c.Uploader
		if c.Era != "" {
			desc += fmt.Sprintf(" • Era: %s", c.Era)
		}
		items = append(items, selectMenuItem{
			label:       c.Title,
			description: desc,
			track:       c,
		})
	}

	return showInteractiveTrackSelector(ctx, "Multiple Matches Found", "Select a song version to queue...", "", items)
}

func handleSessionEditSelection(ctx *bot.Context, song juicewrld.Song, edits []juicewrld.SessionEditItem, thumbnail, eraStr string) error {
	dur := resolveTrackDuration(song.Length)
	if dur <= 0 {
		dur = 180
	}

	items := make([]selectMenuItem, 0, len(edits))
	for i, edit := range edits {
		label, desc := juicewrld.FormatSessionEditOption(song, edit, i)
		track := &Track{
			Title:      formatSessionEditTitle(song.GetName(), edit.Name),
			URL:        edit.URL,
			WebpageURL: edit.URL,
			Uploader:   "Juice WRLD (Studio Session)",
			Thumbnail:  thumbnail,
			Duration:   dur,
			Era:        eraStr,
			Category:   "recording_session",
		}
		items = append(items, selectMenuItem{
			label:       label,
			description: desc,
			track:       track,
		})
	}

	title := fmt.Sprintf("%s - Session Edits", song.GetName())
	return showInteractiveTrackSelector(ctx, title, "Select a session edit file to queue...", thumbnail, items)
}

func (c *RadioGroupCmd) handleInteractiveSearch(ctx *bot.Context, platform, query string) error {
	if clean, isURL := helpers.CleanMediaURL(query); isURL {
		return c.handlePlay(ctx, clean)
	}

	if ok, msg := CheckUserAccess(ctx); !ok {
		return ctx.SendError(msg)
	}
	if _, err := requireRadio(ctx); err != nil {
		return ctx.SendError(err.Error())
	}
	if _, err := c.ensureConnected(ctx, true); err != nil {
		return ctx.SendError(err.Error())
	}

	query = strings.TrimSpace(query)
	platformTitle := "SoundCloud"
	source := "sc"
	if strings.EqualFold(platform, "youtube") || strings.EqualFold(platform, "yt") {
		platformTitle = "YouTube"
		source = "yt"
	}

	tracks, errSearch := coordinator.SearchMultipleTracks(ctx.Context(), source, query, 5)
	if errSearch != nil {
		return ctx.SendError(interactiveSearchErrorMessage(platformTitle, query, errSearch))
	}
	if len(tracks) == 0 {
		return ctx.SendError(fmt.Sprintf("No results found on **%s** for `%s`.", platformTitle, helpers.EscapeMarkdown(query)))
	}

	items := make([]selectMenuItem, 0, len(tracks))
	for i, t := range tracks {
		durStr := FormatTime(float64(t.Duration))
		if t.Duration <= 0 {
			durStr = "Live"
		}
		items = append(items, selectMenuItem{
			label:       fmt.Sprintf("%d. %s", i+1, t.Title),
			description: fmt.Sprintf("%s [%s]", t.Uploader, durStr),
			track:       t,
		})
	}

	title := fmt.Sprintf("%s Search Results", platformTitle)
	placeholder := fmt.Sprintf("Select a %s track to queue...", platformTitle)
	return showInteractiveTrackSelector(ctx, title, placeholder, "", items)
}

func interactiveSearchErrorMessage(source, query string, err error) string {
	var searchErr *coordinator.SearchError
	if !errors.As(err, &searchErr) {
		return fmt.Sprintf("%s search failed for `%s`. Please try again.", source, helpers.EscapeMarkdown(query))
	}

	cleanQuery := helpers.EscapeMarkdown(query)
	switch searchErr.Class {
	case coordinator.SearchFailureEmpty:
		return fmt.Sprintf("No results found on **%s** for `%s`.", source, cleanQuery)
	case coordinator.SearchFailureExecutableUnavailable:
		return fmt.Sprintf("**%s** search is unavailable because the search helper is not installed or configured.", source)
	case coordinator.SearchFailureExecutablePermission:
		return fmt.Sprintf("**%s** search is unavailable because the search helper cannot run.", source)
	case coordinator.SearchFailureDeadline:
		return fmt.Sprintf("**%s** search timed out. Please try again.", source)
	default:
		return fmt.Sprintf("**%s** search failed while looking up `%s`. Please try again.", source, cleanQuery)
	}
}
