// Package ui owns user-facing Slack surfaces: slash command handlers today,
// App Home views and interactive modals in later iterations.
package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	slackgoviews "github.com/slack-go/slack"

	slackpkg "github.com/muszkin/slack-presence-automation/internal/slack"
	"github.com/muszkin/slack-presence-automation/internal/storage"
)

// Store is the storage subset needed by slash commands, App Home rendering,
// and interactive handlers.
type Store interface {
	InsertOverride(ctx context.Context, params storage.InsertOverrideParams) (storage.Override, error)
	ClearOverrides(ctx context.Context) (int64, error)
	ListActiveOverrides(ctx context.Context, expiresAt int64) ([]storage.Override, error)
	GetAppliedState(ctx context.Context) (storage.GetAppliedStateRow, error)

	ListScheduleRules(ctx context.Context) ([]storage.ScheduleRule, error)
	InsertScheduleRule(ctx context.Context, params storage.InsertScheduleRuleParams) (storage.ScheduleRule, error)
	DeleteScheduleRule(ctx context.Context, id int64) (int64, error)

	InsertMeetingPattern(ctx context.Context, params storage.InsertMeetingPatternParams) (storage.MeetingPattern, error)
	ListMeetingPatterns(ctx context.Context) ([]storage.MeetingPattern, error)
	DeleteMeetingPattern(ctx context.Context, id int64) (int64, error)
}

// ViewsClient is the narrow Slack views.* API surface the App Home and
// modals need: publishing the home tab, opening modals, and listing the
// workspace's custom emoji for the emoji picker.
type ViewsClient interface {
	PublishHomeView(ctx context.Context, userID string, view slackgoviews.HomeTabViewRequest) error
	OpenModal(ctx context.Context, triggerID string, view slackgoviews.ModalViewRequest) error
	ListEmoji(ctx context.Context) (map[string]string, error)
}

// Commands handles the `/presence` slash command, App Home events, and
// interactive callbacks. Each handler that mutates state fires the
// reconcile trigger so the tick loop picks up the change immediately.
//
// The service is single-user by design: the xoxp- user token drives exactly
// one Slack profile. To stop other workspace members from hijacking that
// profile through the slash command or App Home, every handler checks the
// incoming user ID against ownerUserID and rejects mismatches.
type Commands struct {
	store       Store
	views       ViewsClient
	trigger     chan<- struct{}
	ownerUserID string
	emojiCat    *Catalog
	logger      *slog.Logger
}

// NewCommands builds a Commands handler. ownerUserID is the Slack user ID
// that owns the installed xoxp- token and is therefore the only one allowed
// to interact with the service — everyone else gets an explanatory "private
// app" message (or silence for home events). trigger must be a buffered
// channel of size 1; the views argument may be nil when App Home / modals
// are disabled in tests.
func NewCommands(store Store, views ViewsClient, ownerUserID string, trigger chan<- struct{}, logger *slog.Logger) *Commands {
	var lister EmojiLister
	if views != nil {
		lister = views
	}
	return &Commands{
		store:       store,
		views:       views,
		trigger:     trigger,
		ownerUserID: ownerUserID,
		emojiCat:    NewCatalog(lister, DefaultEmojiCatalogTTL, logger),
		logger:      logger,
	}
}

// IsOwner reports whether the given Slack user ID is the single authorised
// operator for this service.
func (c *Commands) IsOwner(userID string) bool {
	return userID != "" && userID == c.ownerUserID
}

const privateAppText = "This presence app is private to its owner. If you need presence automation for yourself, deploy your own instance."

// Handle routes a slash command to the matching subcommand.
func (c *Commands) Handle(ctx context.Context, cmd slackpkg.SlashCommand) slackpkg.SlashResponse {
	if !c.IsOwner(cmd.UserID) {
		c.logger.WarnContext(ctx, "rejected slash command from non-owner",
			slog.String("user_id", cmd.UserID), slog.String("text", cmd.Text))
		return slackpkg.SlashResponse{Text: privateAppText}
	}

	fields := strings.Fields(cmd.Text)
	var sub string
	var args []string
	if len(fields) > 0 {
		sub = strings.ToLower(fields[0])
		args = fields[1:]
	}

	switch sub {
	case "", "status":
		return c.handleStatus(ctx)
	case "clear":
		return c.handleClear(ctx)
	case "focus":
		return c.handleOverride(ctx, args, ":brain:", "focus", slackpkg.PresenceDND)
	case "away":
		return c.handleOverride(ctx, args, "", "", slackpkg.PresenceAway)
	case "dnd":
		return c.handleOverride(ctx, args, ":no_bell:", "do not disturb", slackpkg.PresenceDND)
	case "available":
		return c.handleOverride(ctx, args, "", "", slackpkg.PresenceAvailable)
	case "pattern":
		return c.handlePattern(ctx, args)
	case "help":
		return slackpkg.SlashResponse{Text: usageText}
	default:
		return slackpkg.SlashResponse{Text: fmt.Sprintf("Unknown subcommand %q. %s", sub, usageText)}
	}
}

func (c *Commands) handlePattern(ctx context.Context, args []string) slackpkg.SlashResponse {
	if len(args) == 0 {
		return slackpkg.SlashResponse{Text: patternUsage}
	}
	switch strings.ToLower(args[0]) {
	case "list":
		return c.handlePatternList(ctx)
	case "add":
		return c.handlePatternAdd(ctx, args[1:])
	case "delete", "remove", "rm":
		return c.handlePatternDelete(ctx, args[1:])
	default:
		return slackpkg.SlashResponse{Text: patternUsage}
	}
}

func (c *Commands) handlePatternList(ctx context.Context) slackpkg.SlashResponse {
	patterns, err := c.store.ListMeetingPatterns(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "list meeting patterns", slog.Any("err", err))
		return slackpkg.SlashResponse{Text: "Failed to list patterns."}
	}
	if len(patterns) == 0 {
		return slackpkg.SlashResponse{Text: "No meeting patterns configured. Add one: `/presence pattern add <presence> <emoji> <title substring>`."}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Meeting patterns (%d):", len(patterns))
	for _, p := range patterns {
		fmt.Fprintf(&b, "\n  • id=%d priority=%d presence=%s emoji=%s match=%q",
			p.ID, p.Priority, p.Presence, displayOrDash(p.StatusEmoji), p.TitlePattern)
	}
	return slackpkg.SlashResponse{Text: b.String()}
}

func (c *Commands) handlePatternAdd(ctx context.Context, args []string) slackpkg.SlashResponse {
	// Format: <presence> <emoji> <title pattern...>
	// Emoji may be "-" to leave it empty.
	if len(args) < 3 {
		return slackpkg.SlashResponse{Text: patternAddUsage}
	}
	presence := strings.ToLower(args[0])
	if !isValidPresence(presence) {
		return slackpkg.SlashResponse{Text: fmt.Sprintf("Invalid presence %q. Use auto, away, or dnd.", presence)}
	}
	emoji := args[1]
	if emoji == "-" {
		emoji = ""
	}
	pattern := strings.TrimSpace(strings.Join(args[2:], " "))
	if pattern == "" {
		return slackpkg.SlashResponse{Text: "Title pattern must not be empty."}
	}

	inserted, err := c.store.InsertMeetingPattern(ctx, storage.InsertMeetingPatternParams{
		TitlePattern: pattern,
		StatusEmoji:  emoji,
		StatusText:   "",
		Presence:     presence,
		Priority:     0,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "insert meeting pattern", slog.Any("err", err))
		return slackpkg.SlashResponse{Text: "Failed to add pattern."}
	}
	c.fireTrigger()
	return slackpkg.SlashResponse{Text: fmt.Sprintf("Added pattern id=%d: presence=%s emoji=%s match=%q.",
		inserted.ID, presence, displayOrDash(emoji), pattern)}
}

func (c *Commands) handlePatternDelete(ctx context.Context, args []string) slackpkg.SlashResponse {
	if len(args) == 0 {
		return slackpkg.SlashResponse{Text: "Missing pattern id. Example: `/presence pattern delete 3`."}
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return slackpkg.SlashResponse{Text: fmt.Sprintf("%q is not a valid pattern id (positive integer).", args[0])}
	}
	removed, err := c.store.DeleteMeetingPattern(ctx, id)
	if err != nil {
		c.logger.WarnContext(ctx, "delete meeting pattern", slog.Any("err", err))
		return slackpkg.SlashResponse{Text: "Failed to delete pattern."}
	}
	if removed == 0 {
		return slackpkg.SlashResponse{Text: fmt.Sprintf("No pattern with id=%d.", id)}
	}
	c.fireTrigger()
	return slackpkg.SlashResponse{Text: fmt.Sprintf("Deleted pattern id=%d.", id)}
}

func isValidPresence(s string) bool {
	switch s {
	case string(slackpkg.PresenceAuto),
		string(slackpkg.PresenceAvailable),
		string(slackpkg.PresenceAway),
		string(slackpkg.PresenceDND):
		return true
	}
	return false
}

func (c *Commands) handleStatus(ctx context.Context) slackpkg.SlashResponse {
	state, err := c.store.GetAppliedState(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "load applied_state", slog.Any("err", err))
		return slackpkg.SlashResponse{Text: "Unable to read current state."}
	}
	active, err := c.store.ListActiveOverrides(ctx, time.Now().Unix())
	if err != nil {
		c.logger.WarnContext(ctx, "list overrides", slog.Any("err", err))
		return slackpkg.SlashResponse{Text: "Unable to read active overrides."}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Applied: %s %s (presence=%s, source=%s)\n",
		displayOrDash(state.StatusEmoji), displayOrDash(state.StatusText), state.Presence, state.Source)
	fmt.Fprintf(&b, "Active overrides: %d", len(active))
	if len(active) > 0 {
		for _, o := range active {
			fmt.Fprintf(&b, "\n  • presence=%s expires=%s emoji=%s text=%s",
				o.Presence,
				time.Unix(o.ExpiresAt, 0).Format(time.RFC3339),
				displayOrDash(o.StatusEmoji), displayOrDash(o.StatusText))
		}
	}
	return slackpkg.SlashResponse{Text: b.String()}
}

func (c *Commands) handleClear(ctx context.Context) slackpkg.SlashResponse {
	removed, err := c.store.ClearOverrides(ctx)
	if err != nil {
		c.logger.WarnContext(ctx, "clear overrides", slog.Any("err", err))
		return slackpkg.SlashResponse{Text: "Failed to clear overrides."}
	}
	c.fireTrigger()
	return slackpkg.SlashResponse{Text: fmt.Sprintf("Cleared %d override(s).", removed)}
}

func (c *Commands) handleOverride(ctx context.Context, args []string, emoji, text string, presence slackpkg.Presence) slackpkg.SlashResponse {
	if len(args) == 0 {
		return slackpkg.SlashResponse{Text: "Missing duration. Example: `/presence focus 2h`."}
	}
	duration, err := time.ParseDuration(args[0])
	if err != nil {
		return slackpkg.SlashResponse{Text: fmt.Sprintf("Duration %q is not valid. Use Go duration format like `30m`, `2h`.", args[0])}
	}
	if duration < time.Minute {
		return slackpkg.SlashResponse{Text: "Duration must be at least 1m."}
	}
	if duration > 24*time.Hour {
		return slackpkg.SlashResponse{Text: "Duration must be at most 24h."}
	}

	expiresAt := time.Now().Add(duration).Unix()
	if _, err := c.store.InsertOverride(ctx, storage.InsertOverrideParams{
		ExpiresAt:   expiresAt,
		StatusEmoji: emoji,
		StatusText:  text,
		Presence:    string(presence),
	}); err != nil {
		c.logger.WarnContext(ctx, "insert override", slog.Any("err", err))
		return slackpkg.SlashResponse{Text: "Failed to create override."}
	}
	c.fireTrigger()
	label := describeOverride(presence, emoji, text)
	return slackpkg.SlashResponse{Text: fmt.Sprintf("Override active for %s (%s).", duration, label)}
}

func describeOverride(presence slackpkg.Presence, emoji, text string) string {
	parts := []string{string(presence)}
	if emoji != "" {
		parts = append(parts, emoji)
	}
	if text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

func (c *Commands) fireTrigger() {
	select {
	case c.trigger <- struct{}{}:
	default:
	}
}

func displayOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

const (
	usageText = "Usage: `/presence [status | available <dur> | focus <dur> | away <dur> | dnd <dur> | clear | pattern <add|list|delete> | help]` — duration uses Go format (e.g. `30m`, `2h`)."

	patternUsage = "Usage: `/presence pattern <list | add <presence> <emoji|-> <title substring> | delete <id>>`."

	patternAddUsage = "Usage: `/presence pattern add <presence> <emoji|-> <title substring>`. Presence is auto/available/away/dnd. Emoji may be `-` for none. Example: `/presence pattern add away :hamburger: Lunch`."
)
