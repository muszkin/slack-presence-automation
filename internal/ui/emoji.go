package ui

import (
	"sort"
	"strings"

	emojilib "github.com/kyokomi/emoji/v2"
	slackgo "github.com/slack-go/slack"
)

// emojiSuggestionLimit caps the number of options returned to Slack. Slack's
// external_select tolerates up to 100; 25 keeps the dropdown readable.
const emojiSuggestionLimit = 25

type emojiEntry struct {
	shortcode string // ":brain:" including the surrounding colons
	character string // 🧠
}

// allEmojis is a flat, alphabetically sorted snapshot of the shortcode map
// from kyokomi/emoji. Loaded once at package init so per-suggestion filtering
// only walks a slice.
var allEmojis = loadEmojis()

func loadEmojis() []emojiEntry {
	codes := emojilib.CodeMap()
	entries := make([]emojiEntry, 0, len(codes))
	for code, char := range codes {
		entries = append(entries, emojiEntry{shortcode: code, character: char})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].shortcode < entries[j].shortcode
	})
	return entries
}

// filterEmojis returns up to emojiSuggestionLimit options matching the query.
// Matching is case-insensitive; prefix matches rank above substring matches.
// An empty or ":" query returns the first slice of the alphabetically sorted
// list so users see something useful as soon as they focus the picker.
func filterEmojis(query string) []slackgo.OptionBlockObject {
	q := normaliseQuery(query)

	var matches []emojiEntry
	if q == "" {
		if len(allEmojis) <= emojiSuggestionLimit {
			matches = allEmojis
		} else {
			matches = allEmojis[:emojiSuggestionLimit]
		}
	} else {
		matches = rankedMatches(q)
	}

	options := make([]slackgo.OptionBlockObject, 0, len(matches))
	for _, e := range matches {
		label := e.character + " " + e.shortcode
		options = append(options, slackgo.OptionBlockObject{
			Value: e.shortcode,
			Text:  slackgo.NewTextBlockObject(slackgo.PlainTextType, label, false, false),
		})
	}
	return options
}

func normaliseQuery(raw string) string {
	q := strings.TrimSpace(strings.ToLower(raw))
	q = strings.TrimPrefix(q, ":")
	q = strings.TrimSuffix(q, ":")
	return q
}

func rankedMatches(q string) []emojiEntry {
	prefix := make([]emojiEntry, 0, emojiSuggestionLimit)
	substring := make([]emojiEntry, 0, emojiSuggestionLimit)
	for _, e := range allEmojis {
		name := strings.Trim(e.shortcode, ":")
		switch {
		case strings.HasPrefix(name, q):
			prefix = append(prefix, e)
		case strings.Contains(name, q):
			substring = append(substring, e)
		}
		if len(prefix) >= emojiSuggestionLimit {
			break
		}
	}

	remaining := emojiSuggestionLimit - len(prefix)
	if remaining > 0 && len(substring) > remaining {
		substring = substring[:remaining]
	} else if remaining <= 0 {
		substring = nil
	}
	return append(prefix, substring...)
}
