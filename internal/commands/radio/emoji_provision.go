package radio

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

func EnsureRadioApplicationEmojis(s *discordgo.Session, appID string) error {
	if s == nil || appID == "" {
		return errors.New("cannot ensure application emojis: session or appID is empty")
	}

	existingEmojis, err := s.ApplicationEmojis(appID)
	if err != nil {
		return fmt.Errorf("failed to fetch application emojis for app %s: %w", appID, err)
	}

	activeNames := make(map[string]bool, len(radioEmojiAssets))
	for _, asset := range radioEmojiAssets {
		activeNames[asset.Name] = true
	}

	for _, e := range existingEmojis {
		if strings.HasPrefix(e.Name, "radio_") && !activeNames[e.Name] {
			if delErr := s.ApplicationEmojiDelete(appID, e.ID); delErr != nil {
				logger.Warnf("[RADIO EMOJI] Failed to delete obsolete application emoji %s (ID: %s): %v", e.Name, e.ID, delErr)
			} else {
				logger.Infof("[RADIO EMOJI] Wiped obsolete application emoji: %s (ID: %s)", e.Name, e.ID)
			}
		}
	}

	existingByName := make(map[string]*discordgo.Emoji, len(existingEmojis))
	for _, e := range existingEmojis {
		if activeNames[e.Name] {
			existingByName[e.Name] = e
		}
	}

	var creationErrors []error

	for _, asset := range radioEmojiAssets {
		if existing, found := existingByName[asset.Name]; found {
			RegisterResolvedEmoji(asset.Key, existing.ID, existing.Name, existing.Animated)
			logger.Debugf("[RADIO EMOJI] Found existing application emoji: %s (ID: %s)", asset.Name, existing.ID)
			continue
		}

		fileBytes, readErr := emojiAssets.ReadFile(asset.Path)
		if readErr != nil {
			creationErrors = append(creationErrors, fmt.Errorf("failed to read embedded asset %s (%s): %w", asset.Key, asset.Path, readErr))
			continue
		}

		mimeType := "image/png"
		if asset.Animated || strings.EqualFold(filepath.Ext(asset.Path), ".gif") {
			mimeType = "image/gif"
		}
		dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(fileBytes))

		params := &discordgo.EmojiParams{
			Name:  asset.Name,
			Image: dataURI,
		}

		created, createErr := s.ApplicationEmojiCreate(appID, params)
		if createErr != nil {
			creationErrors = append(creationErrors, fmt.Errorf("failed to create application emoji %s: %w", asset.Name, createErr))
			logger.Warnf("[RADIO EMOJI] Failed to auto-provision %s: %v (degrading to Unicode fallback %q)", asset.Name, createErr, asset.UnicodeFallback)
			continue
		}

		RegisterResolvedEmoji(asset.Key, created.ID, created.Name, created.Animated)
		logger.Infof("[RADIO EMOJI] Successfully provisioned application emoji: %s (ID: %s)", created.Name, created.ID)
	}

	if len(creationErrors) > 0 {
		return errors.Join(creationErrors...)
	}

	return nil
}
