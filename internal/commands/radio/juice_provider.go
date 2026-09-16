package radio

import (
	"context"
	"fmt"

	"gobot/internal/helpers"
	"gobot/internal/juicewrld"
	coordinator "gobot/internal/radio"
)

type JuiceTrackProvider struct {
	cache *coordinator.LRUSongCache
}

func NewJuiceTrackProvider(cache *coordinator.LRUSongCache) *JuiceTrackProvider {
	return &JuiceTrackProvider{cache: cache}
}

func (p *JuiceTrackProvider) NextTrack(ctx context.Context, guildID, mode string, recentTitles []string) (*coordinator.Track, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.cache != nil {
		if cachedTrack, _ := p.cache.RandomTrack(recentTitles, mode); cachedTrack != nil && coordinator.IsRadioEligible(cachedTrack) {
			clone := cachedTrack.Clone()
			p.ensurePlayableURL(ctx, &clone)
			return &clone, nil
		}
	}

	track, err := FetchRandomRadioSong(ctx, mode, recentTitles)
	if err == nil && track != nil && coordinator.IsRadioEligible(track) {
		return track, nil
	}

	if p.cache != nil {
		if cachedTrack, _ := p.cache.RandomTrack(nil, mode); cachedTrack != nil && coordinator.IsRadioEligible(cachedTrack) {
			clone := cachedTrack.Clone()
			p.ensurePlayableURL(ctx, &clone)
			return &clone, nil
		}
	}

	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no radio tracks available")
}

func (p *JuiceTrackProvider) ensurePlayableURL(ctx context.Context, t *coordinator.Track) {
	if t == nil {
		return
	}
	if helpers.IsDirectAudioURL(t.URL) {
		return
	}
	if songs, err := juicewrld.SearchJuiceWRLDSongs(ctx, t.Title, ""); err == nil && len(songs) > 0 {
		dl := juicewrld.GetDownloadURL(songs[0].Path)
		t.URL = dl
		t.WebpageURL = dl
	}
}
