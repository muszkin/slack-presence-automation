// Package applier reconciles the desired state produced by the resolver with
// the last state actually applied to Slack, pushing changes only when they
// differ. This is the single place that consults the Slack write APIs and
// the applied_state cache in SQLite.
package applier

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/muszkin/slack-presence-automation/internal/resolver"
	slackpkg "github.com/muszkin/slack-presence-automation/internal/slack"
	"github.com/muszkin/slack-presence-automation/internal/storage"
)

// SlackApplier is the narrow interface the applier uses to push status to
// Slack. In production this is satisfied by *slackpkg.StatusClient; in tests
// a fake implementation lets us assert call counts without network calls.
type SlackApplier interface {
	Apply(ctx context.Context, status slackpkg.Status) error
}

// StateStore captures the two query methods the applier needs from the
// storage.Store — kept as an interface so tests can inject a fake.
type StateStore interface {
	GetAppliedState(ctx context.Context) (storage.GetAppliedStateRow, error)
	SetAppliedState(ctx context.Context, params storage.SetAppliedStateParams) error
}

// Applier pushes resolver.DesiredState to Slack and records what was applied.
type Applier struct {
	slack  SlackApplier
	store  StateStore
	logger *slog.Logger
}

// New builds an Applier wired to a concrete Slack client and storage Store.
func New(slack SlackApplier, store StateStore, logger *slog.Logger) *Applier {
	return &Applier{slack: slack, store: store, logger: logger}
}

// Apply pushes desired to Slack if (and only if) it differs from the last
// state recorded in applied_state. A successful Slack push is followed by an
// applied_state update in the same context; a failed push leaves the cache
// untouched so the next tick retries.
func (a *Applier) Apply(ctx context.Context, desired resolver.DesiredState) error {
	current, err := a.store.GetAppliedState(ctx)
	if err != nil {
		return fmt.Errorf("load applied_state: %w", err)
	}
	if statesEqual(current, desired) {
		a.logger.DebugContext(ctx, "desired state unchanged, skipping Slack push",
			slog.String("source", string(desired.Source)),
			slog.String("presence", string(desired.Presence)))
		return nil
	}

	a.logger.InfoContext(ctx, "applying desired state",
		slog.String("source", string(desired.Source)),
		slog.String("presence", string(desired.Presence)),
		slog.String("emoji", desired.StatusEmoji),
		slog.String("text", desired.StatusText))

	if err := a.slack.Apply(ctx, toSlackStatus(desired)); err != nil {
		return fmt.Errorf("push status to slack: %w", err)
	}

	params := storage.SetAppliedStateParams{
		StatusEmoji: desired.StatusEmoji,
		StatusText:  desired.StatusText,
		Presence:    string(desired.Presence),
		Source:      string(desired.Source),
	}
	if err := a.store.SetAppliedState(ctx, params); err != nil {
		return fmt.Errorf("persist applied_state: %w", err)
	}
	return nil
}

// statesEqual compares the current SQLite row with a DesiredState on every
// field the applier writes. Source changes alone also count as a change so
// the row reflects the latest resolving layer for observability.
func statesEqual(current storage.GetAppliedStateRow, desired resolver.DesiredState) bool {
	return current.StatusEmoji == desired.StatusEmoji &&
		current.StatusText == desired.StatusText &&
		current.Presence == string(desired.Presence) &&
		current.Source == string(desired.Source)
}

func toSlackStatus(desired resolver.DesiredState) slackpkg.Status {
	return slackpkg.Status{
		Emoji:    desired.StatusEmoji,
		Text:     desired.StatusText,
		Presence: slackpkg.Presence(desired.Presence),
	}
}
