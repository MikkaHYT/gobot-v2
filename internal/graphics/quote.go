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
	"os"
	"strings"

	"gobot/internal/helpers"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type QuoteCardOptions struct {
	AvatarBytes       []byte
	QuoteText         string
	AuthorDisplayName string
	AuthorUsername    string
	GuildName         string
	GuildIconBytes    []byte
	AccentColor       color.Color
}

type QuoteGenerator struct {
	HTTPClient *http.Client
}

func NewQuoteGenerator() *QuoteGenerator {
	return &QuoteGenerator{
		HTTPClient: helpers.NewSafeHTTPClient(helpers.DurationTimeoutHTTP),
	}
}

func (g *QuoteGenerator) FetchBytes(url string) ([]byte, error) {
	data, _, err := helpers.FetchImageData(url)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (g *QuoteGenerator) CreateQuoteCard(opts QuoteCardOptions) ([]byte, error) {
	const W, H = 1000, 500
	canvas := image.NewRGBA(image.Rect(0, 0, W, H))

	bgColor := color.RGBA{R: 10, G: 10, B: 11, A: 255}
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	accent := opts.AccentColor
	if accent == nil {
		accent = color.RGBA{R: 70, G: 70, B: 75, A: 255}
	}

	var avatarImg image.Image
	if len(opts.AvatarBytes) > 0 {
		decoded, _, err := image.Decode(bytes.NewReader(opts.AvatarBytes))
		if err == nil && decoded != nil {
			avatarImg = decoded
		}
	}

	if avatarImg == nil {
		placeholder := image.NewRGBA(image.Rect(0, 0, 500, 500))
		draw.Draw(placeholder, placeholder.Bounds(), &image.Uniform{color.RGBA{50, 50, 60, 255}}, image.Point{}, draw.Src)
		avatarImg = placeholder
	}

	scaledAvatar := scaleImage(avatarImg, 500, 500)

	mask := image.NewAlpha(image.Rect(0, 0, 500, 500))
	const fadeStart = 100
	const fadeEnd = 480
	for y := 0; y < 500; y++ {
		for x := 0; x < 500; x++ {
			var a uint8 = 255
			if x > fadeEnd {
				a = 0
			} else if x >= fadeStart {
				progress := float64(x-fadeStart) / float64(fadeEnd-fadeStart)
				a = uint8(255.0 * (1.0 - progress))
			}
			mask.SetAlpha(x, y, color.Alpha{A: a})
		}
	}

	draw.DrawMask(canvas, image.Rect(0, 0, 500, 500), scaledAvatar, image.Point{}, mask, image.Point{}, draw.Over)

	regFont := loadSystemFont("segoeui.ttf", "arial.ttf")
	italicFont := loadSystemFont("segoeuii.ttf", "ariali.ttf")

	quoteText := strings.TrimSpace(opts.QuoteText)
	if len([]rune(quoteText)) > 250 {
		quoteText = helpers.TruncateStringWithEllipsis(quoteText, 250)
	}

	runeLen := len([]rune(quoteText))
	fontSize := float64(42)
	if runeLen > 120 {
		fontSize = 28
	} else if runeLen > 60 {
		fontSize = 34
	}

	authorFace := createFontFace(italicFont, 22)
	userFace := createFontFace(regFont, 20)
	guildFace := createFontFace(regFont, 17)

	const rightCenterX = 750
	const maxTextWidth = 440
	const minQuoteFontSize = 18
	const topPadding = 40

	metaHeight := 25 + 24
	if opts.GuildName != "" {
		metaHeight += 26
	}

	var qFace font.Face
	var lines []string
	var lineHeight int
	var startY int

	for {
		qFace = createFontFace(regFont, fontSize)
		lines = wrapText(quoteText, qFace, maxTextWidth)
		lineHeight = int(fontSize) + 8
		startY = (H - (len(lines)*lineHeight + metaHeight)) / 2
		if startY >= topPadding || fontSize <= minQuoteFontSize {
			break
		}
		fontSize -= 4
	}
	if startY < topPadding {
		startY = topPadding
	}

	currY := startY

	textColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for _, line := range lines {
		lw := measureStringWidth(qFace, line)
		lineX := rightCenterX - (lw / 2)
		drawString(canvas, qFace, lineX, currY+int(fontSize), line, textColor)
		currY += lineHeight
	}

	currY += 25

	authorText := fmt.Sprintf("- %s", opts.AuthorDisplayName)
	authorWidth := measureStringWidth(authorFace, authorText)
	authorX := rightCenterX - (authorWidth / 2)
	drawString(canvas, authorFace, authorX, currY+22, authorText, textColor)
	currY += 24

	userText := fmt.Sprintf("@%s", opts.AuthorUsername)
	userWidth := measureStringWidth(userFace, userText)
	userX := rightCenterX - (userWidth / 2)
	subColor := color.RGBA{R: 160, G: 160, B: 160, A: 255}
	drawString(canvas, userFace, userX, currY+20, userText, subColor)
	currY += 26

	if opts.GuildName != "" {
		guildText := opts.GuildName
		guildWidth := measureStringWidth(guildFace, guildText)
		guildX := rightCenterX - (guildWidth / 2)
		guildColor := color.RGBA{R: 130, G: 130, B: 130, A: 255}
		drawString(canvas, guildFace, guildX, currY+17, guildText, guildColor)
	}

	var outBuf bytes.Buffer
	if err := png.Encode(&outBuf, canvas); err != nil {
		return nil, fmt.Errorf("failed to encode quote PNG: %w", err)
	}

	return outBuf.Bytes(), nil
}

func loadSystemFont(fontNames ...string) *opentype.Font {
	fontDirs := []string{
		"C:/Windows/Fonts",
		"/usr/share/fonts/truetype",
		"/Library/Fonts",
	}

	for _, dir := range fontDirs {
		for _, name := range fontNames {
			path := dir + "/" + name
			if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
				if f, err := opentype.Parse(data); err == nil {
					return f
				}
			}
		}
	}
	if f, err := opentype.Parse(goregular.TTF); err == nil {
		return f
	}
	return nil
}

func createFontFace(f *opentype.Font, size float64) font.Face {
	if f != nil {
		face, err := opentype.NewFace(f, &opentype.FaceOptions{
			Size:    size,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err == nil {
			return face
		}
	}
	return basicfont.Face7x13
}

func measureStringWidth(face font.Face, text string) int {
	var w fixed.Int26_6
	for _, r := range text {
		advance, ok := face.GlyphAdvance(r)
		if ok {
			w += advance
		} else {
			w += fixed.I(8)
		}
	}
	return w.Ceil()
}

func drawString(dst draw.Image, face font.Face, x, y int, text string, c color.Color) {
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	d.DrawString(text)
}

func wrapText(text string, face font.Face, maxWidth int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	var currentLine []string

	for _, word := range words {
		testLine := strings.Join(append(currentLine, word), " ")
		if measureStringWidth(face, testLine) <= maxWidth {
			currentLine = append(currentLine, word)
		} else {
			if len(currentLine) > 0 {
				lines = append(lines, strings.Join(currentLine, " "))
				currentLine = nil
			}
			if measureStringWidth(face, word) > maxWidth {
				var chunk []rune
				for _, r := range word {
					chunk = append(chunk, r)
					if measureStringWidth(face, string(chunk)) > maxWidth {
						if len(chunk) > 1 {
							lines = append(lines, string(chunk[:len(chunk)-1]))
							chunk = []rune{r}
						}
					}
				}
				if len(chunk) > 0 {
					currentLine = []string{string(chunk)}
				}
			} else {
				currentLine = []string{word}
			}
		}
	}
	if len(currentLine) > 0 {
		lines = append(lines, strings.Join(currentLine, " "))
	}
	return lines
}
