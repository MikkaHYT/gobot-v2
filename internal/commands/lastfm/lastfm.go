package lastfm

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/graphics"
	"gobot/internal/helpers"
	lfm "gobot/internal/services/lastfm"

	"github.com/bwmarrin/discordgo"
)

var Commands = []commands.Command{
	&LfGroupCmd{},
	&NowPlayingCmd{},
	&RecentsCmd{},
	&WhoKnowsCmd{},
	&WhoKnowsAlbumCmd{},
	&WhoKnowsTrackCmd{},
	&ChartCmd{},
	&TasteCmd{},
	&CrownsCmd{},
}

func getClient(ctx *bot.Context) (*lfm.Client, error) {
	apiKey := ""
	if ctx != nil && ctx.Config != nil {
		apiKey = strings.TrimSpace(ctx.Config.LastFmAPIKey)
	}
	if apiKey == "" {
		return nil, errors.New("last.fm integration is not configured on this bot (missing API key)")
	}
	return lfm.NewClient(apiKey), nil
}

var TimeframeMap = map[string]struct {
	Period string
	Label  string
}{
	"7d":      {"7day", "Weekly"},
	"7day":    {"7day", "Weekly"},
	"w":       {"7day", "Weekly"},
	"week":    {"7day", "Weekly"},
	"weekly":  {"7day", "Weekly"},
	"1m":      {"1month", "Monthly"},
	"1month":  {"1month", "Monthly"},
	"m":       {"1month", "Monthly"},
	"month":   {"1month", "Monthly"},
	"monthly": {"1month", "Monthly"},
	"3m":      {"3month", "3 Months"},
	"3month":  {"3month", "3 Months"},
	"6m":      {"6month", "6 Months"},
	"6month":  {"6month", "6 Months"},
	"12m":     {"12month", "Yearly"},
	"12month": {"12month", "Yearly"},
	"1y":      {"12month", "Yearly"},
	"y":       {"12month", "Yearly"},
	"year":    {"12month", "Yearly"},
	"yearly":  {"12month", "Yearly"},
	"overall": {"overall", "Overall"},
	"all":     {"overall", "Overall"},
	"alltime": {"overall", "Overall"},
	"o":       {"overall", "Overall"},
}

func ParseTimeframeArgs(args []string) (string, string, []string) {
	period := "overall"
	label := "Overall"
	var clean []string

	for _, arg := range args {
		low := strings.ToLower(strings.TrimSpace(arg))
		if tf, ok := TimeframeMap[low]; ok {
			period = tf.Period
			label = tf.Label
		} else {
			clean = append(clean, arg)
		}
	}
	return period, label, clean
}
func ResolveLastfmUsername(ctx *bot.Context, userArg string) (string, *discordgo.User, error) {
	if ctx.DB == nil {
		return "", nil, fmt.Errorf("database connection unavailable")
	}

	userArg = strings.TrimSpace(userArg)
	if userArg == "" {
		target := ctx.Message.Author
		uname, err := ctx.DB.GetLastfmUsername(target.ID)
		if err != nil || uname == "" {
			return "", target, fmt.Errorf("you have not set a Last.fm username. Use `%slf set <username>`", ctx.Prefix)
		}
		return uname, target, nil
	}

	userID := ctx.ParseUserID(userArg)
	if len(userID) >= 16 {
		u, err := ctx.Session.User(userID)
		if err == nil && u != nil {
			uname, dbErr := ctx.DB.GetLastfmUsername(u.ID)
			if dbErr != nil || uname == "" {
				if u.ID == ctx.Message.Author.ID {
					return "", u, fmt.Errorf("you have not set a Last.fm username. Use `%slf set <username>`", ctx.Prefix)
				}
				return "", u, fmt.Errorf("**%s** has not set a Last.fm username", u.Username)
			}
			return uname, u, nil
		}
	}

	return userArg, nil, nil
}

func userAvatarURL(target *discordgo.User, fallback *discordgo.User) string {
	if target != nil {
		return helpers.UserAvatar(target)
	}
	if fallback != nil {
		return helpers.UserAvatar(fallback)
	}
	return ""
}

type LfGroupCmd struct{}

func (c *LfGroupCmd) Name() string      { return "lf" }
func (c *LfGroupCmd) Aliases() []string { return []string{"setlf", "lastfm"} }
func (c *LfGroupCmd) Category() string  { return "Last.fm" }
func (c *LfGroupCmd) Description() string {
	return "Last.fm scrobble & statistics"
}
func (c *LfGroupCmd) Usage() string   { return "[subcommand] [args]" }
func (c *LfGroupCmd) Example() string { return "set Cloudyy" }

func (c *LfGroupCmd) Subcommands() []commands.Subcommand {
	return []commands.Subcommand{
		{Name: "set", Description: "Link your Last.fm account", Usage: "<username>", Example: "MyLastFMUser"},
		{Name: "unset", Description: "Unlink your Last.fm account", Usage: "", Example: ""},
		{Name: "profile", Description: "View Last.fm profile details", Usage: "[@user]", Example: "@Cloudyy"},
		{Name: "info", Description: "View scrobble statistics and artist info", Usage: "<artist>", Example: "Juice WRLD"},
		{Name: "artists", Description: "View top artists", Usage: "[timeframe] [@user]", Example: "7d"},
		{Name: "albums", Description: "View top albums", Usage: "[timeframe] [@user]", Example: "1m"},
		{Name: "tracks", Description: "View top tracks", Usage: "[timeframe] [@user]", Example: "overall"},
		{Name: "chart", Description: "Generate a visual grid collage of your top albums", Usage: "[3x3|4x4|5x5] [timeframe] [@user]", Example: "3x3 7d"},
	}
}

func (c *LfGroupCmd) Execute(ctx *bot.Context) error {
	sub := ctx.Subcommand()
	subArgs := ctx.SubArgs()
	reqCtx := ctx.Context()

	var err error
	switch sub {
	case "set":
		if !ctx.RequireSubArgs(1, "lf set <username>") {
			return nil
		}
		username := strings.TrimSpace(subArgs[0])
		client, clientErr := getClient(ctx)
		if clientErr != nil {
			err = clientErr
			break
		}
		if _, getErr := client.GetUserInfo(reqCtx, username); getErr != nil {
			err = fmt.Errorf("could not find Last.fm user **%s**. Ensure the account exists", username)
			break
		}
		if dbErr := ctx.DB.SetLastfmUsername(ctx.Message.Author.ID, username); dbErr != nil {
			err = fmt.Errorf("failed to save Last.fm username: %v", dbErr)
			break
		}
		return ctx.SendSuccess("Your Last.fm username has been set to **%s**.", username)

	case "remove", "rm", "delete", "unset":
		if dbErr := ctx.DB.DeleteLastfmUsername(ctx.Message.Author.ID); dbErr != nil {
			err = fmt.Errorf("you do not have a Last.fm username set")
			break
		}
		return ctx.SendSuccess("Your Last.fm username has been removed.")

	case "profile", "user", "stats":
		err = handleProfile(ctx, strings.Join(subArgs, " "))

	case "albums", "topalbums", "ab", "ta":
		err = handleAlbums(ctx, subArgs)

	case "artists", "topartists", "ar", "tar":
		err = handleArtists(ctx, subArgs)

	case "tracks", "toptracks", "tr", "tt":
		err = handleTracks(ctx, subArgs)

	case "info", "artistinfo", "ai":
		if !ctx.RequireSubArgs(1, "lf info <artist>") {
			return nil
		}
		err = handleArtistInfo(ctx, strings.Join(subArgs, " "))

	case "":
		embed := &discordgo.MessageEmbed{
			Author: &discordgo.MessageEmbedAuthor{
				Name:    fmt.Sprintf("%s - Last.fm Commands", ctx.Session.State.User.Username),
				IconURL: helpers.UserAvatar(ctx.Session.State.User),
			},
			Description: fmt.Sprintf(
				"**Available Last.fm Commands:**\n\n"+
					"` %slf set <username> ` - Set your Last.fm username\n"+
					"` %slf remove ` - Remove your saved Last.fm username\n"+
					"` %slf profile [user] ` - View Last.fm profile stats\n"+
					"` %slf albums [timeframe] [user] ` - View top 10 albums\n"+
					"` %slf artists [timeframe] [user] ` - View top 10 artists\n"+
					"` %slf tracks [timeframe] [user] ` - View top 10 tracks\n"+
					"` %slf info <artist> ` - View artist info & scrobbles\n"+
					"` %srecents [user] ` - View recent scrobble history\n"+
					"` %swhoknows <artist> ` - Server artist scrobble leaderboard\n"+
					"` %snp [user] ` - Show current/last played track",
				ctx.Prefix, ctx.Prefix, ctx.Prefix, ctx.Prefix, ctx.Prefix, ctx.Prefix, ctx.Prefix, ctx.Prefix, ctx.Prefix, ctx.Prefix,
			),
		}
		_, err = ctx.ReplyEmbed(embed)
		return err

	default:
		err = fmt.Errorf("unknown subcommand `%s`. Use `%slf` to see all commands", sub, ctx.Prefix)
	}

	if err != nil {
		return ctx.SendError(err.Error())
	}
	return nil
}

func handleProfile(ctx *bot.Context, userArg string) error {
	client, err := getClient(ctx)
	if err != nil {
		return err
	}
	uname, target, err := ResolveLastfmUsername(ctx, userArg)
	if err != nil {
		return err
	}
	user, err := client.GetUserInfo(ctx.Context(), uname)
	if err != nil {
		return fmt.Errorf("failed to fetch profile: %w", err)
	}

	profileURL := user.URL
	if profileURL == "" {
		profileURL = fmt.Sprintf("https://www.last.fm/user/%s", uname)
	}

	desc := []string{
		fmt.Sprintf("**Total Scrobbles:** %s", user.DisplayPlaycount),
	}
	if user.ArtistCount != "" {
		if c, cErr := strconv.Atoi(string(user.ArtistCount)); cErr == nil {
			desc = append(desc, fmt.Sprintf("**Artists Scrobbled:** %s", helpers.FormatNumber(c)))
		}
	}
	if user.TrackCount != "" {
		if c, cErr := strconv.Atoi(string(user.TrackCount)); cErr == nil {
			desc = append(desc, fmt.Sprintf("**Tracks Scrobbled:** %s", helpers.FormatNumber(c)))
		}
	}
	if user.AlbumCount != "" {
		if c, cErr := strconv.Atoi(string(user.AlbumCount)); cErr == nil {
			desc = append(desc, fmt.Sprintf("**Albums Scrobbled:** %s", helpers.FormatNumber(c)))
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s's Profile", user.DisplayName),
		URL:         profileURL,
		Description: strings.Join(desc, "\n"),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s on Last.fm:", uname),
			IconURL: userAvatarURL(target, ctx.Message.Author),
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: user.AvatarURL,
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Registered: %s", user.RegisteredDate),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func handleAlbums(ctx *bot.Context, args []string) error {
	client, err := getClient(ctx)
	if err != nil {
		return err
	}
	period, label, clean := ParseTimeframeArgs(args)
	uname, target, err := ResolveLastfmUsername(ctx, strings.Join(clean, " "))
	if err != nil {
		return err
	}
	res, err := client.GetTopAlbums(ctx.Context(), uname, period, 10)
	if err != nil {
		return fmt.Errorf("failed to fetch top albums: %w", err)
	}

	albums := res.TopAlbums.Album
	if len(albums) == 0 {
		return fmt.Errorf("no top albums found for **%s** (`%s`)", uname, label)
	}

	var lines []string
	totalPlays := 0
	for i, ab := range albums {
		if i >= 10 {
			break
		}
		pc, _ := strconv.Atoi(ab.Playcount)
		totalPlays += pc
		lines = append(lines, fmt.Sprintf("`%d.` **%s** by %s `(%s)`", i+1, ab.DisplayTitle, ab.DisplayArtist, ab.FormattedPlays))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Top Albums (%s)", label),
		Description: strings.Join(lines, "\n"),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s on Last.fm:", uname),
			IconURL: userAvatarURL(target, ctx.Message.Author),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Total Plays: %s", helpers.FormatNumber(totalPlays)),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func handleArtists(ctx *bot.Context, args []string) error {
	client, err := getClient(ctx)
	if err != nil {
		return err
	}
	period, label, clean := ParseTimeframeArgs(args)
	uname, target, err := ResolveLastfmUsername(ctx, strings.Join(clean, " "))
	if err != nil {
		return err
	}
	res, err := client.GetTopArtists(ctx.Context(), uname, period, 10)
	if err != nil {
		return fmt.Errorf("failed to fetch top artists: %w", err)
	}

	artists := res.TopArtists.Artist
	if len(artists) == 0 {
		return fmt.Errorf("no top artists found for **%s** (`%s`)", uname, label)
	}

	var lines []string
	totalPlays := 0
	for i, ar := range artists {
		if i >= 10 {
			break
		}
		pc, _ := strconv.Atoi(ar.Playcount)
		totalPlays += pc
		lines = append(lines, fmt.Sprintf("`%d.` %s `(%s)`", i+1, ar.ArtistLink, ar.FormattedPlays))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Top Artists (%s)", label),
		Description: strings.Join(lines, "\n"),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s on Last.fm:", uname),
			IconURL: userAvatarURL(target, ctx.Message.Author),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Total Plays: %s", helpers.FormatNumber(totalPlays)),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func handleTracks(ctx *bot.Context, args []string) error {
	client, err := getClient(ctx)
	if err != nil {
		return err
	}
	period, label, clean := ParseTimeframeArgs(args)
	uname, target, err := ResolveLastfmUsername(ctx, strings.Join(clean, " "))
	if err != nil {
		return err
	}
	res, err := client.GetTopTracks(ctx.Context(), uname, period, 10)
	if err != nil {
		return fmt.Errorf("failed to fetch top tracks: %w", err)
	}

	tracks := res.TopTracks.Track
	if len(tracks) == 0 {
		return fmt.Errorf("no top tracks found for **%s** (`%s`)", uname, label)
	}

	var lines []string
	totalPlays := 0
	for i, tr := range tracks {
		if i >= 10 {
			break
		}
		pc, _ := strconv.Atoi(tr.Playcount)
		totalPlays += pc
		lines = append(lines, fmt.Sprintf("`%d.` %s by **%s** `(%s)`", i+1, tr.TrackLink, tr.DisplayArtist, tr.FormattedPlays))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("Top Tracks (%s)", label),
		Description: strings.Join(lines, "\n"),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s on Last.fm:", uname),
			IconURL: userAvatarURL(target, ctx.Message.Author),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Total Plays: %s", helpers.FormatNumber(totalPlays)),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

func handleArtistInfo(ctx *bot.Context, artistQuery string) error {
	client, err := getClient(ctx)
	if err != nil {
		return err
	}
	uname, _ := ctx.DB.GetLastfmUsername(ctx.Message.Author.ID)

	ar, err := client.GetArtistInfo(ctx.Context(), artistQuery, uname)
	if err != nil {
		return fmt.Errorf("failed to fetch artist info: %w", err)
	}

	bioClean := ar.CleanBio
	if len([]rune(bioClean)) > 300 {
		bioClean = helpers.TruncateStringWithEllipsis(bioClean, 300)
	}

	var tags []string
	for _, t := range ar.CleanTags {
		tags = append(tags, fmt.Sprintf("`#%s`", t))
	}
	tagLine := "None"
	if len(tags) > 0 {
		if len(tags) > 5 {
			tags = tags[:5]
		}
		tagLine = strings.Join(tags, " ")
	}

	embed := &discordgo.MessageEmbed{
		Title:       ar.DisplayName,
		URL:         ar.URL,
		Description: fmt.Sprintf("%s\n\n**Genres:** %s", bioClean, tagLine),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s - Artist Info", ctx.Message.Author.Username),
			IconURL: helpers.UserAvatar(ctx.Message.Author),
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Global Listeners", Value: ar.Stats.Listeners, Inline: true},
			{Name: "Global Scrobbles", Value: ar.Stats.Playcount, Inline: true},
		},
	}

	if ar.Stats.UserPlayCount != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Your Scrobbles",
			Value:  ar.Stats.UserPlayCount,
			Inline: true,
		})
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

type NowPlayingCmd struct{}

func (c *NowPlayingCmd) Name() string      { return "np" }
func (c *NowPlayingCmd) Aliases() []string { return []string{"nowplaying", "fm"} }
func (c *NowPlayingCmd) Category() string  { return "Last.fm" }
func (c *NowPlayingCmd) Description() string {
	return "Displays currently playing or last played track."
}
func (c *NowPlayingCmd) Usage() string   { return "[user]" }
func (c *NowPlayingCmd) Example() string { return "@Cloudyy" }

func (c *NowPlayingCmd) Execute(ctx *bot.Context) error {
	client, err := getClient(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	uname, target, err := ResolveLastfmUsername(ctx, strings.Join(ctx.Args, " "))
	if err != nil {
		return ctx.SendError(err.Error())
	}
	res, err := client.GetRecentTracks(ctx.Context(), uname, 1)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to fetch now playing: %v", err))
	}

	tracks := res.RecentTracks.Track
	if len(tracks) == 0 {
		return ctx.SendError(fmt.Sprintf("No recent tracks found for **%s**.", uname))
	}

	tr := tracks[0]
	totalScrobbles := res.RecentTracks.Attr.Total
	if totalScrobbles == "" {
		totalScrobbles = "0"
	}

	embed := &discordgo.MessageEmbed{
		Title: tr.StatusLabel,
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s on Last.fm:", uname),
			IconURL: userAvatarURL(target, ctx.Message.Author),
		},
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Track:", Value: fmt.Sprintf("%s by %s", tr.TrackLink, tr.ArtistLink), Inline: false},
			{Name: "Album:", Value: tr.DisplayAlbum, Inline: false},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Total Scrobbles: %s", totalScrobbles),
		},
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: tr.CoverURL,
		},
	}

	msg, err := ctx.ReplyEmbed(embed)
	if err == nil && msg != nil {
		_ = ctx.Session.MessageReactionAdd(msg.ChannelID, msg.ID, "👍")
		_ = ctx.Session.MessageReactionAdd(msg.ChannelID, msg.ID, "👎")
	}
	return err
}

type RecentsCmd struct{}

func (c *RecentsCmd) Name() string      { return "recents" }
func (c *RecentsCmd) Aliases() []string { return []string{"recent", "lfr"} }
func (c *RecentsCmd) Category() string  { return "Last.fm" }
func (c *RecentsCmd) Description() string {
	return "Displays the last 10 scrobbled tracks."
}
func (c *RecentsCmd) Usage() string   { return "[user]" }
func (c *RecentsCmd) Example() string { return "@Cloudyy" }

func (c *RecentsCmd) Execute(ctx *bot.Context) error {
	client, err := getClient(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	uname, target, err := ResolveLastfmUsername(ctx, strings.Join(ctx.Args, " "))
	if err != nil {
		return ctx.SendError(err.Error())
	}
	res, err := client.GetRecentTracks(ctx.Context(), uname, 10)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to fetch recent tracks: %v", err))
	}

	tracks := res.RecentTracks.Track
	if len(tracks) == 0 {
		return ctx.SendError(fmt.Sprintf("No recent tracks found for **%s**.", uname))
	}

	var lines []string
	for i, tr := range tracks {
		if i >= 10 {
			break
		}
		line := fmt.Sprintf("`%d.` %s by %s", i+1, tr.TrackLink, tr.ArtistLink)
		if tr.Attr.NowPlaying == "true" {
			line += " *(Now Playing)*"
		}
		lines = append(lines, line)
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Recent Scrobbles",
		Description: strings.Join(lines, "\n"),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s on Last.fm:", uname),
			IconURL: userAvatarURL(target, ctx.Message.Author),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type ChartCmd struct{}

func (c *ChartCmd) Name() string      { return "chart" }
func (c *ChartCmd) Aliases() []string { return []string{"grid", "c"} }
func (c *ChartCmd) Category() string  { return "Last.fm" }
func (c *ChartCmd) Description() string {
	return "Creates an album cover grid collage."
}
func (c *ChartCmd) Usage() string   { return "[3x3 / 4x4 / 5x5] [timeframe] [user]" }
func (c *ChartCmd) Example() string { return "3x3 7d @Cloudyy" }

func (c *ChartCmd) Execute(ctx *bot.Context) error {
	gridSize := 3
	var cleanArgs []string

	for _, arg := range ctx.Args {
		low := strings.ToLower(arg)
		if low == "3x3" || low == "3" {
			gridSize = 3
		} else if low == "4x4" || low == "4" {
			gridSize = 4
		} else if low == "5x5" || low == "5" {
			gridSize = 5
		} else {
			cleanArgs = append(cleanArgs, arg)
		}
	}

	period, label, userTokens := ParseTimeframeArgs(cleanArgs)
	uname, target, err := ResolveLastfmUsername(ctx, strings.Join(userTokens, " "))
	if err != nil {
		return ctx.SendError(err.Error())
	}

	totalNeeded := gridSize * gridSize
	client, err := getClient(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}
	res, err := client.GetTopAlbums(ctx.Context(), uname, period, totalNeeded)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to fetch top albums for chart: %v", err))
	}
	albums := res.TopAlbums.Album
	if len(albums) == 0 {
		return ctx.SendError(fmt.Sprintf("No top albums found for **%s** (`%s`).", uname, label))
	}

	imgURLs := make([]string, len(albums))
	var wg sync.WaitGroup

	workerSem := make(chan struct{}, 5)

	for i, ab := range albums {
		idx := i
		album := ab
		wg.Add(1)
		helpers.Spawn(func() {
			defer wg.Done()
			url := album.CoverURL
			if url == "" {
				workerSem <- struct{}{}
				url, _ = client.FetchiTunesAlbumCover(ctx.Context(), album.DisplayArtist, album.DisplayTitle)
				<-workerSem
			}
			imgURLs[idx] = url
		})
	}

	wg.Wait()

	gen := graphics.NewChartGenerator()
	pngData, err := gen.BuildGridChart(imgURLs, gridSize)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to generate chart image: %v", err))
	}

	filename := fmt.Sprintf("chart_%s_%dx%d.png", uname, gridSize, gridSize)
	file := &discordgo.File{
		Name:        filename,
		ContentType: "image/png",
		Reader:      bytes.NewReader(pngData),
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%dx%d Top Albums (%s)", gridSize, gridSize, label),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s on Last.fm:", uname),
			IconURL: userAvatarURL(target, ctx.Message.Author),
		},
		Image: &discordgo.MessageEmbedImage{
			URL: fmt.Sprintf("attachment://%s", filename),
		},
	}

	_, err = ctx.SendFileWithEmbed(file, embed)
	return err
}

type TasteCmd struct{}

func (c *TasteCmd) Name() string      { return "taste" }
func (c *TasteCmd) Aliases() []string { return []string{"compare"} }
func (c *TasteCmd) Category() string  { return "Last.fm" }
func (c *TasteCmd) Description() string {
	return "Compares music taste & top artists with another user."
}
func (c *TasteCmd) Usage() string   { return "<user>" }
func (c *TasteCmd) Example() string { return "@Cloudyy" }

func (c *TasteCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	client, err := getClient(ctx)
	if err != nil {
		return ctx.SendError(err.Error())
	}

	authorUname, _, err := ResolveLastfmUsername(ctx, "")
	if err != nil {
		return ctx.SendError(err.Error())
	}

	targetUname, _, err := ResolveLastfmUsername(ctx, strings.Join(ctx.Args, " "))
	if err != nil {
		return ctx.SendError(err.Error())
	}

	if strings.EqualFold(authorUname, targetUname) {
		return ctx.SendError("Cannot compare music taste with yourself.")
	}
	authorRes, err := client.GetTopArtists(ctx.Context(), authorUname, "overall", 50)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to fetch top artists for **%s**.", authorUname))
	}

	targetRes, err := client.GetTopArtists(ctx.Context(), targetUname, "overall", 50)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to fetch top artists for **%s**.", targetUname))
	}

	targetMap := make(map[string]int)
	for i, ar := range targetRes.TopArtists.Artist {
		targetMap[strings.ToLower(ar.DisplayName)] = 50 - i
	}

	var shared []string
	score := 0
	for i, ar := range authorRes.TopArtists.Artist {
		lowName := strings.ToLower(ar.DisplayName)
		if targetWeight, ok := targetMap[lowName]; ok {
			authorWeight := 50 - i
			score += (authorWeight + targetWeight)
			shared = append(shared, ar.DisplayName)
		}
	}

	maxItems := len(authorRes.TopArtists.Artist)
	if len(targetRes.TopArtists.Artist) < maxItems {
		maxItems = len(targetRes.TopArtists.Artist)
	}
	if maxItems > 50 {
		maxItems = 50
	}

	maxPossible := 0
	for i := 0; i < maxItems; i++ {
		maxPossible += (50 - i) * 2
	}
	if maxPossible == 0 {
		maxPossible = 1
	}

	pct := (score * 100) / maxPossible
	if pct > 100 {
		pct = 100
	}

	rating := "Very Low"
	switch {
	case pct >= 80:
		rating = "Super High"
	case pct >= 60:
		rating = "High"
	case pct >= 40:
		rating = "Medium"
	case pct >= 20:
		rating = "Low"
	}

	sharedText := "None"
	if len(shared) > 0 {
		if len(shared) > 10 {
			shared = shared[:10]
		}
		sharedText = strings.Join(shared, ", ")
	}

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("%s vs %s Music Taste", authorUname, targetUname),
		Description: fmt.Sprintf(
			"Compatibility between **%s** and **%s** is **%s** (`%d%%`).\n\n**Shared Top Artists:**\n%s",
			authorUname, targetUname, rating, pct, sharedText,
		),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    fmt.Sprintf("%s's Compatibility Check", ctx.Message.Author.Username),
			IconURL: helpers.UserAvatar(ctx.Message.Author),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}

type CrownsCmd struct{}

func (c *CrownsCmd) Name() string      { return "crowns" }
func (c *CrownsCmd) Aliases() []string { return []string{"crown", "mycrowns"} }
func (c *CrownsCmd) Category() string  { return "Last.fm" }
func (c *CrownsCmd) Description() string {
	return "Displays artist crowns held by a user or server leaderboard."
}
func (c *CrownsCmd) Usage() string   { return "(@user / artist)" }
func (c *CrownsCmd) Example() string { return "@Cloudyy" }

func (c *CrownsCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if ctx.DB == nil {
		return ctx.SendError("Database connection unavailable.")
	}

	if len(ctx.Args) > 0 {
		userID := ctx.ParseUserID(ctx.Args[0])
		if len(userID) < 16 {
			artistQuery := strings.Join(ctx.Args, " ")
			crown, err := ctx.DB.GetCrown(ctx.Message.GuildID, artistQuery)
			if err != nil || crown == nil {
				return ctx.SendError(fmt.Sprintf("Nobody holds the crown for **%s** in this server yet. Run `%swhoknows %s` to claim it.", helpers.EscapeMarkdown(artistQuery), ctx.Prefix, helpers.EscapeMarkdown(artistQuery)))
			}

			holderName := crown.UserID
			if holder, errU := ctx.Session.User(crown.UserID); errU == nil && holder != nil {
				holderName = holder.Username
			}

			embed := &discordgo.MessageEmbed{
				Description: fmt.Sprintf("👑 **%s** holds the **%s** crown with **%s** scrobbles!", helpers.EscapeMarkdown(holderName), helpers.EscapeMarkdown(crown.ArtistName), helpers.FormatNumber(int(crown.Playcount))),
			}
			_, err = ctx.ReplyEmbed(embed)
			return err
		}
	}

	targetUser, _, _ := ctx.TargetUserAndMember()
	if targetUser == nil {
		targetUser = ctx.Message.Author
	}
	if targetUser == nil {
		return ctx.SendError("Could not identify user.")
	}

	crowns, err := ctx.DB.GetUserCrowns(ctx.Message.GuildID, targetUser.ID)
	if err != nil || len(crowns) == 0 {
		return ctx.SendError(fmt.Sprintf("**%s** does not hold any artist crowns in this server yet.", helpers.EscapeMarkdown(targetUser.Username)))
	}

	var lines []string
	for i, cr := range crowns {
		if i >= 15 {
			break
		}
		lines = append(lines, fmt.Sprintf("`%d.` 👑 **%s** - `%s` scrobbles", i+1, helpers.EscapeMarkdown(cr.ArtistName), helpers.FormatNumber(int(cr.Playcount))))
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("👑 %s's Crowns (%s total)", helpers.EscapeMarkdown(targetUser.Username), helpers.FormatNumber(len(crowns))),
		Description: strings.Join(lines, "\n"),
		Author: &discordgo.MessageEmbedAuthor{
			Name:    targetUser.Username,
			IconURL: helpers.UserAvatar(targetUser),
		},
	}
	_, err = ctx.ReplyEmbed(embed)
	return err
}
