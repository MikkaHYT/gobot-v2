package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type OcrCmd struct{}

func (c *OcrCmd) Name() string        { return "ocr" }
func (c *OcrCmd) Aliases() []string   { return []string{"readtext", "image2text"} }
func (c *OcrCmd) Category() string    { return "Utility" }
func (c *OcrCmd) Description() string { return "Extract text from an image attachment, URL, or reply." }
func (c *OcrCmd) Usage() string       { return "[image_url | attachment | reply]" }
func (c *OcrCmd) Example() string {
	return "https://i.pinimg.com/564x/ba/7c/85/ba7c8560adc488ff5e299e1609cc2c73.jpg"
}

func (c *OcrCmd) Execute(ctx *bot.Context) error {
	imageURL := ctx.ExtractImageURL()
	if imageURL == "" {
		_, _ = ctx.SendUsage(c)
		return nil
	}

	if !strings.HasPrefix(imageURL, "https://") || !helpers.IsURLSafe(imageURL) {
		return ctx.SendError("Please provide a valid, secure HTTPS image URL.")
	}

	apiKey := ""
	if ctx.Config != nil {
		apiKey = ctx.Config.OCRSpaceAPIKey
	}

	extractedText, err := performOCR(ctx.Context(), apiKey, imageURL)
	if err != nil || strings.TrimSpace(extractedText) == "" {
		embed := &discordgo.MessageEmbed{
			Description: "Unable to read text from image.",
		}
		_, err = ctx.ReplyEmbed(embed)
		return err
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Extracted Text",
		Description: helpers.TruncateStringWithEllipsis(extractedText, 3000),
	}

	_, err = ctx.ReplyEmbed(embed)
	return err
}

type ocrSpaceResponse struct {
	ParsedResults []struct {
		ParsedText   string `json:"ParsedText"`
		ErrorMessage string `json:"ErrorMessage"`
	} `json:"ParsedResults"`
	OCRExitCode           int      `json:"OCRExitCode"`
	IsErroredOnProcessing bool     `json:"IsErroredOnProcessing"`
	ErrorMessage          []string `json:"ErrorMessage"`
}

func performOCR(ctx context.Context, apiKey, imgURL string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fileType := "PNG"
	lowerURL := strings.ToLower(imgURL)
	if strings.Contains(lowerURL, ".jpg") || strings.Contains(lowerURL, ".jpeg") {
		fileType = "JPG"
	} else if strings.Contains(lowerURL, ".webp") {
		fileType = "WEBP"
	} else if strings.Contains(lowerURL, ".gif") {
		fileType = "GIF"
	} else if strings.Contains(lowerURL, ".pdf") {
		fileType = "PDF"
	}

	formData := url.Values{}
	formData.Set("url", imgURL)
	formData.Set("filetype", fileType)
	formData.Set("detectOrientation", "true")
	formData.Set("scale", "true")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.ocr.space/parse/image", strings.NewReader(formData.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("apikey", apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	client := helpers.NewSafeHTTPClient(15 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ocr request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OCR HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return "", err
	}

	var res ocrSpaceResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return "", err
	}

	if res.IsErroredOnProcessing || len(res.ParsedResults) == 0 {
		return "", fmt.Errorf("OCR processing error")
	}

	return res.ParsedResults[0].ParsedText, nil
}
