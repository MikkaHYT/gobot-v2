package graphics

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"
	"sync"
	"gobot/internal/helpers"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

type LevelCardData struct {
	Username      string
	Discriminator string
	ServerRank    int
	Level         int
	CurrentXP     int
	NextLevelXP   int
	PrevLevelXP   int
	TotalMessages int
	CurrentRole   string
	NextRole      string
	NextRoleLevel int
	AvatarURL     string
	DecorationURL string
}
var (
	graphicsMu       sync.RWMutex
	customFontPath   string
	defaultCardTheme string
)

func ConfigureGraphics(fontPath, theme string) {
	graphicsMu.Lock()
	defer graphicsMu.Unlock()
	customFontPath = fontPath
	defaultCardTheme = strings.ToLower(theme)
}

func loadFont(dc *gg.Context, size float64, isBold bool) {
	graphicsMu.RLock()
	fp := customFontPath
	graphicsMu.RUnlock()
	if fp != "" {
		if _, errStat := os.Stat(fp); errStat == nil {
			if err := dc.LoadFontFace(fp, size); err == nil {
				return
			}
		}
	}
	var fontBytes []byte
	if isBold {
		fontBytes = gobold.TTF
	} else {
		fontBytes = goregular.TTF
	}

	f, err := truetype.Parse(fontBytes)
	if err == nil && f != nil {
		face := truetype.NewFace(f, &truetype.Options{Size: size})
		dc.SetFontFace(face)
	}
}

type cardThemePalette struct {
	bg     string
	panel  string
	border string
	accent string
	track  string
	badge  string
}

func resolveCardThemePalette(theme string) cardThemePalette {
	p := cardThemePalette{
		bg:     "#0E1017",
		panel:  "#161822",
		border: "#3D3B66",
		accent: "#7C5CFF",
		track:  "#222536",
		badge:  "#8F73FF",
	}

	switch theme {
	case "light":
		p.bg, p.panel, p.border, p.accent, p.track, p.badge = "#F8FAFC", "#FFFFFF", "#E2E8F0", "#6366F1", "#F1F5F9", "#4F46E5"
	case "oled":
		p.bg, p.panel, p.border, p.accent, p.track, p.badge = "#000000", "#0A0A0A", "#262626", "#00E5FF", "#171717", "#00E5FF"
	case "glassmorphism":
		p.bg, p.panel, p.border, p.accent, p.track, p.badge = "#0F172A", "#1E293B", "#334155", "#38BDF8", "#0F172A", "#38BDF8"
	case "vibrant":
		p.bg, p.panel, p.border, p.accent, p.track, p.badge = "#180828", "#28103C", "#4C1D95", "#FF007F", "#180828", "#FF007F"
	case "nord":
		p.bg, p.panel, p.border, p.accent, p.track, p.badge = "#2E3440", "#3B4252", "#4C566A", "#88C0D0", "#242933", "#81A1C1"
	case "emerald":
		p.bg, p.panel, p.border, p.accent, p.track, p.badge = "#06120E", "#0C231B", "#1A4234", "#10B981", "#081712", "#34D399"
	case "sunset":
		p.bg, p.panel, p.border, p.accent, p.track, p.badge = "#140C10", "#221218", "#48202A", "#FF5E7E", "#180D12", "#FF809B"
	case "amber":
		p.bg, p.panel, p.border, p.accent, p.track, p.badge = "#12100E", "#1F1A14", "#423625", "#F59E0B", "#181512", "#FBBF24"
	}
	return p
}

func GenerateLevelCard(data LevelCardData) ([]byte, error) {
	const (
		width  = 1000
		height = 360
	)

	dc := gg.NewContext(width, height)

	graphicsMu.RLock()
	theme := defaultCardTheme
	graphicsMu.RUnlock()

	palette := resolveCardThemePalette(theme)
	drawCardBackground(dc, palette, width, height)

	avatarCX, avatarCY, avatarR := 130.0, 180.0, 80.0
	drawCardAvatar(dc, data, palette, avatarCX, avatarCY, avatarR)

	textX := 240.0
	drawCardHeader(dc, data, textX)

	barX, barY, barW, barH := textX, 195.0, 700.0, 36.0
	drawCardProgressBar(dc, data, palette, barX, barY, barW, barH)

	roleY := 285.0
	drawCardRoles(dc, data, barX, barW, roleY)

	var buf bytes.Buffer
	if err := dc.EncodePNG(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func drawCardBackground(dc *gg.Context, p cardThemePalette, width, height float64) {
	dc.SetHexColor(p.bg)
	dc.Clear()

	margin := 15.0
	cardX := margin
	cardY := margin
	cardW := width - margin*2
	cardH := height - margin*2

	dc.DrawRoundedRectangle(cardX, cardY, cardW, cardH, 28)
	dc.SetHexColor(p.panel)
	dc.FillPreserve()
	dc.SetLineWidth(2.5)
	dc.SetHexColor(p.border)
	dc.Stroke()
}

func drawCardAvatar(dc *gg.Context, data LevelCardData, p cardThemePalette, avatarCX, avatarCY, avatarR float64) {
	dc.DrawCircle(avatarCX, avatarCY, avatarR+6)
	dc.SetHexColor(p.accent)
	dc.Fill()

	dc.DrawCircle(avatarCX, avatarCY, avatarR+2)
	dc.SetHexColor(p.panel)
	dc.Fill()

	if data.AvatarURL != "" {
		avImg, err := downloadImage(data.AvatarURL)
		if err == nil && avImg != nil {
			targetSize := int(avatarR * 2)
			resizedAv := resizeImage(avImg, targetSize, targetSize)

			avDC := gg.NewContext(targetSize, targetSize)
			avDC.DrawCircle(avatarR, avatarR, avatarR)
			avDC.Clip()
			avDC.DrawImageAnchored(resizedAv, int(avatarR), int(avatarR), 0.5, 0.5)

			dc.DrawImageAnchored(avDC.Image(), int(avatarCX), int(avatarCY), 0.5, 0.5)
		}
	}

	if data.DecorationURL != "" {
		decImg, err := downloadImage(data.DecorationURL)
		if err == nil && decImg != nil {
			decSize := int(avatarR * 2 * 1.22)
			resizedDec := resizeImage(decImg, decSize, decSize)
			dc.DrawImageAnchored(resizedDec, int(avatarCX), int(avatarCY), 0.5, 0.5)
		}
	}
}

func drawCardHeader(dc *gg.Context, data LevelCardData, textX float64) {
	loadFont(dc, 38, true)
	dc.SetHexColor("#FFFFFF")
	dc.DrawStringAnchored(data.Username, textX, 75, 0, 0.5)

	loadFont(dc, 22, false)
	dc.SetHexColor("#A0A5BD")
	rankText := fmt.Sprintf("#%d Rank  •  %s Messages", data.ServerRank, helpers.FormatNumber(data.TotalMessages))
	dc.DrawStringAnchored(rankText, textX, 115, 0, 0.5)
}

func drawCardProgressBar(dc *gg.Context, data LevelCardData, p cardThemePalette, barX, barY, barW, barH float64) {
	currentLevelXP := data.CurrentXP - data.PrevLevelXP
	neededXP := data.NextLevelXP - data.PrevLevelXP
	if neededXP <= 0 {
		neededXP = 1
	}
	progressRatio := float64(currentLevelXP) / float64(neededXP)
	if progressRatio < 0 {
		progressRatio = 0
	}
	if progressRatio > 1 {
		progressRatio = 1
	}
	progressPercent := int(progressRatio * 100)

	loadFont(dc, 32, true)
	dc.SetHexColor(p.badge)
	dc.DrawStringAnchored(fmt.Sprintf("LEVEL %d", data.Level), barX, barY-30, 0, 0.5)

	loadFont(dc, 24, true)
	dc.SetHexColor("#FFFFFF")
	xpStr := fmt.Sprintf("%s / %s XP  (%d%%)", helpers.FormatNumber(data.CurrentXP), helpers.FormatNumber(data.NextLevelXP), progressPercent)
	dc.DrawStringAnchored(xpStr, barX+barW, barY-30, 1, 0.5)

	dc.DrawRoundedRectangle(barX, barY, barW, barH, 18)
	dc.SetHexColor(p.track)
	dc.Fill()

	if progressRatio > 0 {
		fillW := barW * progressRatio
		if fillW < 36 {
			fillW = 36
		}
		dc.DrawRoundedRectangle(barX, barY, fillW, barH, 18)
		dc.SetHexColor(p.accent)
		dc.Fill()
	}
}

func drawCardRoles(dc *gg.Context, data LevelCardData, barX, barW, roleY float64) {
	loadFont(dc, 22, false)
	if data.CurrentRole != "" {
		dc.SetHexColor("#00B894")
		dc.DrawStringAnchored(fmt.Sprintf("Role: %s", data.CurrentRole), barX, roleY, 0, 0.5)
	} else {
		dc.SetHexColor("#A0A5BD")
		dc.DrawStringAnchored("Role: None", barX, roleY, 0, 0.5)
	}

	if data.NextRole != "" {
		dc.SetHexColor("#FDCB6E")
		nextStr := fmt.Sprintf("Next: %s (Level %d)", data.NextRole, data.NextRoleLevel)
		dc.DrawStringAnchored(nextStr, barX+barW, roleY, 1, 0.5)
	}
}

func resizeImage(src image.Image, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func downloadImage(url string) (image.Image, error) {
	data, _, err := helpers.FetchImageData(url)
	if err != nil {
		return nil, err
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, img, bounds.Min, draw.Src)
	return dst, nil
}
