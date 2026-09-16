package graphics

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"sync"

	"gobot/internal/helpers"
)

const CellSize = 300

type ChartGenerator struct {
	HTTPClient *http.Client
}

func NewChartGenerator() *ChartGenerator {
	return &ChartGenerator{
		HTTPClient: helpers.NewSafeHTTPClient(helpers.DurationTimeoutHTTP),
	}
}

func (g *ChartGenerator) BuildGridChart(imageURLs []string, gridSize int) ([]byte, error) {
	if gridSize < 1 {
		gridSize = 3
	}
	totalCells := gridSize * gridSize
	if len(imageURLs) > totalCells {
		imageURLs = imageURLs[:totalCells]
	}

	canvasWidth := gridSize * CellSize
	canvasHeight := gridSize * CellSize
	canvas := image.NewRGBA(image.Rect(0, 0, canvasWidth, canvasHeight))

	downloadedImages := make([]image.Image, totalCells)
	var wg sync.WaitGroup

	for i, url := range imageURLs {
		if url == "" {
			continue
		}
		wg.Add(1)
		idx := i
		imgURL := url
		helpers.Spawn(func() {
			defer wg.Done()
			img, err := g.fetchImage(imgURL)
			if err == nil && img != nil {
				downloadedImages[idx] = img
			}
		})
	}

	wg.Wait()

	for i := 0; i < totalCells; i++ {
		row := i / gridSize
		col := i % gridSize
		x := col * CellSize
		y := row * CellSize

		targetRect := image.Rect(x, y, x+CellSize, y+CellSize)

		img := downloadedImages[i]
		if img == nil {
			draw.Draw(canvas, targetRect, &image.Uniform{color.RGBA{R: 30, G: 30, B: 30, A: 255}}, image.Point{}, draw.Src)
		} else {
			resized := scaleImage(img, CellSize, CellSize)
			draw.Draw(canvas, targetRect, resized, image.Point{}, draw.Over)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, fmt.Errorf("failed to encode chart png: %w", err)
	}

	return buf.Bytes(), nil
}

func (g *ChartGenerator) fetchImage(url string) (image.Image, error) {
	bodyBytes, _, err := helpers.FetchImageData(url)
	if err != nil {
		return nil, err
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	const maxDimension = 4096
	if cfg.Width > maxDimension || cfg.Height > maxDimension || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, fmt.Errorf("image dimensions (%dx%d) exceed maximum allowed (%dx%d)", cfg.Width, cfg.Height, maxDimension, maxDimension)
	}

	img, _, err := image.Decode(bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	return img, nil
}

func scaleImage(src image.Image, targetWidth, targetHeight int) image.Image {
	bounds := src.Bounds()
	if bounds.Dx() == targetWidth && bounds.Dy() == targetHeight {
		return src
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := 0; y < targetHeight; y++ {
		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + (x * bounds.Dx() / targetWidth)
			srcY := bounds.Min.Y + (y * bounds.Dy() / targetHeight)
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}
