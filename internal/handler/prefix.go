package handler

import "strings"

func parseCommandContent(content, prefix, botID string) (string, bool) {
	if prefix != "" && strings.HasPrefix(content, prefix) {
		after := content[len(prefix):]
		if len(after) > 0 && (after[0] == ' ' || after[0] == '\t' || after[0] == '\n' || after[0] == '\r') {
			return "", false
		}
		return after, true
	}

	for _, mention := range []string{"<@" + botID + ">", "<@!" + botID + ">"} {
		if strings.HasPrefix(content, mention) {
			return strings.TrimPrefix(content, mention), true
		}
	}

	return "", false
}
