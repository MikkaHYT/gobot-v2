package helpers

import (
	"encoding/json"
	"fmt"

	"github.com/bwmarrin/discordgo"
)

const (
	CV2FlagsComponentsV2 = 32768

	ComponentTypeActionRow   = 1
	ComponentTypeButton      = 2
	ComponentTypeSelectMenu  = 3
	ComponentTypeTextInput   = 4
	ComponentTypeSection     = 9
	ComponentTypeTextDisplay = 10
	ComponentTypeMedia       = 11
	ComponentTypeSeparator   = 14
	ComponentTypeContainer   = 17

	ButtonStylePrimary   = 1
	ButtonStyleSecondary = 2
	ButtonStyleSuccess   = 3
	ButtonStyleDanger    = 4
	ButtonStyleLink      = 5
)

type rawContainerComponent struct {
	Components []map[string]interface{} `json:"components"`
}

func (c rawContainerComponent) Type() discordgo.ComponentType {
	return discordgo.ComponentType(ComponentTypeContainer)
}

func (c rawContainerComponent) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":       ComponentTypeContainer,
		"components": c.Components,
	})
}

func TextDisplay(content string) map[string]interface{} {
	return map[string]interface{}{
		"type":    ComponentTypeTextDisplay,
		"content": content,
	}
}

func Separator() map[string]interface{} {
	return map[string]interface{}{
		"type":    ComponentTypeSeparator,
		"divider": true,
	}
}

const DefaultThumbnailURL = "https://cdn.discordapp.com/embed/avatars/0.png"

func MediaAccessory(url string) map[string]interface{} {
	if url == "" {
		url = DefaultThumbnailURL
	}
	return map[string]interface{}{
		"type": ComponentTypeMedia,
		"media": map[string]interface{}{
			"url": url,
		},
	}
}

func SectionWithAccessory(accessory map[string]interface{}, components ...map[string]interface{}) map[string]interface{} {
	if len(accessory) == 0 {
		accessory = MediaAccessory(DefaultThumbnailURL)
	} else if media, ok := accessory["media"].(map[string]interface{}); ok {
		if url, _ := media["url"].(string); url == "" {
			accessory["media"] = map[string]interface{}{
				"url": DefaultThumbnailURL,
			}
		}
	}

	return map[string]interface{}{
		"type":       ComponentTypeSection,
		"components": components,
		"accessory":  accessory,
	}
}

func LinkButton(label, url string) map[string]interface{} {
	return map[string]interface{}{
		"type":  ComponentTypeButton,
		"style": ButtonStyleLink,
		"label": label,
		"url":   url,
	}
}

func ActionRow(components ...map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":       ComponentTypeActionRow,
		"components": components,
	}
}

func PaginationRow(prevCustomID, nextCustomID string, currentPage, totalPages int) map[string]interface{} {
	return map[string]interface{}{
		"type": ComponentTypeActionRow,
		"components": []map[string]interface{}{
			{
				"type":      ComponentTypeButton,
				"custom_id": prevCustomID,
				"label":     "◀ Previous",
				"style":     ButtonStyleSecondary,
				"disabled":  currentPage <= 0,
			},
			{
				"type":      ComponentTypeButton,
				"custom_id": "page_indicator",
				"label":     fmt.Sprintf("Page %d/%d", currentPage+1, totalPages),
				"style":     ButtonStyleSecondary,
				"disabled":  true,
			},
			{
				"type":      ComponentTypeButton,
				"custom_id": nextCustomID,
				"label":     "Next ▶",
				"style":     ButtonStyleSecondary,
				"disabled":  currentPage >= totalPages-1,
			},
		},
	}
}

func Container(components ...map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":       ComponentTypeContainer,
		"components": components,
	}
}

func BuildCV2Payload(components []map[string]interface{}, extraRootComponents []map[string]interface{}) map[string]interface{} {
	rootComponents := []map[string]interface{}{
		Container(components...),
	}
	if len(extraRootComponents) > 0 {
		rootComponents = append(rootComponents, extraRootComponents...)
	}
	return map[string]interface{}{
		"flags":      CV2FlagsComponentsV2,
		"components": rootComponents,
		"embeds":     []interface{}{},
		"content":    "",
		"allowed_mentions": map[string]interface{}{
			"parse": []string{},
		},
	}
}

func SendCV2Message(s *discordgo.Session, channelID string, components []map[string]interface{}, extraRootComponents []map[string]interface{}) error {
	_, err := SendCV2MessageAndReturn(s, channelID, components, extraRootComponents)
	return err
}

func SendCV2MessageAndReturn(s *discordgo.Session, channelID string, components []map[string]interface{}, extraRootComponents []map[string]interface{}) (*discordgo.Message, error) {
	if s == nil || channelID == "" || len(components) == 0 {
		return nil, nil
	}
	payload := BuildCV2Payload(components, extraRootComponents)
	endpoint := discordgo.EndpointChannelMessages(channelID)
	body, err := s.RequestWithBucketID("POST", endpoint, payload, endpoint)
	if err != nil {
		return nil, err
	}
	var msg discordgo.Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func PatchCV2Message(s *discordgo.Session, channelID string, messageID string, components []map[string]interface{}, extraRootComponents []map[string]interface{}) error {
	if s == nil || channelID == "" || messageID == "" || len(components) == 0 {
		return nil
	}
	payload := BuildCV2Payload(components, extraRootComponents)
	endpoint := discordgo.EndpointChannelMessage(channelID, messageID)
	_, err := s.RequestWithBucketID("PATCH", endpoint, payload, discordgo.EndpointChannelMessage(channelID, ""))
	return err
}

func SendCV2MessageWithFiles(s *discordgo.Session, channelID string, components []map[string]interface{}, files []*discordgo.File) error {
	_, err := SendCV2MessageWithFilesAndReturn(s, channelID, components, files)
	return err
}

func SendCV2MessageWithFilesAndReturn(s *discordgo.Session, channelID string, components []map[string]interface{}, files []*discordgo.File) (*discordgo.Message, error) {
	if s == nil || channelID == "" || len(components) == 0 {
		return nil, nil
	}
	if len(files) == 0 {
		return SendCV2MessageAndReturn(s, channelID, components, nil)
	}

	msg := &discordgo.MessageSend{
		Flags: CV2FlagsComponentsV2,
		Components: []discordgo.MessageComponent{
			rawContainerComponent{
				Components: components,
			},
		},
		Files: files,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	}

	return s.ChannelMessageSendComplex(channelID, msg)
}

type RawComponent map[string]interface{}

func (r RawComponent) Type() discordgo.ComponentType {
	if t, ok := r["type"].(int); ok {
		return discordgo.ComponentType(t)
	}
	if t, ok := r["type"].(float64); ok {
		return discordgo.ComponentType(t)
	}
	return 0
}

func (r RawComponent) MarshalJSON() ([]byte, error) {
	return json.Marshal((map[string]interface{})(r))
}
