package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

var (
	defaultHTTPClient = helpers.NewSafeHTTPClient(helpers.DurationTimeoutHTTP)
	bingImageRegex    = regexp.MustCompile(`class="iusc"[^>]*?m="([^"]+)"`)
)

type UrbanCmd struct{}

func (c *UrbanCmd) Name() string      { return "urban" }
func (c *UrbanCmd) Aliases() []string { return []string{"ud", "ub"} }
func (c *UrbanCmd) Category() string  { return "Utility" }
func (c *UrbanCmd) Description() string {
	return "Searches Urban Dictionary for a word or phrase"
}
func (c *UrbanCmd) Usage() string   { return "<term>" }
func (c *UrbanCmd) Example() string { return "ratio" }

func (c *UrbanCmd) Execute(ctx *bot.Context) error {
	term := strings.Join(ctx.Args, " ")
	resp, err := fetchUrbanDefinitions(ctx.Context(), term)

	if err != nil || resp == nil || len(resp.List) == 0 {
		desc := "No Urban Dictionary definition found."
		if term != "" {
			desc = fmt.Sprintf("No Urban Dictionary definition found for **%s**.", term)
		}
		return ctx.SendError(desc)
	}

	var embeds []*discordgo.MessageEmbed
	for i, entry := range resp.List {
		desc := entry.Definition
		if len([]rune(desc)) > 1950 {
			desc = helpers.TruncateString(desc, 1950) + "... *(definition truncated)*"
		}

		fields := []*discordgo.MessageEmbedField{}
		if entry.Example != "" {
			ex := entry.Example
			if len([]rune(ex)) > 1000 {
				ex = helpers.TruncateStringWithEllipsis(ex, 1000)
			}
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   "Example",
				Value:  ex,
				Inline: false,
			})
		}

		embed := &discordgo.MessageEmbed{
			Title:       entry.Word,
			URL:         entry.Permalink,
			Description: desc,
			Fields:      fields,
			Footer: &discordgo.MessageEmbedFooter{
				Text: fmt.Sprintf("Definition %d of %d", i+1, len(resp.List)),
			},
		}
		embeds = append(embeds, embed)
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

func fetchUrbanDefinitions(ctx context.Context, term string) (*UrbanAPIResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	term = strings.TrimSpace(term)
	apiURL := "https://api.urbandictionary.com/v0/random"
	if term != "" {
		apiURL = "https://api.urbandictionary.com/v0/define?term=" + url.QueryEscape(term)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", helpers.GetUserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result UrbanAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	return &result, nil
}

type UrbanAPIResponse struct {
	List []UrbanDefinitionEntry `json:"list"`
}

type UrbanDefinitionEntry struct {
	Word       string    `json:"word"`
	Definition string    `json:"definition"`
	Example    string    `json:"example"`
	Author     string    `json:"author"`
	Permalink  string    `json:"permalink"`
	ThumbsUp   int       `json:"thumbs_up"`
	ThumbsDown int       `json:"thumbs_down"`
	WrittenOn  time.Time `json:"written_on"`
}

type DefineCmd struct{}

func (c *DefineCmd) Name() string      { return "define" }
func (c *DefineCmd) Aliases() []string { return []string{"def", "dict"} }
func (c *DefineCmd) Category() string  { return "Utility" }
func (c *DefineCmd) Description() string {
	return "Searches for a word's definition and pronunciation."
}
func (c *DefineCmd) Usage() string   { return "<word>" }
func (c *DefineCmd) Example() string { return "ephemeral" }

func (c *DefineCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	word := strings.Join(ctx.Args, " ")
	apiURL := "https://api.dictionaryapi.dev/api/v2/entries/en/" + url.PathEscape(word)

	req, err := http.NewRequestWithContext(ctx.Context(), http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Failed to query dictionary for **%s**.", word))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ctx.SendError(fmt.Sprintf("No dictionary definition found for **%s**.", word))
	} else if resp.StatusCode != http.StatusOK {
		return ctx.SendError(fmt.Sprintf("Unexpected API error (%d) searching for **%s**.", resp.StatusCode, word))
	}

	var dictResp []DictEntry
	if err := json.NewDecoder(resp.Body).Decode(&dictResp); err != nil || len(dictResp) == 0 {
		return ctx.SendError(fmt.Sprintf("Failed to parse dictionary results for **%s**.", word))
	}

	entry := dictResp[0]
	var audioURL, phoneticText string
	for _, p := range entry.Phonetics {
		if p.Text != "" && phoneticText == "" {
			phoneticText = p.Text
		}
		if p.Audio != "" && audioURL == "" {
			audioURL = p.Audio
			if strings.HasPrefix(audioURL, "//") {
				audioURL = "https:" + audioURL
			}
		}
	}

	var embeds []*discordgo.MessageEmbed
	for i, meaning := range entry.Meanings {
		desc := ""
		if phoneticText != "" {
			desc += fmt.Sprintf("**Pronunciation:** %s\n", phoneticText)
		}
		if audioURL != "" {
			desc += fmt.Sprintf("[Listen to Audio](%s)\n", audioURL)
		}

		embed := &discordgo.MessageEmbed{
			Title:       fmt.Sprintf("%s (%s)", entry.Word, meaning.PartOfSpeech),
			Description: desc,
		}

		if len(entry.SourceUrls) > 0 {
			embed.URL = entry.SourceUrls[0]
		}

		for j, def := range meaning.Definitions {
			if j >= 3 {
				break
			}
			val := def.Definition
			if def.Example != "" {
				val += fmt.Sprintf("\n*Example: \"%s\"*", def.Example)
			}
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:   fmt.Sprintf("Definition %d", j+1),
				Value:  val,
				Inline: false,
			})
		}

		if len(meaning.Synonyms) > 0 {
			syns := strings.Join(meaning.Synonyms, ", ")
			if len([]rune(syns)) > 1000 {
				syns = helpers.TruncateStringWithEllipsis(syns, 1000)
			}
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:   "Synonyms",
				Value:  syns,
				Inline: false,
			})
		}

		embed.Footer = &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Meaning %d of %d", i+1, len(entry.Meanings)),
		}
		embeds = append(embeds, embed)
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

type DictEntry struct {
	Word      string `json:"word"`
	Phonetics []struct {
		Text  string `json:"text"`
		Audio string `json:"audio"`
	} `json:"phonetics"`
	Meanings []struct {
		PartOfSpeech string `json:"partOfSpeech"`
		Definitions  []struct {
			Definition string `json:"definition"`
			Example    string `json:"example"`
		} `json:"definitions"`
		Synonyms []string `json:"synonyms"`
	} `json:"meanings"`
	SourceUrls []string `json:"sourceUrls"`
}

func SearchImages(ctx context.Context, query string) ([]DDGImageResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint := fmt.Sprintf("https://www.bing.com/images/async?q=%s&async=1&first=1&count=35", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	matches := bingImageRegex.FindAllSubmatch(body, 35)
	if len(matches) == 0 {
		return nil, nil
	}

	var results []DDGImageResult
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		rawJSON := html.UnescapeString(string(m[1]))
		var item struct {
			Title string `json:"t"`
			MURL  string `json:"murl"`
			TURL  string `json:"turl"`
			PURL  string `json:"purl"`
		}
		if err := json.Unmarshal([]byte(rawJSON), &item); err == nil {
			imgURL := item.TURL
			if imgURL == "" {
				imgURL = item.MURL
			}
			if imgURL == "" {
				continue
			}
			title := strings.TrimSpace(item.Title)
			if title == "" {
				title = query
			}
			results = append(results, DDGImageResult{
				Title: title,
				Image: imgURL,
				URL:   item.PURL,
			})
		}
	}

	return results, nil
}

type DDGImageResult struct {
	Title string `json:"title"`
	Image string `json:"image"`
	URL   string `json:"url"`
}

type DDGResponse struct {
	Results []DDGImageResult `json:"results"`
}

type ImageCmd struct{}

func (c *ImageCmd) Name() string        { return "image" }
func (c *ImageCmd) Aliases() []string   { return []string{"img", "gis"} }
func (c *ImageCmd) Category() string    { return "Utility" }
func (c *ImageCmd) Description() string { return "Searches the web for an image matching the query." }
func (c *ImageCmd) Usage() string       { return "<query>" }
func (c *ImageCmd) Example() string     { return "Juice WRLD aesthetic" }

func (c *ImageCmd) Execute(ctx *bot.Context) error {
	if !ctx.RequireArgs(c, 1) {
		return nil
	}

	query := strings.Join(ctx.Args, " ")
	results, err := SearchImages(ctx.Context(), query)
	if err != nil {
		return ctx.SendError(fmt.Sprintf("Image search failed: %v", err))
	}
	if len(results) == 0 {
		return ctx.SendError(fmt.Sprintf("No image results found for **%s**.", query))
	}

	var embeds []*discordgo.MessageEmbed
	for i, res := range results {
		embed := &discordgo.MessageEmbed{
			Title: res.Title,
			URL:   res.URL,
			Image: &discordgo.MessageEmbedImage{
				URL: res.Image,
			},
			Footer: &discordgo.MessageEmbedFooter{
				Text:    fmt.Sprintf("Page %d of %d", i+1, len(results)),
				IconURL: "https://www.bing.com/sa/simg/favicon-trans-bg-blue-mg-png.png",
			},
		}
		embeds = append(embeds, embed)
	}

	return ctx.SendPaginatedEmbeds(embeds)
}

type CatCmd struct{}

func (c *CatCmd) Name() string        { return "cat" }
func (c *CatCmd) Aliases() []string   { return []string{"kitty", "meow"} }
func (c *CatCmd) Category() string    { return "Media & Fun" }
func (c *CatCmd) Description() string { return "Displays a random cat image or GIF." }
func (c *CatCmd) Usage() string       { return "" }
func (c *CatCmd) Example() string     { return "" }

func (c *CatCmd) Execute(ctx *bot.Context) error {
	isGif := rand.Intn(2) == 0

	catURL := fetchCatAPI(ctx.Context(), isGif)

	if catURL == "" {
		timestamp := time.Now().UnixMilli()
		if isGif {
			catURL = fmt.Sprintf("https://cataas.com/cat/gif?t=%d", timestamp)
		} else {
			catURL = fmt.Sprintf("https://cataas.com/cat?t=%d", timestamp)
		}
	}

	_, err := ctx.ReplyText(catURL)
	return err
}

func fetchCatAPI(ctx context.Context, isGif bool) string {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint := "https://api.thecatapi.com/v1/images/search?mime_types=jpg,png"
	if isGif {
		endpoint = "https://api.thecatapi.com/v1/images/search?mime_types=gif"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var data []struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || len(data) == 0 {
		return ""
	}

	return data[0].URL
}

type BunnyCmd struct{}

func (c *BunnyCmd) Name() string        { return "bunny" }
func (c *BunnyCmd) Aliases() []string   { return []string{"rabbit", "bun"} }
func (c *BunnyCmd) Category() string    { return "Media & Fun" }
func (c *BunnyCmd) Description() string { return "Displays a random bunny image." }
func (c *BunnyCmd) Usage() string       { return "" }
func (c *BunnyCmd) Example() string     { return "" }

func (c *BunnyCmd) Execute(ctx *bot.Context) error {
	imageURL := fetchPrimaryBunny(ctx.Context())

	if imageURL == "" {
		imageURL = fetchBackupBunny(ctx.Context())
	}

	if imageURL == "" {
		return c.fallbackError(ctx)
	}

	_, err := ctx.ReplyText(imageURL)
	return err
}

func fetchPrimaryBunny(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint := "https://rabbit-api-two.vercel.app/api/random"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var data struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.URL == "" {
		return ""
	}

	return data.URL
}

func fetchBackupBunny(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint := "https://api.bunnies.io/v2/loop/random/?media=gif,png"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var data struct {
		Media struct {
			GIF    string `json:"gif"`
			Poster string `json:"poster"`
		} `json:"media"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}

	if data.Media.GIF != "" {
		return data.Media.GIF
	}
	return data.Media.Poster
}

func (c *BunnyCmd) fallbackError(ctx *bot.Context) error {
	return ctx.SendError("Failed to fetch a bunny image from all sources.")
}

type DogCmd struct{}

func (c *DogCmd) Name() string        { return "dog" }
func (c *DogCmd) Aliases() []string   { return []string{"puppy", "doggo"} }
func (c *DogCmd) Category() string    { return "Media & Fun" }
func (c *DogCmd) Description() string { return "Displays a random dog image." }
func (c *DogCmd) Usage() string       { return "" }
func (c *DogCmd) Example() string     { return "" }

func (c *DogCmd) Execute(ctx *bot.Context) error {
	endpoint := "https://dog.ceo/api/breeds/image/random"

	req, err := http.NewRequestWithContext(ctx.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		return c.fallbackError(ctx)
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return c.fallbackError(ctx)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.fallbackError(ctx)
	}

	var data struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Status != "success" || data.Message == "" {
		return c.fallbackError(ctx)
	}

	_, err = ctx.ReplyText(data.Message)
	return err
}

func (c *DogCmd) fallbackError(ctx *bot.Context) error {
	return ctx.SendError("Failed to fetch a dog image.")
}
