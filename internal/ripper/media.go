package ripper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"gobot/internal/helpers"
)

const minMediaSizeBytes = 100

func validateMediaFiles(files []string, maxSizeBytes int64) ([]string, error) {
	var valid []string
	var totalSize int64
	for _, path := range files {
		fi, err := os.Stat(path)
		if err != nil || fi.Size() < minMediaSizeBytes {
			continue
		}
		if maxSizeBytes > 0 && fi.Size() > maxSizeBytes {
			if len(files) == 1 {
				return nil, ErrMediaTooLarge
			}
			continue
		}
		if maxSizeBytes > 0 && totalSize+fi.Size() > maxSizeBytes {
			if len(valid) > 0 {
				break
			}
			return nil, ErrMediaTooLarge
		}
		totalSize += fi.Size()
		valid = append(valid, path)
	}

	if len(valid) == 0 {
		return nil, ErrNoValidMedia
	}
	return valid, nil
}

func embedID3Artwork(ctx context.Context, audioPath string, thumbnailURL string) string {
	if thumbnailURL == "" || !helpers.IsURLSafe(thumbnailURL) {
		return audioPath
	}

	thumbData, _, err := helpers.FetchImageData(thumbnailURL)
	if err != nil || len(thumbData) == 0 {
		return audioPath
	}

	thumbFile := filepath.Join(filepath.Dir(audioPath), fmt.Sprintf("thumb_%s.jpg", filepath.Base(audioPath)))
	if err := os.WriteFile(thumbFile, thumbData, 0600); err != nil {
		return audioPath
	}
	defer os.Remove(thumbFile)

	outputPath := filepath.Join(filepath.Dir(audioPath), fmt.Sprintf("tagged_%s", filepath.Base(audioPath)))
	args := []string{
		"-y",
		"-i", audioPath,
		"-i", thumbFile,
		"-map", "0:0",
		"-map", "1:0",
		"-c", "copy",
		"-id3v2_version", "3",
		"-metadata:s:v", "title=\"Album cover\"",
		"-metadata:s:v", "comment=\"Cover (front)\"",
		outputPath,
	}

	cmd := exec.CommandContext(ctx, helpers.GetFFmpegPath(), args...)
	if err := cmd.Run(); err != nil {
		return audioPath
	}
	return outputPath
}
