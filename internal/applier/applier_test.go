package applier_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/muszkin/slack-presence-automation/internal/applier"
	"github.com/muszkin/slack-presence-automation/internal/resolver"
	slackpkg "github.com/muszkin/slack-presence-automation/internal/slack"
	"github.com/muszkin/slack-presence-automation/internal/storage"
)

type fakeSlack struct {
	calls    []slackpkg.Status
	applyErr error
}

func (f *fakeSlack) Apply(_ context.Context, s slackpkg.Status) error {
	f.calls = append(f.calls, s)
	return f.applyErr
}

type fakeStore struct {
	current   storage.GetAppliedStateRow
	setParams []storage.SetAppliedStateParams
	getErr    error
	setErr    error
	getCalls  int
	setCalls  int
}

func (f *fakeStore) GetAppliedState(_ context.Context) (storage.GetAppliedStateRow, error) {
	f.getCalls++
	return f.current, f.getErr
}

func (f *fakeStore) SetAppliedState(_ context.Context, params storage.SetAppliedStateParams) error {
	f.setCalls++
	f.setParams = append(f.setParams, params)
	if f.setErr != nil {
		return f.setErr
	}
	f.current = storage.GetAppliedStateRow{
		StatusEmoji: params.StatusEmoji,
		StatusText:  params.StatusText,
		Presence:    params.Presence,
		Source:      params.Source,
	}
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestApplySkipsSlackWhenStateUnchanged(t *testing.T) {
	t.Parallel()

	slack := &fakeSlack{}
	store := &fakeStore{current: storage.GetAppliedStateRow{
		StatusEmoji: ":focus:", StatusText: "deep work", Presence: "dnd", Source: "override",
	}}
	a := applier.New(slack, store, discardLogger())

	err := a.Apply(t.Context(), resolver.DesiredState{
		StatusEmoji: ":focus:", StatusText: "deep work",
		Presence: resolver.PresenceDND, Source: resolver.SourceOverride,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(slack.calls) != 0 {
		t.Errorf("expected no Slack call, got %d", len(slack.calls))
	}
	if store.setCalls != 0 {
		t.Errorf("expected no applied_state write, got %d", store.setCalls)
	}
}

func TestApplyPushesAndPersistsOnStateChange(t *testing.T) {
	t.Parallel()

	slack := &fakeSlack{}
	store := &fakeStore{current: storage.GetAppliedStateRow{
		StatusEmoji: "", StatusText: "", Presence: "auto", Source: "default",
	}}
	a := applier.New(slack, store, discardLogger())

	desired := resolver.DesiredState{
		StatusEmoji: ":meeting:", StatusText: "Design sync",
		Presence: resolver.PresenceDND, Source: resolver.SourceCalendar,
	}
	if err := a.Apply(t.Context(), desired); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(slack.calls) != 1 {
		t.Fatalf("expected 1 Slack call, got %d", len(slack.calls))
	}
	if slack.calls[0].Emoji != ":meeting:" || slack.calls[0].Presence != slackpkg.PresenceDND {
		t.Errorf("Slack call = %+v", slack.calls[0])
	}
	if store.setCalls != 1 {
		t.Fatalf("expected 1 applied_state write, got %d", store.setCalls)
	}
	if store.setParams[0].Source != "calendar" {
		t.Errorf("source persisted = %q, want calendar", store.setParams[0].Source)
	}
}

func TestApplyDoesNotPersistWhenSlackFails(t *testing.T) {
	t.Parallel()

	slackErr := errors.New("rate limited")
	slack := &fakeSlack{applyErr: slackErr}
	store := &fakeStore{current: storage.GetAppliedStateRow{Presence: "auto", Source: "default"}}
	a := applier.New(slack, store, discardLogger())

	err := a.Apply(t.Context(), resolver.DesiredState{
		StatusEmoji: ":focus:", Presence: resolver.PresenceDND, Source: resolver.SourceOverride,
	})
	if !errors.Is(err, slackErr) {
		t.Fatalf("expected wrapped rate-limited error, got: %v", err)
	}
	if store.setCalls != 0 {
		t.Errorf("applied_state must not be updated on Slack failure, got %d writes", store.setCalls)
	}
}

func TestApplyTreatsSourceChangeAsStateChange(t *testing.T) {
	t.Parallel()

	slack := &fakeSlack{}
	store := &fakeStore{current: storage.GetAppliedStateRow{
		StatusEmoji: ":focus:", StatusText: "deep work",
		Presence: "dnd", Source: "schedule",
	}}
	a := applier.New(slack, store, discardLogger())

	// Same status/presence but a different source layer (override now instead of schedule).
	err := a.Apply(t.Context(), resolver.DesiredState{
		StatusEmoji: ":focus:", StatusText: "deep work",
		Presence: resolver.PresenceDND, Source: resolver.SourceOverride,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(slack.calls) != 1 {
		t.Errorf("expected re-apply on source change, got %d Slack calls", len(slack.calls))
	}
}
