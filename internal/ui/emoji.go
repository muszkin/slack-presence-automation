package ui

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	emojilib "github.com/kyokomi/emoji/v2"
	slackgo "github.com/slack-go/slack"
)

// emojiSuggestionLimit caps the number of options returned to Slack. Slack's
// external_select tolerates up to 100; 25 keeps the dropdown readable.
const emojiSuggestionLimit = 25

// DefaultEmojiCatalogTTL is how long the workspace custom-emoji list is
// cached before the next block_suggestion call triggers a refresh. Slack's
// emoji.list is cheap but not free; 10 minutes is more than short enough to
// pick up a freshly uploaded emoji without hammering the API.
const DefaultEmojiCatalogTTL = 10 * time.Minute

type emojiEntry struct {
	shortcode string // ":brain:" including the surrounding colons
	character string // Unicode char for built-ins; empty for custom workspace emoji
}

// builtinEmojis is the shortcode snapshot from kyokomi/emoji. Initialised
// once at package load; filtering walks a slice, not a map.
var builtinEmojis = loadBuiltinEmojis()

func loadBuiltinEmojis() []emojiEntry {
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

// EmojiLister is the thin interface the Catalog needs from the Slack API.
// ViewsClient satisfies it, but tests can substitute a fake to avoid
// network calls and assert refresh behaviour.
type EmojiLister interface {
	ListEmoji(ctx context.Context) (map[string]string, error)
}

// Catalog serves emoji suggestions combining the built-in Unicode shortcodes
// with the workspace's custom emoji fetched from Slack's emoji.list. Custom
// emoji are refreshed lazily on a TTL; built-ins never change at runtime.
type Catalog struct {
	builtin []emojiEntry

	mu        sync.Mutex
	custom    []emojiEntry
	lastFetch time.Time

	lister EmojiLister
	ttl    time.Duration
	logger *slog.Logger
}

// NewCatalog builds a Catalog. When lister is nil (e.g. in slash-only tests)
// the catalog degrades to built-ins only without errors.
func NewCatalog(lister EmojiLister, ttl time.Duration, logger *slog.Logger) *Catalog {
	if ttl <= 0 {
		ttl = DefaultEmojiCatalogTTL
	}
	return &Catalog{
		builtin: builtinEmojis,
		lister:  lister,
		ttl:     ttl,
		logger:  logger,
	}
}

// Filter returns up to emojiSuggestionLimit options matching the query.
// Prefix matches rank above substring matches; empty query returns the
// first slice of the alphabetically sorted catalogue so the dropdown is
// never blank when a user first focuses the picker.
func (c *Catalog) Filter(ctx context.Context, query string) []slackgo.OptionBlockObject {
	entries := c.allEntries(ctx)
	q := normaliseQuery(query)

	matches := entries
	if q != "" {
		matches = rankedMatches(entries, q)
	} else if len(entries) > emojiSuggestionLimit {
		matches = entries[:emojiSuggestionLimit]
	}

	options := make([]slackgo.OptionBlockObject, 0, len(matches))
	for _, e := range matches {
		options = append(options, slackgo.OptionBlockObject{
			Value: e.shortcode,
			Text:  slackgo.NewTextBlockObject(slackgo.PlainTextType, entryLabel(e), false, false),
		})
	}
	return options
}

// allEntries returns the combined built-in + custom catalogue, refreshing
// the custom cache lazily when the TTL has elapsed. Refresh failures are
// logged and the call falls back to whatever was previously cached — a
// single transient emoji.list error must not brick the picker.
func (c *Catalog) allEntries(ctx context.Context) []emojiEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lister != nil && (c.lastFetch.IsZero() || time.Since(c.lastFetch) > c.ttl) {
		c.refreshLocked(ctx)
	}
	combined := make([]emojiEntry, 0, len(c.builtin)+len(c.custom))
	combined = append(combined, c.custom...) // custom first so workspace brand wins ties
	combined = append(combined, c.builtin...)
	return combined
}

func (c *Catalog) refreshLocked(ctx context.Context) {
	raw, err := c.lister.ListEmoji(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "emoji.list failed, keeping previous cache",
			slog.Any("err", err),
			slog.Int("cached", len(c.custom)))
		c.lastFetch = time.Now() // back off so we don't hammer a failing API
		return
	}
	entries := make([]emojiEntry, 0, len(raw))
	for name := range raw {
		entries = append(entries, emojiEntry{
			shortcode: ":" + name + ":",
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].shortcode < entries[j].shortcode
	})
	c.custom = entries
	c.lastFetch = time.Now()
	c.logger.DebugContext(ctx, "emoji catalog refreshed",
		slog.Int("custom", len(entries)),
		slog.Int("builtin", len(c.builtin)))
}

func normaliseQuery(raw string) string {
	q := strings.TrimSpace(strings.ToLower(raw))
	q = strings.TrimPrefix(q, ":")
	q = strings.TrimSuffix(q, ":")
	return q
}

func rankedMatches(entries []emojiEntry, q string) []emojiEntry {
	prefix := make([]emojiEntry, 0, emojiSuggestionLimit)
	substring := make([]emojiEntry, 0, emojiSuggestionLimit)
	for _, e := range entries {
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
	switch {
	case remaining <= 0:
		substring = nil
	case len(substring) > remaining:
		substring = substring[:remaining]
	}
	return append(prefix, substring...)
}

func entryLabel(e emojiEntry) string {
	if e.character == "" {
		return e.shortcode
	}
	return e.character + " " + e.shortcode
}
