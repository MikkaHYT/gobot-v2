package radio

import (
	"embed"
	"fmt"
	"sync"

	"github.com/bwmarrin/discordgo"
)

//go:embed assets/*
var emojiAssets embed.FS

// RadioEmojiAsset update the asset file and increment the version to change the icon, this won't delete it // todo implement delete prev icon v
type RadioEmojiAsset struct {
	Key             string
	Name            string
	Path            string
	Animated        bool
	UnicodeFallback string
}

var radioEmojiAssets = []RadioEmojiAsset{
	{
		Key:             "download",
		Name:            "radio_download_v2",
		Path:            "assets/radio_download.png",
		Animated:        false,
		UnicodeFallback: "⬇️",
	},
	{
		Key:             "previous",
		Name:            "radio_previous_v2",
		Path:            "assets/radio_previous.png",
		Animated:        false,
		UnicodeFallback: "⏮️",
	},
	{
		Key:             "pause",
		Name:            "radio_pause_v2",
		Path:            "assets/radio_pause.png",
		Animated:        false,
		UnicodeFallback: "⏸️",
	},
	{
		Key:             "play",
		Name:            "radio_play_v2",
		Path:            "assets/radio_play.png",
		Animated:        false,
		UnicodeFallback: "▶️",
	},
	{
		Key:             "skip",
		Name:            "radio_skip_v2",
		Path:            "assets/radio_skip.png",
		Animated:        false,
		UnicodeFallback: "⏭️",
	},
	{
		Key:             "loop",
		Name:            "radio_loop_v2",
		Path:            "assets/radio_loop.png",
		Animated:        false,
		UnicodeFallback: "🔁",
	},
	{
		Key:             "loop_track",
		Name:            "radio_loop_track_v2",
		Path:            "assets/radio_loop_track.png",
		Animated:        false,
		UnicodeFallback: "🔂",
	},
	{
		Key:             "loop_queue",
		Name:            "radio_loop_queue_v2",
		Path:            "assets/radio_loop_queue.png",
		Animated:        false,
		UnicodeFallback: "🔄",
	},
	{
		Key:             "queue",
		Name:            "radio_queue_v2",
		Path:            "assets/radio_queue.png",
		Animated:        false,
		UnicodeFallback: "📋",
	},
	{
		Key:             "paused",
		Name:            "radio_paused_v1",
		Path:            "assets/radio_paused.png",
		Animated:        false,
		UnicodeFallback: "⏸️",
	},
	{
		Key:             "playing",
		Name:            "radio_playing_v1",
		Path:            "assets/radio_playing.gif",
		Animated:        true,
		UnicodeFallback: "🎶",
	},
	{Key: "bar_f0", Name: "radio_bar_f0_v1", Path: "assets/radio_bar_f0.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f1", Name: "radio_bar_f1_v1", Path: "assets/radio_bar_f1.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f2", Name: "radio_bar_f2_v1", Path: "assets/radio_bar_f2.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f3", Name: "radio_bar_f3_v1", Path: "assets/radio_bar_f3.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f4", Name: "radio_bar_f4_v1", Path: "assets/radio_bar_f4.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f5", Name: "radio_bar_f5_v1", Path: "assets/radio_bar_f5.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f6", Name: "radio_bar_f6_v1", Path: "assets/radio_bar_f6.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f7", Name: "radio_bar_f7_v1", Path: "assets/radio_bar_f7.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f8", Name: "radio_bar_f8_v1", Path: "assets/radio_bar_f8.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_f9", Name: "radio_bar_f9_v1", Path: "assets/radio_bar_f9.png", Animated: false, UnicodeFallback: "━"},
	{Key: "bar_k0", Name: "radio_bar_k0_v2", Path: "assets/radio_bar_k0.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k1", Name: "radio_bar_k1_v1", Path: "assets/radio_bar_k1.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k2", Name: "radio_bar_k2_v1", Path: "assets/radio_bar_k2.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k3", Name: "radio_bar_k3_v1", Path: "assets/radio_bar_k3.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k4", Name: "radio_bar_k4_v1", Path: "assets/radio_bar_k4.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k5", Name: "radio_bar_k5_v1", Path: "assets/radio_bar_k5.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k6", Name: "radio_bar_k6_v1", Path: "assets/radio_bar_k6.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k7", Name: "radio_bar_k7_v1", Path: "assets/radio_bar_k7.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k8", Name: "radio_bar_k8_v1", Path: "assets/radio_bar_k8.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_k9", Name: "radio_bar_k9_v1", Path: "assets/radio_bar_k9.png", Animated: false, UnicodeFallback: "●"},
	{Key: "bar_em", Name: "radio_bar_em_v1", Path: "assets/radio_bar_em.png", Animated: false, UnicodeFallback: "─"},
	{Key: "bar_er", Name: "radio_bar_er_v1", Path: "assets/radio_bar_er.png", Animated: false, UnicodeFallback: "─"},
}

type resolvedEmoji struct {
	ID       string
	Name     string
	Animated bool
}

type emojiRegistry struct {
	mu       sync.RWMutex
	resolved map[string]resolvedEmoji
}

var (
	globalEmojiRegistry = &emojiRegistry{
		resolved: make(map[string]resolvedEmoji),
	}
	assetByKey = make(map[string]RadioEmojiAsset)
)

func init() {
	for _, asset := range radioEmojiAssets {
		assetByKey[asset.Key] = asset
	}
}

func RegisterResolvedEmoji(key, id, name string, animated bool) {
	globalEmojiRegistry.mu.Lock()
	defer globalEmojiRegistry.mu.Unlock()
	globalEmojiRegistry.resolved[key] = resolvedEmoji{
		ID:       id,
		Name:     name,
		Animated: animated,
	}
}

func ResetEmojiRegistry() {
	globalEmojiRegistry.mu.Lock()
	defer globalEmojiRegistry.mu.Unlock()
	globalEmojiRegistry.resolved = make(map[string]resolvedEmoji)
}

// GetEmoji returns a button emoji component.
func GetEmoji(key string) *discordgo.ComponentEmoji {
	globalEmojiRegistry.mu.RLock()
	resolved, ok := globalEmojiRegistry.resolved[key]
	globalEmojiRegistry.mu.RUnlock()

	if ok && resolved.ID != "" {
		return &discordgo.ComponentEmoji{
			ID:       resolved.ID,
			Name:     resolved.Name,
			Animated: resolved.Animated,
		}
	}

	if asset, exists := assetByKey[key]; exists {
		return &discordgo.ComponentEmoji{
			Name: asset.UnicodeFallback,
		}
	}

	return nil
}

// GetFormattedEmoji returns a Markdown emoji string.
func GetFormattedEmoji(key string) string {
	globalEmojiRegistry.mu.RLock()
	resolved, ok := globalEmojiRegistry.resolved[key]
	globalEmojiRegistry.mu.RUnlock()

	if ok && resolved.ID != "" {
		if resolved.Animated {
			return fmt.Sprintf("<a:%s:%s>", resolved.Name, resolved.ID)
		}
		return fmt.Sprintf("<:%s:%s>", resolved.Name, resolved.ID)
	}

	if asset, exists := assetByKey[key]; exists {
		return asset.UnicodeFallback
	}

	return ""
}

func ComponentEmoji(name, id string, animated bool) *discordgo.ComponentEmoji {
	return &discordgo.ComponentEmoji{
		Name:     name,
		ID:       id,
		Animated: animated,
	}
}

func FormatEmoji(name, id string, animated bool) string {
	if animated {
		return fmt.Sprintf("<a:%s:%s>", name, id)
	}
	return fmt.Sprintf("<:%s:%s>", name, id)
}
