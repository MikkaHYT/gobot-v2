package ripper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gobot/internal/bot"
	"gobot/internal/commands"
	"gobot/internal/helpers"
	"gobot/internal/ripper"

	"github.com/bwmarrin/discordgo"
)

var Commands = []commands.Command{
	&RipCmd{},
}

var (
	pipelineMu     sync.RWMutex
	globalPipeline *ripper.Pipeline
	taskSequence   uint64
)

func SetPipeline(p *ripper.Pipeline) {
	pipelineMu.Lock()
	defer pipelineMu.Unlock()
	globalPipeline = p
}

func GetPipeline() *ripper.Pipeline {
	pipelineMu.RLock()
	defer pipelineMu.RUnlock()
	return globalPipeline
}

func StartQueueManager(ctx context.Context, baseDir string, maxDownloadSizeMB int64, cookiesPath ...string) error {
	c := ""
	if len(cookiesPath) > 0 {
		c = cookiesPath[0]
	}
	p := ripper.New(ripper.Options{
		BaseDir:           baseDir,
		MaxDownloadSizeMB: maxDownloadSizeMB,
		Downloader: ripper.NewCompositeDownloader(
			ripper.NewYTDLPDownloader(c),
			ripper.NewAPIFallbackDownloader(),
		),
	})
	if err := p.Start(ctx); err != nil {
		bot.Errorf("[RIPPER] Failed to initialize media pipeline: %v", err)
		return err
	}
	SetPipeline(p)
	return nil
}

func StopQueueManager() {
	p := GetPipeline()
	if p != nil {
		p.Stop()
	}
}

type RipCmd struct{}

func (c *RipCmd) Name() string      { return "rip" }
func (c *RipCmd) Aliases() []string { return []string{"ripper", "download", "dl"} }
func (c *RipCmd) Category() string  { return "Utility" }
func (c *RipCmd) Description() string {
	return "Rip media from links"
}
func (c *RipCmd) Usage() string {
	return "<url> [audio] [best]"
}
func (c *RipCmd) Example() string {
	return "rip https://x.com/user/status/123 | rip audio https://tiktok.com/@user/video/123 | rip audio <url> best"
}

func parseRipArgs(args []string) (string, bool, bool) {
	var targetURL string
	var audioOnly bool
	var bestQuality bool

	for _, arg := range args {
		lower := strings.ToLower(arg)
		switch lower {
		case "audio", "mp3", "sound":
			audioOnly = true
		case "best", "max", "highest", "hq":
			bestQuality = true
		default:
			if targetURL == "" && strings.HasPrefix(lower, "http") {
				targetURL = arg
			}
		}
	}

	if targetURL == "" && len(args) > 0 {
		for _, arg := range args {
			lower := strings.ToLower(arg)
			if lower == "audio" || lower == "mp3" || lower == "sound" ||
				lower == "best" || lower == "max" || lower == "highest" || lower == "hq" {
				continue
			}
			if strings.HasPrefix(lower, "http") {
				targetURL = arg
				break
			}
		}
	}

	return targetURL, audioOnly, bestQuality
}

func (c *RipCmd) Execute(ctx *bot.Context) error {

	targetURL, audioOnly, bestQuality := parseRipArgs(ctx.Args)

	if targetURL == "" && ctx.Message.ReferencedMessage != nil {
		targetURL = ExtractURLFromMessage(ctx.Message.ReferencedMessage)
	}

	if targetURL == "" {
		_, _ = ctx.SendUsage(c)
		return nil
	}

	targetURL = CleanTrackingParams(targetURL)

	if !helpers.IsDomainAllowed(targetURL) {
		return ctx.SendError("The provided link is not in the rip whitelist.")
	}

	p := GetPipeline()
	if p == nil {
		return ctx.SendError("The media downloader service is currently unavailable.")
	}

	maxSizeMB := getMaxUploadSizeMB(ctx)
	maxSizeBytes := int64(maxSizeMB) * 1024 * 1024

	desc := "Downloading media..."
	if audioOnly && bestQuality {
		desc = "Downloading audio (best quality)..."
	} else if bestQuality {
		desc = "Downloading media (best quality)..."
	} else if audioOnly {
		desc = "Downloading audio..."
	}

	statusEmbed := &discordgo.MessageEmbed{
		Description: desc,
		Color:       helpers.ColorInfo,
	}

	msg, err := ctx.ReplyEmbed(statusEmbed)
	if err != nil {
		return err
	}

	taskID := fmt.Sprintf("%s_%d_%d", ctx.Message.Author.ID, time.Now().UnixNano(), atomic.AddUint64(&taskSequence, 1))
	req := ripper.TaskRequest{
		ID:           taskID,
		URL:          targetURL,
		AudioOnly:    audioOnly,
		BestQuality:  bestQuality,
		UserID:       ctx.Message.Author.ID,
		GuildID:      ctx.Message.GuildID,
		MaxSizeBytes: maxSizeBytes,
	}

	globalMaxSizeMB := 200
	if ctx != nil && ctx.Config != nil && ctx.Config.MaxMediaDownloadSizeMB > 0 {
		globalMaxSizeMB = int(ctx.Config.MaxMediaDownloadSizeMB)
	}

	presenter := NewDiscordPresenter(ctx, msg, maxSizeMB, globalMaxSizeMB)

	submitErr := p.Submit(ctx.Context(), req, presenter, presenter.PresentOutcome)
	if submitErr != nil {
		presenter.Stop()
		if errors.Is(submitErr, ripper.ErrUserSlotExhausted) {
			_, _ = ctx.Session.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, &discordgo.MessageEmbed{
				Description: "You already have 2 active downloads in progress. Please wait for them to complete.",
				Color:       helpers.ColorError,
			})
			return nil
		}
		if errors.Is(submitErr, ripper.ErrQueueFull) {
			_, _ = ctx.Session.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, &discordgo.MessageEmbed{
				Description: "The download queue is currently full. Please try again in a moment.",
				Color:       helpers.ColorError,
			})
			return nil
		}
		_, _ = ctx.Session.ChannelMessageEditEmbed(msg.ChannelID, msg.ID, &discordgo.MessageEmbed{
			Description: "Failed to queue download request.",
			Color:       helpers.ColorError,
		})
		return submitErr
	}

	return nil
}

func getMaxUploadSizeMB(ctx *bot.Context) int {
	discordLimit := 24
	if ctx != nil && ctx.Message != nil && ctx.Message.GuildID != "" {
		if guild, _ := ctx.Guild(); guild != nil {
			switch guild.PremiumTier {
			case discordgo.PremiumTier2:
				discordLimit = 48
			case discordgo.PremiumTier3:
				discordLimit = 490
			}
		}
	}
	if ctx != nil && ctx.Config != nil && ctx.Config.MaxMediaDownloadSizeMB > 0 && ctx.Config.MaxMediaDownloadSizeMB < int64(discordLimit) {
		return int(ctx.Config.MaxMediaDownloadSizeMB)
	}
	return discordLimit
}
