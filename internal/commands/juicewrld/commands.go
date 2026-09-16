package juicewrld

import (
	"strings"

	"gobot/internal/bot"
	"gobot/internal/commands"
)

var Commands = []commands.Command{
	&LeakCmd{},
	&SonginfoCmd{},
	&SessionCmd{},
	&SessioninfoCmd{},
	&SnipCmd{},
	&CoverCmd{},
	&InstrumentalCmd{},
	&RandomSongCmd{},
}

type RandomSongCmd struct{}

func (c *RandomSongCmd) Name() string      { return "randomsong" }
func (c *RandomSongCmd) Aliases() []string { return []string{"random", "rand"} }
func (c *RandomSongCmd) Category() string  { return "Juice WRLD" }
func (c *RandomSongCmd) Description() string {
	return "Displays a random Juice WRLD song (supports era or category filters)."
}
func (c *RandomSongCmd) Usage() string   { return "[era/category]" }
func (c *RandomSongCmd) Example() string { return "drfl" }

func (c *RandomSongCmd) Execute(ctx *bot.Context) error {
	filter := strings.TrimSpace(strings.Join(ctx.Args, " "))
	return ExecuteRandomSongCommand(ctx, filter)
}

type LeakCmd struct{}

func (c *LeakCmd) Name() string        { return "leak" }
func (c *LeakCmd) Aliases() []string   { return []string{"og", "ogfile", "song"} }
func (c *LeakCmd) Category() string    { return "Juice WRLD" }
func (c *LeakCmd) Description() string { return "Search & download Juice WRLD leaks." }
func (c *LeakCmd) Usage() string       { return "<song_name>" }
func (c *LeakCmd) Example() string     { return "mula" }

func (c *LeakCmd) Execute(ctx *bot.Context) error {
	query := strings.Join(ctx.Args, " ")
	return ExecuteSearchCommand(ctx, "leak", query, "")
}

type SessionCmd struct{}

func (c *SessionCmd) Name() string        { return "session" }
func (c *SessionCmd) Aliases() []string   { return []string{"sess"} }
func (c *SessionCmd) Category() string    { return "Juice WRLD" }
func (c *SessionCmd) Description() string { return "Displays studio session downloads & edits." }
func (c *SessionCmd) Usage() string       { return "<song_name>" }
func (c *SessionCmd) Example() string     { return "mula" }

func (c *SessionCmd) Execute(ctx *bot.Context) error {
	query := strings.Join(ctx.Args, " ")
	return ExecuteSearchCommand(ctx, "session", query, "recording_session")
}

type SnipCmd struct{}

func (c *SnipCmd) Name() string      { return "snip" }
func (c *SnipCmd) Aliases() []string { return []string{} }
func (c *SnipCmd) Category() string  { return "Juice WRLD" }
func (c *SnipCmd) Description() string {
	return "Displays snippets."
}
func (c *SnipCmd) Usage() string   { return "<song_name>" }
func (c *SnipCmd) Example() string { return "star of the show" }

func (c *SnipCmd) Execute(ctx *bot.Context) error {
	query := strings.Join(ctx.Args, " ")
	return ExecuteSearchCommand(ctx, "snip", query, "")
}

type CoverCmd struct{}

func (c *CoverCmd) Name() string      { return "cover" }
func (c *CoverCmd) Aliases() []string { return []string{"coverart", "co"} }
func (c *CoverCmd) Category() string  { return "Juice WRLD" }
func (c *CoverCmd) Description() string {
	return "Displays cover art for songs."
}
func (c *CoverCmd) Usage() string   { return "<song_name>" }
func (c *CoverCmd) Example() string { return "bandit" }

func (c *CoverCmd) Execute(ctx *bot.Context) error {
	query := strings.Join(ctx.Args, " ")
	return ExecuteSearchCommand(ctx, "cover", query, "")
}

type SonginfoCmd struct{}

func (c *SonginfoCmd) Name() string        { return "songinfo" }
func (c *SonginfoCmd) Aliases() []string   { return []string{"si"} }
func (c *SonginfoCmd) Category() string    { return "Juice WRLD" }
func (c *SonginfoCmd) Description() string { return "Displays detailed song info." }
func (c *SonginfoCmd) Usage() string       { return "<song_name>" }
func (c *SonginfoCmd) Example() string     { return "All girls are the same" } // thanks jim

func (c *SonginfoCmd) Execute(ctx *bot.Context) error {
	query := strings.Join(ctx.Args, " ")
	return ExecuteSearchCommand(ctx, "songinfo", query, "")
}

type SessioninfoCmd struct{}

func (c *SessioninfoCmd) Name() string      { return "sessioninfo" }
func (c *SessioninfoCmd) Aliases() []string { return []string{"ssi"} }
func (c *SessioninfoCmd) Category() string  { return "Juice WRLD" }
func (c *SessioninfoCmd) Description() string {
	return "Displays detailed studio session info."
}
func (c *SessioninfoCmd) Usage() string   { return "<song_name>" }
func (c *SessioninfoCmd) Example() string { return "sometimes" }

func (c *SessioninfoCmd) Execute(ctx *bot.Context) error {
	query := strings.Join(ctx.Args, " ")
	return ExecuteSearchCommand(ctx, "sessioninfo", query, "recording_session")
}

type InstrumentalCmd struct{}

func (c *InstrumentalCmd) Name() string { return "instrumental" }
func (c *InstrumentalCmd) Aliases() []string {
	return []string{"inst", "instrumentals", "instromental"}
}
func (c *InstrumentalCmd) Category() string    { return "Juice WRLD" }
func (c *InstrumentalCmd) Description() string { return "Search & download Juice WRLD instrumentals." }
func (c *InstrumentalCmd) Usage() string       { return "<song_name>" }
func (c *InstrumentalCmd) Example() string     { return "robbery" }

func (c *InstrumentalCmd) Execute(ctx *bot.Context) error {
	query := strings.Join(ctx.Args, " ")
	return ExecuteSearchCommand(ctx, "instrumental", query, "")
}
