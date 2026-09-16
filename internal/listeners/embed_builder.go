package listeners

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"gobot/internal/helpers"
	"gobot/internal/logger"

	"github.com/bwmarrin/discordgo"
)

type CustomButton struct {
	Label string `json:"label"`
	URL   string `json:"url,omitempty"`
	Emoji string `json:"emoji,omitempty"`
}

type EmbedSession struct {
	mu              sync.Mutex
	SessionID       string
	AuthorID        string
	GuildID         string
	ChannelID       string
	TargetChannelID string
	TargetMessageID string
	Embed           *discordgo.MessageEmbed
	Buttons         []CustomButton
	Created         time.Time
}

type EmbedSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*EmbedSession
	stopChan chan struct{}
	running  bool
	wg       sync.WaitGroup
}

var GlobalEmbedSessions = NewEmbedSessionStore()

func NewEmbedSessionStore() *EmbedSessionStore {
	return &EmbedSessionStore{
		sessions: make(map[string]*EmbedSession),
		stopChan: make(chan struct{}),
	}
}

func (s *EmbedSessionStore) Set(session *EmbedSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.SessionID] = session
}

func (s *EmbedSessionStore) Get(sessionID string) *EmbedSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[sessionID]
}

func (s *EmbedSessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *EmbedSessionStore) Start(ctx context.Context) {
	s.StartCleanupRoutine(ctx, 5*time.Minute, 30*time.Minute)
}

func (s *EmbedSessionStore) StartCleanupRoutine(ctx context.Context, interval, maxAge time.Duration) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopChan = make(chan struct{})
	stopCh := s.stopChan
	s.mu.Unlock()

	s.wg.Add(1)
	helpers.Spawn(func() {
		defer s.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.Lock()
				now := time.Now()
				for id, session := range s.sessions {
					if now.Sub(session.Created) > maxAge {
						delete(s.sessions, id)
					}
				}
				s.mu.Unlock()
			}
		}
	})
}

func (s *EmbedSessionStore) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopChan)
	s.mu.Unlock()

	s.wg.Wait()
}

func resolveChannelID(s *discordgo.Session, guildID, input string) string {
	ch, err := helpers.ResolveGuildChannel(s, guildID, input)
	if err != nil || ch == nil {
		return ""
	}
	return ch.ID
}

func LoadEmbedFromJSON(jsonStr string) (*discordgo.MessageEmbed, []CustomButton, error) {
	jsonStr = strings.TrimSpace(jsonStr)
	jsonStr = strings.TrimPrefix(jsonStr, "```json")
	jsonStr = strings.TrimPrefix(jsonStr, "```")
	jsonStr = strings.TrimSuffix(jsonStr, "```")
	jsonStr = strings.TrimSpace(jsonStr)

	if jsonStr == "" {
		return nil, nil, fmt.Errorf("JSON payload is empty")
	}

	var rawMap map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &rawMap); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON syntax: %w", err)
	}

	var buttons []CustomButton
	if btnsRaw, ok := rawMap["buttons"].([]any); ok {
		for _, b := range btnsRaw {
			if bMap, ok := b.(map[string]any); ok {
				cb := CustomButton{}
				if lbl, ok := bMap["label"].(string); ok {
					cb.Label = lbl
				}
				if u, ok := bMap["url"].(string); ok {
					cb.URL = u
				}
				if em, ok := bMap["emoji"].(string); ok {
					cb.Emoji = em
				}
				buttons = append(buttons, cb)
			}
		}
	}

	embedData := rawMap
	if embedsRaw, ok := rawMap["embeds"].([]any); ok && len(embedsRaw) > 0 {
		if eMap, ok := embedsRaw[0].(map[string]any); ok {
			embedData = eMap
		}
	} else if embedRaw, ok := rawMap["embed"].(map[string]any); ok {
		embedData = embedRaw
	}

	embedBytes, err := json.Marshal(embedData)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to process embed data: %w", err)
	}

	var embed discordgo.MessageEmbed
	if err := json.Unmarshal(embedBytes, &embed); err != nil {
		return nil, nil, fmt.Errorf("failed to construct embed: %w", err)
	}

	return &embed, buttons, nil
}

func ExportEmbedToJSON(embed *discordgo.MessageEmbed, buttons []CustomButton) (string, error) {
	embedBytes, err := json.Marshal(embed)
	if err != nil {
		return "", err
	}

	var resultMap map[string]any
	if err := json.Unmarshal(embedBytes, &resultMap); err != nil {
		return "", err
	}

	if len(buttons) > 0 {
		resultMap["buttons"] = buttons
	}

	finalBytes, err := json.MarshalIndent(resultMap, "", "  ")
	if err != nil {
		return "", err
	}

	return string(finalBytes), nil
}

func CreateCustomButtonsView(buttons []CustomButton) []discordgo.MessageComponent {
	if len(buttons) == 0 {
		return nil
	}

	var components []discordgo.MessageComponent
	for i, b := range buttons {
		label := b.Label
		if label == "" {
			label = "Button"
		}
		var emoji *discordgo.ComponentEmoji
		if b.Emoji != "" {
			emoji = &discordgo.ComponentEmoji{Name: b.Emoji}
		}

		if strings.HasPrefix(b.URL, "http://") || strings.HasPrefix(b.URL, "https://") {
			components = append(components, discordgo.Button{
				Style: discordgo.LinkButton,
				Label: label,
				URL:   b.URL,
				Emoji: emoji,
			})
		} else {
			components = append(components, discordgo.Button{
				Style:    discordgo.SecondaryButton,
				Label:    label,
				Emoji:    emoji,
				CustomID: fmt.Sprintf("btn_custom_%d_%d", i, rand.Intn(9000)+1000),
				Disabled: true,
			})
		}
	}

	var rows []discordgo.MessageComponent
	for i := 0; i < len(components); i += 5 {
		end := i + 5
		if end > len(components) {
			end = len(components)
		}
		rows = append(rows, discordgo.ActionsRow{
			Components: components[i:end],
		})
	}

	return rows
}

func BuildEmbedBuilderComponents(sessionID string) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Title & Desc", CustomID: "emb_title_" + sessionID},
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Color", CustomID: "emb_color_" + sessionID},
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Author & Footer", CustomID: "emb_af_" + sessionID},
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Images", CustomID: "emb_img_" + sessionID},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Add Field", CustomID: "emb_addfield_" + sessionID},
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Clear Fields", CustomID: "emb_clrfield_" + sessionID},
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Add Button", CustomID: "emb_addbtn_" + sessionID},
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Clear Buttons", CustomID: "emb_clrbtn_" + sessionID},
			},
		},
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Import JSON", CustomID: "emb_import_" + sessionID},
				discordgo.Button{Style: discordgo.SecondaryButton, Label: "Export JSON", CustomID: "emb_export_" + sessionID},
				discordgo.Button{Style: discordgo.SuccessButton, Label: "Send Embed", CustomID: "emb_send_" + sessionID},
				discordgo.Button{Style: discordgo.DangerButton, Label: "Cancel", CustomID: "emb_cancel_" + sessionID},
			},
		},
	}
}

func OnEmbedInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionMessageComponent:
		action, sessionID, session, ok := authenticateEmbedInteraction(s, i, false)
		if !ok {
			return
		}
		handleEmbedButton(s, i, session, action, sessionID)

	case discordgo.InteractionModalSubmit:
		modalType, sessionID, session, ok := authenticateEmbedInteraction(s, i, true)
		if !ok {
			return
		}
		handleEmbedModal(s, i, session, modalType, sessionID)
	}
}

func authenticateEmbedInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, isModal bool) (string, string, *EmbedSession, bool) {
	if i == nil {
		return "", "", nil, false
	}

	if isModal {
		customID := i.ModalSubmitData().CustomID
		if !strings.HasPrefix(customID, "emb_modal_") {
			return "", "", nil, false
		}

		parts := strings.SplitN(customID, "_", 4)
		if len(parts) < 4 {
			return "", "", nil, false
		}

		modalType := parts[2]
		sessionID := parts[3]

		session := GlobalEmbedSessions.Get(sessionID)
		if session == nil {
			respond(s, i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Session expired.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return "", "", nil, false
		}

		var interactorID string
		if i.Member != nil && i.Member.User != nil {
			interactorID = i.Member.User.ID
		} else if i.User != nil {
			interactorID = i.User.ID
		}

		if interactorID == "" || interactorID != session.AuthorID {
			respond(s, i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "You are not authorized to modify this embed session.",
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return "", "", nil, false
		}

		return modalType, sessionID, session, true
	}

	customID := i.MessageComponentData().CustomID
	if !strings.HasPrefix(customID, "emb_") {
		return "", "", nil, false
	}

	parts := strings.SplitN(customID, "_", 3)
	if len(parts) < 3 {
		return "", "", nil, false
	}

	action := parts[1]
	sessionID := parts[2]

	session := GlobalEmbedSessions.Get(sessionID)
	if session == nil {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "This embed builder session has expired.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return "", "", nil, false
	}

	userID := ""
	if i.Member != nil && i.Member.User != nil {
		userID = i.Member.User.ID
	} else if i.User != nil {
		userID = i.User.ID
	}

	if userID != session.AuthorID {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Only <@%s> can interact with this embed builder session.", session.AuthorID),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return "", "", nil, false
	}

	return action, sessionID, session, true
}

func handleEmbedButton(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, action, sessionID string) {
	switch action {
	case "title":
		promptTitleModal(s, i, session, sessionID)
	case "color":
		promptColorModal(s, i, session, sessionID)
	case "af":
		promptAuthorFooterModal(s, i, session, sessionID)
	case "img":
		promptImagesModal(s, i, session, sessionID)
	case "addfield":
		promptAddFieldModal(s, i, sessionID)
	case "clrfield":
		clearEmbedFields(s, i, session, sessionID)
	case "addbtn":
		promptAddButtonModal(s, i, sessionID)
	case "clrbtn":
		clearEmbedButtons(s, i, session, sessionID)
	case "import":
		promptImportModal(s, i, sessionID)
	case "export":
		exportEmbedJSON(s, i, session)
	case "send":
		promptSendModal(s, i, sessionID)
	case "cancel":
		cancelEmbedSession(s, i, sessionID)
	}
}

func promptTitleModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, sessionID string) {
	session.mu.Lock()
	curTitle := session.Embed.Title
	curDesc := session.Embed.Description
	session.mu.Unlock()

	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "emb_modal_title_" + sessionID,
			Title:    "Edit Title & Description",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "title",
							Label:       "Embed Title",
							Style:       discordgo.TextInputShort,
							Required:    helpers.Ptr(false),
							Value:       curTitle,
							Placeholder: "Enter embed title...",
							MaxLength:   256,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "desc",
							Label:       "Embed Description",
							Style:       discordgo.TextInputParagraph,
							Required:    helpers.Ptr(false),
							Value:       curDesc,
							Placeholder: "Enter embed description...",
							MaxLength:   4000,
						},
					},
				},
			},
		},
	})
}

func promptColorModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, sessionID string) {
	curHex := ""
	session.mu.Lock()
	if session.Embed.Color != 0 {
		curHex = fmt.Sprintf("#%06x", session.Embed.Color)
	}
	session.mu.Unlock()

	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "emb_modal_color_" + sessionID,
			Title:    "Set Embed Color",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "color",
							Label:       "Hex Color / Name",
							Style:       discordgo.TextInputShort,
							Required:    helpers.Ptr(false),
							Value:       curHex,
							Placeholder: "#7289da (or red, blue, green)",
						},
					},
				},
			},
		},
	})
}

func promptAuthorFooterModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, sessionID string) {
	aName, aIcon, fText, fIcon := "", "", "", ""
	session.mu.Lock()
	if session.Embed.Author != nil {
		aName = session.Embed.Author.Name
		aIcon = session.Embed.Author.IconURL
	}
	if session.Embed.Footer != nil {
		fText = session.Embed.Footer.Text
		fIcon = session.Embed.Footer.IconURL
	}
	session.mu.Unlock()

	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "emb_modal_af_" + sessionID,
			Title:    "Author & Footer",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "author_name", Label: "Author Name", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Value: aName, MaxLength: 256},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "author_icon", Label: "Author Icon URL", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Value: aIcon},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "footer_text", Label: "Footer Text", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Value: fText, MaxLength: 2048},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "footer_icon", Label: "Footer Icon URL", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Value: fIcon},
					},
				},
			},
		},
	})
}

func promptImagesModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, sessionID string) {
	tURL, iURL := "", ""
	session.mu.Lock()
	if session.Embed.Thumbnail != nil {
		tURL = session.Embed.Thumbnail.URL
	}
	if session.Embed.Image != nil {
		iURL = session.Embed.Image.URL
	}
	session.mu.Unlock()

	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "emb_modal_img_" + sessionID,
			Title:    "Set Thumbnail & Main Image",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "thumb_url", Label: "Thumbnail Image URL", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Value: tURL},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "image_url", Label: "Main Image URL", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Value: iURL},
					},
				},
			},
		},
	})
}

func promptAddFieldModal(s *discordgo.Session, i *discordgo.InteractionCreate, sessionID string) {
	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "emb_modal_field_" + sessionID,
			Title:    "Add Field",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "field_name", Label: "Field Name / Title", Style: discordgo.TextInputShort, Required: helpers.Ptr(true), Placeholder: "Field Title", MaxLength: 256},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "field_val", Label: "Field Value / Content", Style: discordgo.TextInputParagraph, Required: helpers.Ptr(true), Placeholder: "Field Value", MaxLength: 1024},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "field_inline", Label: "Inline? (yes / no)", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Value: "no", MaxLength: 10},
					},
				},
			},
		},
	})
}

func clearEmbedFields(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, sessionID string) {
	session.mu.Lock()
	session.Embed.Fields = nil
	session.mu.Unlock()
	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{session.Embed},
			Components: BuildEmbedBuilderComponents(sessionID),
		},
	})
}

func promptAddButtonModal(s *discordgo.Session, i *discordgo.InteractionCreate, sessionID string) {
	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "emb_modal_btn_" + sessionID,
			Title:    "Add Button",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "btn_label", Label: "Button Label", Style: discordgo.TextInputShort, Required: helpers.Ptr(true), Placeholder: "e.g. Download, Stream", MaxLength: 80},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "btn_url", Label: "Button Link / URL (optional)", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Placeholder: "https://..."},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "btn_emoji", Label: "Button Emoji (optional)", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Placeholder: "🎵, 📁"},
					},
				},
			},
		},
	})
}

func clearEmbedButtons(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, sessionID string) {
	session.mu.Lock()
	session.Buttons = nil
	session.mu.Unlock()
	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{session.Embed},
			Components: BuildEmbedBuilderComponents(sessionID),
		},
	})
}

func promptImportModal(s *discordgo.Session, i *discordgo.InteractionCreate, sessionID string) {
	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "emb_modal_import_" + sessionID,
			Title:    "Import Embed JSON",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "json_input", Label: "Embed JSON Data", Style: discordgo.TextInputParagraph, Required: helpers.Ptr(true), Placeholder: `{"title": "My Title", "description": "My Desc"}`},
					},
				},
			},
		},
	})
}

func exportEmbedJSON(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession) {
	session.mu.Lock()
	jsonStr, err := ExportEmbedToJSON(session.Embed, session.Buttons)
	session.mu.Unlock()

	if err != nil {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Failed to export JSON: `%v`", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	content := fmt.Sprintf("### Embed JSON Export\n```json\n%s\n```", jsonStr)
	if len(jsonStr) >= 1900 {
		content = fmt.Sprintf("### Embed JSON Export\n```json\n%s\n```", helpers.TruncateString(jsonStr, 1900))
	}
	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func promptSendModal(s *discordgo.Session, i *discordgo.InteractionCreate, sessionID string) {
	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "emb_modal_send_" + sessionID,
			Title:    "Send Embed to Channel",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{CustomID: "channel_input", Label: "Channel (#channel, ID, or name)", Style: discordgo.TextInputShort, Required: helpers.Ptr(false), Placeholder: "Leave blank to send to current channel"},
					},
				},
			},
		},
	})
}

func cancelEmbedSession(s *discordgo.Session, i *discordgo.InteractionCreate, sessionID string) {
	GlobalEmbedSessions.Delete(sessionID)
	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    "*Embed builder session cancelled.*",
			Embeds:     nil,
			Components: []discordgo.MessageComponent{},
		},
	})
}

func handleEmbedModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, modalType, sessionID string) {
	data := i.ModalSubmitData()

	getVal := func(key string) string {
		for _, row := range data.Components {
			if actionRow, ok := row.(*discordgo.ActionsRow); ok {
				for _, comp := range actionRow.Components {
					if input, ok := comp.(*discordgo.TextInput); ok {
						if input.CustomID == key {
							return input.Value
						}
					}
				}
			}
		}
		return ""
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	switch modalType {
	case "title":
		applyTitleModal(session, getVal)
	case "color":
		applyColorModal(session, getVal)
	case "af":
		applyAuthorFooterModal(session, getVal)
	case "img":
		applyImagesModal(session, getVal)
	case "field":
		if !applyFieldModal(s, i, session, getVal) {
			return
		}
	case "btn":
		if !applyButtonModal(s, i, session, getVal) {
			return
		}
	case "import":
		if !applyImportModal(s, i, session, getVal) {
			return
		}
	case "send":
		applySendModal(s, i, session, sessionID, getVal)
		return
	}

	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{session.Embed},
			Components: BuildEmbedBuilderComponents(sessionID),
		},
	})
}

func applyTitleModal(session *EmbedSession, getVal func(string) string) {
	session.Embed.Title = strings.TrimSpace(getVal("title"))
	session.Embed.Description = strings.TrimSpace(getVal("desc"))
}

func applyColorModal(session *EmbedSession, getVal func(string) string) {
	session.Embed.Color = helpers.ParseHexColor(getVal("color"))
}

func applyAuthorFooterModal(session *EmbedSession, getVal func(string) string) {
	aName := strings.TrimSpace(getVal("author_name"))
	aIcon := strings.TrimSpace(getVal("author_icon"))
	fText := strings.TrimSpace(getVal("footer_text"))
	fIcon := strings.TrimSpace(getVal("footer_icon"))

	if aName != "" {
		session.Embed.Author = &discordgo.MessageEmbedAuthor{
			Name:    aName,
			IconURL: aIcon,
		}
	} else {
		session.Embed.Author = nil
	}

	if fText != "" || fIcon != "" {
		session.Embed.Footer = &discordgo.MessageEmbedFooter{
			Text:    fText,
			IconURL: fIcon,
		}
	} else {
		session.Embed.Footer = nil
	}
}

func applyImagesModal(session *EmbedSession, getVal func(string) string) {
	tURL := strings.TrimSpace(getVal("thumb_url"))
	iURL := strings.TrimSpace(getVal("image_url"))

	if tURL != "" {
		session.Embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: tURL}
	} else {
		session.Embed.Thumbnail = nil
	}

	if iURL != "" {
		session.Embed.Image = &discordgo.MessageEmbedImage{URL: iURL}
	} else {
		session.Embed.Image = nil
	}
}

func applyFieldModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, getVal func(string) string) bool {
	fName := strings.TrimSpace(getVal("field_name"))
	fVal := strings.TrimSpace(getVal("field_val"))
	fInlineStr := strings.ToLower(strings.TrimSpace(getVal("field_inline")))
	isInline := fInlineStr == "yes" || fInlineStr == "y" || fInlineStr == "true" || fInlineStr == "1"

	if len(session.Embed.Fields) >= 25 {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "An embed cannot have more than 25 fields.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return false
	}

	session.Embed.Fields = append(session.Embed.Fields, &discordgo.MessageEmbedField{
		Name:   fName,
		Value:  fVal,
		Inline: isInline,
	})
	return true
}

func applyButtonModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, getVal func(string) string) bool {
	lbl := strings.TrimSpace(getVal("btn_label"))
	uStr := strings.TrimSpace(getVal("btn_url"))
	em := strings.TrimSpace(getVal("btn_emoji"))

	if len(session.Buttons) >= 25 {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You cannot add more than 25 custom buttons.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return false
	}

	session.Buttons = append(session.Buttons, CustomButton{
		Label: lbl,
		URL:   uStr,
		Emoji: em,
	})
	return true
}

func applyImportModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, getVal func(string) string) bool {
	jInput := getVal("json_input")
	emb, btns, err := LoadEmbedFromJSON(jInput)
	if err != nil || emb == nil {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Failed to import JSON: `%v`", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return false
	}
	session.Embed = emb
	if len(btns) > 0 {
		session.Buttons = btns
	}
	return true
}

func applySendModal(s *discordgo.Session, i *discordgo.InteractionCreate, session *EmbedSession, sessionID string, getVal func(string) string) {
	chInput := strings.TrimSpace(getVal("channel_input"))
	targetChannelID := session.ChannelID
	if session.TargetChannelID != "" {
		targetChannelID = session.TargetChannelID
	}

	if chInput != "" {
		if resolved := resolveChannelID(s, session.GuildID, chInput); resolved != "" {
			targetChannelID = resolved
		}
	}

	perms, errP := s.UserChannelPermissions(session.AuthorID, targetChannelID)
	if errP != nil || (perms&discordgo.PermissionSendMessages == 0 || perms&discordgo.PermissionManageMessages == 0) {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "You do not have permission to send and manage messages in the target channel.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if valErr := ValidateEmbedSize(session.Embed); valErr != nil {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Cannot send embed: %v", valErr),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	customRows := CreateCustomButtonsView(session.Buttons)

	if session.TargetMessageID != "" {
		edit := &discordgo.MessageEdit{
			ID:         session.TargetMessageID,
			Channel:    targetChannelID,
			Embeds:     &[]*discordgo.MessageEmbed{session.Embed},
			Components: &[]discordgo.MessageComponent{},
		}
		if len(customRows) > 0 {
			edit.Components = &customRows
		}

		_, err := s.ChannelMessageEditComplex(edit)
		if err != nil {
			respond(s, i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("Failed to edit message `%s`: `%v`", session.TargetMessageID, err),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		GlobalEmbedSessions.Delete(sessionID)

		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Embed updated in <#%s>! [Jump to Message](%s)", targetChannelID, helpers.MessageURL(session.GuildID, targetChannelID, session.TargetMessageID)),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	msgSend := &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{session.Embed},
	}
	if len(customRows) > 0 {
		msgSend.Components = customRows
	}

	sentMsg, err := s.ChannelMessageSendComplex(targetChannelID, msgSend)
	if err != nil {
		respond(s, i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Failed to send embed to <#%s>: `%v`", targetChannelID, err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	GlobalEmbedSessions.Delete(sessionID)

	respond(s, i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("Embed sent to <#%s>! [Jump to Message](%s)", targetChannelID, helpers.MessageURL(session.GuildID, sentMsg.ChannelID, sentMsg.ID)),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}
func respond(s *discordgo.Session, i *discordgo.Interaction, resp *discordgo.InteractionResponse) {
	if err := s.InteractionRespond(i, resp); err != nil {
		logger.Warnf("[EMBED BUILDER] Failed to respond to interaction %s: %v", i.ID, err)
	}
}

func ValidateEmbedSize(embed *discordgo.MessageEmbed) error {
	if embed == nil {
		return fmt.Errorf("embed cannot be empty")
	}

	totalChars := 0
	if len(embed.Title) > 256 {
		return fmt.Errorf("embed title exceeds maximum length of 256 characters")
	}
	totalChars += len(embed.Title)

	if len(embed.Description) > 4096 {
		return fmt.Errorf("embed description exceeds maximum length of 4096 characters")
	}
	totalChars += len(embed.Description)

	if embed.Author != nil {
		if len(embed.Author.Name) > 256 {
			return fmt.Errorf("embed author name exceeds maximum length of 256 characters")
		}
		totalChars += len(embed.Author.Name)
	}

	if embed.Footer != nil {
		if len(embed.Footer.Text) > 2048 {
			return fmt.Errorf("embed footer text exceeds maximum length of 2048 characters")
		}
		totalChars += len(embed.Footer.Text)
	}

	if len(embed.Fields) > 25 {
		return fmt.Errorf("embed cannot contain more than 25 fields")
	}

	for idx, f := range embed.Fields {
		if len(f.Name) > 256 {
			return fmt.Errorf("field %d title exceeds 256 characters", idx+1)
		}
		if len(f.Value) > 1024 {
			return fmt.Errorf("field %d value exceeds 1024 characters", idx+1)
		}
		totalChars += len(f.Name) + len(f.Value)
	}

	if totalChars > 6000 {
		return fmt.Errorf("total embed character count (%d) exceeds Discord's maximum limit of 6000 characters", totalChars)
	}

	return nil
}
