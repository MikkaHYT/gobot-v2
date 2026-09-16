package ripper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	"gobot/internal/ripper"

	"github.com/bwmarrin/discordgo"
)

const progressUpdateInterval = 2500 * time.Millisecond

type DiscordPresenter struct {
	ctx             *bot.Context
	statusMsg       *discordgo.Message
	maxSizeMB       int
	globalMaxSizeMB int
	uploader        ripper.ExternalUploader

	mu        sync.Mutex
	latest    ripper.Progress
	hasUpdate bool
	stopCh    chan struct{}
	stopOnce  sync.Once
}

func NewDiscordPresenter(ctx *bot.Context, statusMsg *discordgo.Message, maxSizeMB int, globalMaxSizeMB ...int) *DiscordPresenter {
	var apiKey string
	if ctx != nil && ctx.Config != nil {
		apiKey = ctx.Config.StorageToAPIKey
	}
	gLimit := 200
	if len(globalMaxSizeMB) > 0 && globalMaxSizeMB[0] > 0 {
		gLimit = globalMaxSizeMB[0]
	}
	p := &DiscordPresenter{
		ctx:             ctx,
		statusMsg:       statusMsg,
		maxSizeMB:       maxSizeMB,
		globalMaxSizeMB: gLimit,
		uploader:        ripper.NewDefaultExternalUploader(apiKey),
		stopCh:          make(chan struct{}),
	}
	helpers.Spawn(p.runUpdateLoop)
	return p
}

func (p *DiscordPresenter) SetUploader(u ripper.ExternalUploader) {
	p.uploader = u
}

func (p *DiscordPresenter) OnProgress(ctx context.Context, prog ripper.Progress) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.latest = prog
	p.hasUpdate = true
	if prog.Stage == ripper.StageCompleted || prog.Stage == ripper.StageFailed {
		p.Stop()
	}
}

func (p *DiscordPresenter) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
}

func (p *DiscordPresenter) runUpdateLoop() {
	ticker := time.NewTicker(progressUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.flushProgress()
		}
	}
}

func (p *DiscordPresenter) flushProgress() {
	p.mu.Lock()
	if !p.hasUpdate {
		p.mu.Unlock()
		return
	}
	prog := p.latest
	p.hasUpdate = false
	p.mu.Unlock()

	var embed *discordgo.MessageEmbed
	switch prog.Stage {
	case ripper.StageQueued:
		embed = &discordgo.MessageEmbed{
			Description: fmt.Sprintf("Position **#%d** in queue...", prog.QueuePosition),
			Color:       helpers.ColorPending,
		}
	case ripper.StageDownloading:
		desc := "Downloading media..."
		if prog.Percent > 0 {
			desc = fmt.Sprintf("Downloading... **%.1f%%**", prog.Percent)
			if prog.Speed != "" && prog.ETA != "" {
				desc += fmt.Sprintf(" at **%s** (ETA %s)", prog.Speed, prog.ETA)
			}
		}
		embed = &discordgo.MessageEmbed{
			Description: desc,
			Color:       helpers.ColorInfo,
		}
	case ripper.StageTransforming:
		embed = &discordgo.MessageEmbed{
			Description: "Processing audio...",
			Color:       helpers.ColorInfo,
		}
	case ripper.StageValidating:
		embed = &discordgo.MessageEmbed{
			Description: "Validating media...",
			Color:       helpers.ColorInfo,
		}
	case ripper.StageUploading:
		embed = &discordgo.MessageEmbed{
			Description: "Uploading media...",
			Color:       helpers.ColorPurple,
		}
	default:
		return
	}

	updateCtx, cancel := context.WithTimeout(p.ctx.Context(), 2*time.Second)
	defer cancel()
	_, _ = p.ctx.Session.ChannelMessageEditEmbed(p.statusMsg.ChannelID, p.statusMsg.ID, embed, discordgo.WithContext(updateCtx))
}

func (p *DiscordPresenter) PresentOutcome(ctx context.Context, outcome ripper.TaskOutcome) error {
	p.Stop()

	if outcome.Err != nil {
		p.handleError(ctx, outcome.Err)
		return outcome.Err
	}

	if ctx.Err() != nil {
		p.handleError(ctx, ctx.Err())
		return ctx.Err()
	}

	return p.handleSuccess(ctx, outcome)
}

func (p *DiscordPresenter) handleError(ctx context.Context, err error) {
	desc := "Failed to download media from link."
	if errors.Is(err, ripper.ErrServiceStopped) {
		desc = "Download task cancelled: service is shutting down."
	} else if errors.Is(err, ripper.ErrExternalUploadFailed) {
		desc = fmt.Sprintf("The file exceeds Discord's **%d MB** limit, and external upload providers failed. Please try again later.", p.maxSizeMB)
	} else if errors.Is(err, ripper.ErrMediaTooLarge) {
		limit := p.globalMaxSizeMB
		if limit <= 0 {
			limit = 200
		}
		desc = fmt.Sprintf("The file exceeds the maximum download limit of **%d MB**.", limit)
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		desc = "Download task timed out."
	}

	embed := &discordgo.MessageEmbed{
		Description: desc,
		Color:       helpers.ColorError,
	}

	editCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), helpers.DurationFeedbackShort)
	defer cancel()
	_, _ = p.ctx.Session.ChannelMessageEditEmbed(p.statusMsg.ChannelID, p.statusMsg.ID, embed, discordgo.WithContext(editCtx))
}

func (p *DiscordPresenter) handleSuccess(ctx context.Context, outcome ripper.TaskOutcome) error {
	limitBytes := int64(p.maxSizeMB) * 1024 * 1024
	var oversized []string
	for _, path := range outcome.Files {
		fi, err := os.Stat(path)
		if err == nil && fi.Size() > limitBytes {
			oversized = append(oversized, path)
		}
	}
	if len(oversized) > 0 {
		return p.handleExternalUpload(ctx, outcome, outcome.Files)
	}

	var filesToSend []*discordgo.File
	var openedFiles []*os.File
	defer func() {
		for _, f := range openedFiles {
			_ = f.Close()
		}
	}()

	for _, path := range outcome.Files {
		if ctx.Err() != nil {
			p.handleError(ctx, ctx.Err())
			return ctx.Err()
		}
		f, err := os.Open(path)
		if err != nil {
			bot.Errorf("[RIPPER] Failed to open downloaded file for upload '%s': %v", path, err)
			continue
		}
		openedFiles = append(openedFiles, f)
		filesToSend = append(filesToSend, &discordgo.File{
			Name:   filepath.Base(path),
			Reader: f,
		})
	}

	if len(filesToSend) == 0 {
		err := fmt.Errorf("no readable files")
		p.handleError(ctx, err)
		return err
	}

	uploaderName := outcome.Meta.Uploader
	if uploaderName == "" {
		uploaderName = ExtractAuthorFromURL(outcome.URL)
	}
	if uploaderName == "" {
		uploaderName = p.ctx.Message.Author.Username
	}

	webURL := outcome.Meta.WebpageURL
	if webURL == "" {
		webURL = outcome.URL
	}

	embedCard := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name: uploaderName,
		},
		Description: formatDescriptionWithHashtags(outcome.Meta.Title, outcome.Meta.Description, outcome.Meta.Tags, webURL),
		Color:       helpers.ColorDefault,
	}

	firstBatch := filesToSend
	var secondBatch []*discordgo.File
	if len(filesToSend) > 10 {
		firstBatch = filesToSend[:10]
		secondBatch = filesToSend[10:]
		if len(secondBatch) > 10 {
			secondBatch = secondBatch[:10]
		}
	}

	totalAttached := len(firstBatch) + len(secondBatch)
	if len(outcome.Files) > 1 {
		footerText := fmt.Sprintf("%d items attached", totalAttached)
		if len(outcome.Files) > totalAttached {
			footerText = fmt.Sprintf("Attached %d of %d items", totalAttached, len(outcome.Files))
		}
		embedCard.Footer = &discordgo.MessageEmbedFooter{
			Text: footerText,
		}
	}

	msgSend := &discordgo.MessageSend{
		Content: p.ctx.Message.Author.Mention(),
		Embeds:  []*discordgo.MessageEmbed{embedCard},
		Files:   firstBatch,
	}

	_, err := p.ctx.Session.ChannelMessageSendComplex(p.statusMsg.ChannelID, msgSend, discordgo.WithContext(ctx))
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "too large") || strings.Contains(errMsg, "413") || strings.Contains(errMsg, "maximum size") {
			return p.handleExternalUpload(ctx, outcome, outcome.Files)
		}
		bot.Errorf("[RIPPER] Upload failed for URL '%s': %v", helpers.SanitizeURL(outcome.URL), err)
		p.handleError(ctx, fmt.Errorf("upload failed: %w", err))
		return err
	}

	delCtx, cancel := context.WithTimeout(context.Background(), helpers.DurationFeedbackShort)
	defer cancel()
	_ = p.ctx.Session.ChannelMessageDelete(p.statusMsg.ChannelID, p.statusMsg.ID, discordgo.WithContext(delCtx))

	if len(secondBatch) > 0 {
		secondSend := &discordgo.MessageSend{
			Files: secondBatch,
		}
		_, secondErr := p.ctx.Session.ChannelMessageSendComplex(p.statusMsg.ChannelID, secondSend, discordgo.WithContext(ctx))
		if secondErr != nil {
			bot.Warnf("[RIPPER] Failed to send secondary attachment batch: %v", secondErr)
		}
	}

	return nil
}

func (p *DiscordPresenter) handleExternalUpload(ctx context.Context, outcome ripper.TaskOutcome, files []string) error {
	if p.uploader == nil {
		p.handleError(ctx, ripper.ErrExternalUploadFailed)
		return ripper.ErrExternalUploadFailed
	}

	if ctx.Err() != nil {
		p.handleError(ctx, ctx.Err())
		return ctx.Err()
	}

	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), helpers.DurationFeedbackShort)
	_, _ = p.ctx.Session.ChannelMessageEditEmbed(p.statusMsg.ChannelID, p.statusMsg.ID, &discordgo.MessageEmbed{
		Description: "File exceeds Discord upload limit. Uploading to external host...",
		Color:       helpers.ColorPurple,
	}, discordgo.WithContext(updateCtx))
	cancel()

	type uploadItem struct {
		name string
		size int64
		res  *ripper.ExternalUploadResult
	}
	var uploaded []uploadItem

	for _, path := range files {
		if ctx.Err() != nil {
			p.handleError(ctx, ctx.Err())
			return ctx.Err()
		}
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		res, err := p.uploader.Upload(ctx, path)
		if err != nil {
			bot.Errorf("[RIPPER] External upload failed for '%s': %v", path, err)
			continue
		}
		uploaded = append(uploaded, uploadItem{
			name: filepath.Base(path),
			size: fi.Size(),
			res:  res,
		})
	}

	if len(uploaded) == 0 {
		p.handleError(ctx, ripper.ErrExternalUploadFailed)
		return ripper.ErrExternalUploadFailed
	}

	uploaderName := outcome.Meta.Uploader
	if uploaderName == "" {
		uploaderName = ExtractAuthorFromURL(outcome.URL)
	}
	if uploaderName == "" {
		uploaderName = p.ctx.Message.Author.Username
	}

	webURL := outcome.Meta.WebpageURL
	if webURL == "" {
		webURL = outcome.URL
	}

	desc := formatDescriptionWithHashtags(outcome.Meta.Title, outcome.Meta.Description, outcome.Meta.Tags, webURL)
	if desc != "" {
		desc += "\n\n"
	}

	formatExpiry := func(raw string) string {
		if raw == "" {
			return "Expires soon"
		}
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return fmt.Sprintf("Expires <t:%d:R>", t.Unix())
		}
		if strings.HasSuffix(raw, "h") || strings.HasSuffix(raw, "d") {
			return fmt.Sprintf("Expires in %s", raw)
		}
		return fmt.Sprintf("Expires %s", raw)
	}

	var buttons []discordgo.MessageComponent
	if len(uploaded) == 1 {
		item := uploaded[0]

		desc += fmt.Sprintf("-# File size exceeds discord bot limits, using external host\n-# Size: `%s` • %s",
			helpers.FormatBytes(uint64(item.size)), formatExpiry(item.res.ExpiresAt))
		buttons = append(buttons, discordgo.Button{
			Label: "Download Media",
			Style: discordgo.LinkButton,
			URL:   item.res.URL,
		})
		if item.res.PageURL != "" && item.res.PageURL != item.res.URL {
			buttons = append(buttons, discordgo.Button{
				Label: "Web Mirror",
				Style: discordgo.LinkButton,
				URL:   item.res.PageURL,
			})
		}
	} else {
		for _, item := range uploaded {
			line := fmt.Sprintf("\n• [%s](%s)", helpers.EscapeMarkdown(item.name), item.res.URL)
			if item.res.PageURL != "" && item.res.PageURL != item.res.URL {
				line += fmt.Sprintf(" ([Mirror](%s))", item.res.PageURL)
			}
			line += fmt.Sprintf(" (`%s`)", helpers.FormatBytes(uint64(item.size)))
			desc += line
		}
		desc += fmt.Sprintf("\n\n-# Host: **%s** • %s\n-# file size exceeds discord bot limits", uploaded[0].res.Provider, formatExpiry(uploaded[0].res.ExpiresAt))
		buttons = append(buttons, discordgo.Button{
			Label: "Download Media",
			Style: discordgo.LinkButton,
			URL:   uploaded[0].res.URL,
		})
	}

	embedCard := &discordgo.MessageEmbed{
		Author: &discordgo.MessageEmbedAuthor{
			Name: uploaderName,
		},
		Description: desc,
		Color:       helpers.ColorDefault,
	}
	if outcome.Meta.Thumbnail != "" {
		embedCard.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: outcome.Meta.Thumbnail,
		}
	}

	msgSend := &discordgo.MessageSend{
		Content: p.ctx.Message.Author.Mention(),
		Embeds:  []*discordgo.MessageEmbed{embedCard},
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: buttons,
			},
		},
	}

	_, err := p.ctx.Session.ChannelMessageSendComplex(p.statusMsg.ChannelID, msgSend, discordgo.WithContext(ctx))
	if err != nil {
		bot.Errorf("[RIPPER] Failed to send external upload message: %v", err)
		p.handleError(ctx, fmt.Errorf("failed to send message: %w", err))
		return err
	}

	delCtx, delCancel := context.WithTimeout(context.Background(), helpers.DurationFeedbackShort)
	defer delCancel()
	_ = p.ctx.Session.ChannelMessageDelete(p.statusMsg.ChannelID, p.statusMsg.ID, discordgo.WithContext(delCtx))

	return nil
}
