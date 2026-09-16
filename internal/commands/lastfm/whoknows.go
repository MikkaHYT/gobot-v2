package lastfm

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	lfm "gobot/internal/services/lastfm"

	"github.com/bwmarrin/discordgo"
)

type LeaderboardEntry struct {
	UserID      string
	DisplayName string
	Username    string
	Plays       int
}

type whoKnowsTask struct {
	member   *discordgo.Member
	username string
}

// fetchWhoKnowsLeaderboard matches guild members with database records in memory.
func fetchWhoKnowsLeaderboard(ctx *bot.Context, fetchPlaycount func(reqCtx context.Context, client *lfm.Client, username string) int) ([]LeaderboardEntry, error) {
	allLinked, err := ctx.DB.GetAllLastfmUsersMap()
	if err != nil || len(allLinked) == 0 {
		return nil, fmt.Errorf("no members in this server have linked their Last.fm account yet")
	}

	guild, errG := ctx.Guild()
	if errG != nil || guild == nil {
		return nil, fmt.Errorf("failed to retrieve server members")
	}

	var tasks []whoKnowsTask
	for _, m := range guild.Members {
		if m == nil || m.User == nil || m.User.Bot {
			continue
		}
		if uname, ok := allLinked[m.User.ID]; ok && uname != "" {
			tasks = append(tasks, whoKnowsTask{member: m, username: uname})
		}
	}

	if len(tasks) == 0 {
		return nil, fmt.Errorf("no registered Last.fm users found in this server")
	}

	reqCtx := ctx.Context()
	client, err := getClient(ctx)
	if err != nil {
		return nil, err
	}
	taskChan := make(chan whoKnowsTask, len(tasks))
	resultChan := make(chan LeaderboardEntry, len(tasks))

	for _, t := range tasks {
		taskChan <- t
	}
	close(taskChan)

	workers := 4
	if len(tasks) < workers {
		workers = len(tasks)
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		helpers.Spawn(func() {
			defer wg.Done()
			for {
				select {
				case <-reqCtx.Done():
					return
				case t, ok := <-taskChan:
					if !ok {
						return
					}
					if pc := fetchPlaycount(reqCtx, client, t.username); pc > 0 {
						dispName := t.member.Nick
						if dispName == "" {
							dispName = t.member.User.Username
						}
						resultChan <- LeaderboardEntry{
							UserID:      t.member.User.ID,
							DisplayName: dispName,
							Username:    t.username,
							Plays:       pc,
						}
					}
				}
			}
		})
	}

	wg.Wait()
	close(resultChan)

	var leaderboard []LeaderboardEntry
	for entry := range resultChan {
		leaderboard = append(leaderboard, entry)
	}

	sort.Slice(leaderboard, func(i, j int) bool {
		if leaderboard[i].Plays != leaderboard[j].Plays {
			return leaderboard[i].Plays > leaderboard[j].Plays
		}
		return leaderboard[i].UserID < leaderboard[j].UserID
	})

	return leaderboard, nil
}

type WhoKnowsCmd struct{}

func (c *WhoKnowsCmd) Name() string      { return "whoknows" }
func (c *WhoKnowsCmd) Aliases() []string { return []string{"wk", "whoknowsartist"} }
func (c *WhoKnowsCmd) Category() string  { return "Last.fm" }
func (c *WhoKnowsCmd) Description() string {
	return "Displays server member scrobble rankings for an artist."
}
func (c *WhoKnowsCmd) Usage() string   { return "<artist>" }
func (c *WhoKnowsCmd) Example() string { return "Juice WRLD" }

func (c *WhoKnowsCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	artistQuery := strings.Join(ctx.Args, " ")

	leaderboard, err := fetchWhoKnowsLeaderboard(ctx, func(reqCtx context.Context, client *lfm.Client, username string) int {
		ar, errFetch := client.GetArtistInfo(reqCtx, artistQuery, username)
		if errFetch == nil && ar.Stats.UserPlayCount != "" {
			pc, _ := strconv.Atoi(ar.Stats.UserPlayCount)
			return pc
		}
		return 0
	})

	if err != nil {
		return ctx.SendError(err.Error())
	}
	if len(leaderboard) == 0 {
		return ctx.SendError(fmt.Sprintf("Nobody in this server has scrobbled **%s** yet.", artistQuery))
	}

	topPlayer := leaderboard[0]
	crownNote := ""
	existingCrown, _ := ctx.DB.GetCrown(ctx.Message.GuildID, artistQuery)

	cleanTopName := helpers.EscapeMarkdown(topPlayer.DisplayName)
	cleanArtist := helpers.EscapeMarkdown(artistQuery)

	if existingCrown == nil {
		if errCrown := ctx.DB.SetCrown(ctx.Message.GuildID, artistQuery, topPlayer.UserID, topPlayer.Plays); errCrown == nil {
			crownNote = fmt.Sprintf("\n\n👑 **%s** claimed the **%s** crown with `%s` scrobbles!", cleanTopName, cleanArtist, helpers.FormatNumber(topPlayer.Plays))
		}
	} else if topPlayer.Plays > int(existingCrown.Playcount) && existingCrown.UserID != topPlayer.UserID {
		if errCrown := ctx.DB.SetCrown(ctx.Message.GuildID, artistQuery, topPlayer.UserID, topPlayer.Plays); errCrown == nil {
			crownNote = fmt.Sprintf("\n\n👑 **%s** stole the **%s** crown with `%s` scrobbles!", cleanTopName, cleanArtist, helpers.FormatNumber(topPlayer.Plays))
		}
	}

	title := fmt.Sprintf("Who Knows **%s**?", helpers.EscapeMarkdown(helpers.TitleCase(artistQuery)))
	return renderWhoKnowsLeaderboard(ctx, title, fmt.Sprintf("Nobody in this server has scrobbled **%s** yet.", artistQuery), crownNote, leaderboard)
}

type WhoKnowsAlbumCmd struct{}

func (c *WhoKnowsAlbumCmd) Name() string      { return "whoknowsalbum" }
func (c *WhoKnowsAlbumCmd) Aliases() []string { return []string{"wka", "whoknowsab"} }
func (c *WhoKnowsAlbumCmd) Category() string  { return "Last.fm" }
func (c *WhoKnowsAlbumCmd) Description() string {
	return "Displays server member scrobble rankings for an album."
}
func (c *WhoKnowsAlbumCmd) Usage() string   { return "<album>" }
func (c *WhoKnowsAlbumCmd) Example() string { return "Goodbye & Good Riddance" }

func (c *WhoKnowsAlbumCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	albumQuery := strings.Join(ctx.Args, " ")

	leaderboard, err := fetchWhoKnowsLeaderboard(ctx, func(reqCtx context.Context, client *lfm.Client, username string) int {
		res, errFetch := client.GetTopAlbums(reqCtx, username, "overall", 100)
		if errFetch == nil {
			for _, ab := range res.TopAlbums.Album {
				if strings.EqualFold(ab.DisplayTitle, albumQuery) {
					pc, _ := strconv.Atoi(ab.Playcount)
					return pc
				}
			}
		}
		return 0
	})

	if err != nil {
		return ctx.SendError(err.Error())
	}

	title := fmt.Sprintf("Who Knows Album **%s**?", helpers.TitleCase(albumQuery))
	emptyErr := fmt.Sprintf("Nobody in this server has scrobbled the album **%s** yet.", albumQuery)
	return renderWhoKnowsLeaderboard(ctx, title, emptyErr, "", leaderboard)
}

type WhoKnowsTrackCmd struct{}

func (c *WhoKnowsTrackCmd) Name() string      { return "whoknowstrack" }
func (c *WhoKnowsTrackCmd) Aliases() []string { return []string{"wkt", "whoknowstr"} }
func (c *WhoKnowsTrackCmd) Category() string  { return "Last.fm" }
func (c *WhoKnowsTrackCmd) Description() string {
	return "Displays server member scrobble rankings for a track."
}
func (c *WhoKnowsTrackCmd) Usage() string   { return "<track>" }
func (c *WhoKnowsTrackCmd) Example() string { return "Lucid Dreams" }

func (c *WhoKnowsTrackCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireGuild() {
		return nil
	}
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	trackQuery := strings.Join(ctx.Args, " ")

	leaderboard, err := fetchWhoKnowsLeaderboard(ctx, func(reqCtx context.Context, client *lfm.Client, username string) int {
		res, errFetch := client.GetTopTracks(reqCtx, username, "overall", 100)
		if errFetch == nil {
			for _, tr := range res.TopTracks.Track {
				if strings.EqualFold(tr.DisplayTitle, trackQuery) {
					pc, _ := strconv.Atoi(tr.Playcount)
					return pc
				}
			}
		}
		return 0
	})

	if err != nil {
		return ctx.SendError(err.Error())
	}

	title := fmt.Sprintf("Who Knows Track **%s**?", helpers.TitleCase(trackQuery))
	emptyErr := fmt.Sprintf("Nobody in this server has scrobbled the track **%s** yet.", trackQuery)
	return renderWhoKnowsLeaderboard(ctx, title, emptyErr, "", leaderboard)
}

func renderWhoKnowsLeaderboard(ctx *bot.Context, title, emptyErrMsg, extraNote string, leaderboard []LeaderboardEntry) error {
	if len(leaderboard) == 0 {
		return ctx.SendError(emptyErrMsg)
	}

	var lines []string
	for i, entry := range leaderboard {
		if i >= 15 {
			break
		}
		lines = append(lines, fmt.Sprintf("`%d.` **%s** (`%s`) - `%s` scrobbles", i+1, helpers.EscapeMarkdown(entry.DisplayName), helpers.EscapeMarkdown(entry.Username), helpers.FormatNumber(entry.Plays)))
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: strings.Join(lines, "\n") + extraNote,
	}
	_, err := ctx.ReplyEmbed(embed)
	return err
}
