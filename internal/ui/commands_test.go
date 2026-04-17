package ui_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	slackpkg "github.com/muszkin/slack-presence-automation/internal/slack"
	"github.com/muszkin/slack-presence-automation/internal/storage"
	"github.com/muszkin/slack-presence-automation/internal/ui"
)

type fakeStore struct {
	inserted  []storage.InsertOverrideParams
	cleared   int64
	active    []storage.Override
	applied   storage.GetAppliedStateRow
	insertErr error
	clearErr  error
	listErr   error
	stateErr  error

	rules          []storage.ScheduleRule
	ruleInserted   []storage.InsertScheduleRuleParams
	ruleDeleteIDs  []int64
	ruleDeleteRows int64
	ruleListErr    error
	ruleInsertErr  error
	ruleDeleteErr  error

	patterns          []storage.MeetingPattern
	patternInserted   []storage.InsertMeetingPatternParams
	patternDeleteIDs  []int64
	patternDeleteRows int64
	patternListErr    error
	patternInsertErr  error
	patternDeleteErr  error
}

func (f *fakeStore) InsertOverride(_ context.Context, p storage.InsertOverrideParams) (storage.Override, error) {
	f.inserted = append(f.inserted, p)
	return storage.Override{ID: int64(len(f.inserted))}, f.insertErr
}

func (f *fakeStore) ClearOverrides(_ context.Context) (int64, error) {
	return f.cleared, f.clearErr
}

func (f *fakeStore) ListActiveOverrides(_ context.Context, _ int64) ([]storage.Override, error) {
	return f.active, f.listErr
}

func (f *fakeStore) GetAppliedState(_ context.Context) (storage.GetAppliedStateRow, error) {
	return f.applied, f.stateErr
}

func (f *fakeStore) ListScheduleRules(_ context.Context) ([]storage.ScheduleRule, error) {
	return f.rules, f.ruleListErr
}

func (f *fakeStore) InsertScheduleRule(_ context.Context, p storage.InsertScheduleRuleParams) (storage.ScheduleRule, error) {
	f.ruleInserted = append(f.ruleInserted, p)
	return storage.ScheduleRule{ID: int64(len(f.ruleInserted))}, f.ruleInsertErr
}

func (f *fakeStore) DeleteScheduleRule(_ context.Context, id int64) (int64, error) {
	f.ruleDeleteIDs = append(f.ruleDeleteIDs, id)
	return f.ruleDeleteRows, f.ruleDeleteErr
}

func (f *fakeStore) InsertMeetingPattern(_ context.Context, p storage.InsertMeetingPatternParams) (storage.MeetingPattern, error) {
	f.patternInserted = append(f.patternInserted, p)
	return storage.MeetingPattern{ID: int64(len(f.patternInserted))}, f.patternInsertErr
}

func (f *fakeStore) ListMeetingPatterns(_ context.Context) ([]storage.MeetingPattern, error) {
	return f.patterns, f.patternListErr
}

func (f *fakeStore) DeleteMeetingPattern(_ context.Context, id int64) (int64, error) {
	f.patternDeleteIDs = append(f.patternDeleteIDs, id)
	return f.patternDeleteRows, f.patternDeleteErr
}

const testOwnerID = "U_OWNER"

func newCommands(t *testing.T, store ui.Store) (*ui.Commands, chan struct{}) {
	t.Helper()
	trigger := make(chan struct{}, 1)
	cmd := ui.NewCommands(store, nil, testOwnerID, trigger, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return cmd, trigger
}

func TestHandleRejectsSlashCommandFromNonOwner(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, trigger := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: "U_OTHER", Text: "focus 1h"})
	if !strings.Contains(resp.Text, "private to its owner") {
		t.Errorf("response = %q, want privacy notice", resp.Text)
	}
	if len(store.inserted) != 0 {
		t.Errorf("non-owner must not be able to insert overrides, got %d", len(store.inserted))
	}
	select {
	case <-trigger:
		t.Error("trigger must not fire for non-owner slash command")
	default:
	}
}

func TestHandleHelpAndEmptyReturnUsageOrStatus(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		applied: storage.GetAppliedStateRow{Presence: "auto", Source: "default"},
	}
	cmd, _ := newCommands(t, store)

	if resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "help"}); !strings.Contains(resp.Text, "Usage:") {
		t.Errorf("help response = %q", resp.Text)
	}

	if resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: ""}); !strings.Contains(resp.Text, "Applied:") {
		t.Errorf("empty subcommand should show status, got %q", resp.Text)
	}
}

func TestHandleFocusInsertsOverrideAndFiresTrigger(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, trigger := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "focus 45m"})
	if !strings.Contains(resp.Text, "Override active") {
		t.Errorf("response = %q", resp.Text)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("inserted count = %d, want 1", len(store.inserted))
	}
	inserted := store.inserted[0]
	if inserted.Presence != "dnd" {
		t.Errorf("presence = %q, want dnd", inserted.Presence)
	}
	if inserted.StatusEmoji != ":brain:" {
		t.Errorf("emoji = %q", inserted.StatusEmoji)
	}
	expected := time.Now().Add(45 * time.Minute).Unix()
	if diff := inserted.ExpiresAt - expected; diff > 2 || diff < -2 {
		t.Errorf("ExpiresAt drift = %d seconds", diff)
	}

	select {
	case <-trigger:
	default:
		t.Error("expected trigger channel to be fired")
	}
}

func TestHandleAwayStoresEmptyEmojiAndText(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "away 2h"})
	if !strings.Contains(resp.Text, "Override active") {
		t.Errorf("response = %q", resp.Text)
	}
	if len(store.inserted) != 1 {
		t.Fatalf("inserted count = %d", len(store.inserted))
	}
	inserted := store.inserted[0]
	if inserted.Presence != "away" {
		t.Errorf("presence = %q, want away", inserted.Presence)
	}
	if inserted.StatusEmoji != "" {
		t.Errorf("emoji = %q, want empty (away should not set emoji)", inserted.StatusEmoji)
	}
	if inserted.StatusText != "" {
		t.Errorf("text = %q, want empty (away should not set text)", inserted.StatusText)
	}
}

func TestHandleFocusRejectsInvalidDuration(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, trigger := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "focus forever"})
	if !strings.Contains(resp.Text, "not valid") {
		t.Errorf("response = %q", resp.Text)
	}
	if len(store.inserted) != 0 {
		t.Error("no override should be inserted on invalid duration")
	}
	select {
	case <-trigger:
		t.Error("trigger should not fire on failure")
	default:
	}
}

func TestHandleFocusRejectsDurationBelowMinimum(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "focus 30s"})
	if !strings.Contains(resp.Text, "at least 1m") {
		t.Errorf("response = %q", resp.Text)
	}
}

func TestHandleFocusRejectsDurationAboveMaximum(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "focus 48h"})
	if !strings.Contains(resp.Text, "at most 24h") {
		t.Errorf("response = %q", resp.Text)
	}
}

func TestHandleClearFiresTrigger(t *testing.T) {
	t.Parallel()

	store := &fakeStore{cleared: 3}
	cmd, trigger := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "clear"})
	if !strings.Contains(resp.Text, "Cleared 3") {
		t.Errorf("response = %q", resp.Text)
	}
	select {
	case <-trigger:
	default:
		t.Error("trigger should fire after clear")
	}
}

func TestHandleUnknownSubcommandShowsUsage(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "whatever"})
	if !strings.Contains(resp.Text, "Unknown subcommand") || !strings.Contains(resp.Text, "Usage:") {
		t.Errorf("response = %q", resp.Text)
	}
}

func TestHandlePatternAddInsertsAndFiresTrigger(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, trigger := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "pattern add away :hamburger: Lunch break"})
	if !strings.Contains(resp.Text, "Added pattern") {
		t.Errorf("response = %q", resp.Text)
	}
	if len(store.patternInserted) != 1 {
		t.Fatalf("pattern inserts = %d, want 1", len(store.patternInserted))
	}
	got := store.patternInserted[0]
	if got.Presence != "away" || got.StatusEmoji != ":hamburger:" || got.TitlePattern != "Lunch break" {
		t.Errorf("insert = %+v", got)
	}
	select {
	case <-trigger:
	default:
		t.Error("trigger should fire after pattern add")
	}
}

func TestHandlePatternAddAcceptsDashAsEmptyEmoji(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, _ := newCommands(t, store)

	_ = cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "pattern add away - Lunch"})
	if len(store.patternInserted) != 1 {
		t.Fatalf("pattern inserts = %d, want 1", len(store.patternInserted))
	}
	if store.patternInserted[0].StatusEmoji != "" {
		t.Errorf("emoji = %q, want empty (dash)", store.patternInserted[0].StatusEmoji)
	}
}

func TestHandlePatternAddRejectsBadPresence(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "pattern add busy :dart: Focus"})
	if !strings.Contains(resp.Text, "Invalid presence") {
		t.Errorf("response = %q", resp.Text)
	}
	if len(store.patternInserted) != 0 {
		t.Error("no pattern should be inserted on bad presence")
	}
}

func TestHandlePatternAddRequiresThreeArgs(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "pattern add away"})
	if !strings.Contains(resp.Text, "Usage:") {
		t.Errorf("response = %q", resp.Text)
	}
}

func TestHandlePatternListShowsPatterns(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		patterns: []storage.MeetingPattern{
			{ID: 1, TitlePattern: "Lunch", StatusEmoji: ":hamburger:", Presence: "away", Priority: 5},
			{ID: 2, TitlePattern: "1:1", Presence: "dnd"},
		},
	}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "pattern list"})
	if !strings.Contains(resp.Text, "Lunch") || !strings.Contains(resp.Text, "1:1") {
		t.Errorf("list response missing patterns: %q", resp.Text)
	}
}

func TestHandlePatternDeleteReportsWhenNotFound(t *testing.T) {
	t.Parallel()

	store := &fakeStore{patternDeleteRows: 0}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "pattern delete 42"})
	if !strings.Contains(resp.Text, "No pattern with id=42") {
		t.Errorf("response = %q", resp.Text)
	}
}

func TestHandlePatternDeleteRejectsNonNumericID(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	cmd, _ := newCommands(t, store)

	resp := cmd.Handle(t.Context(), slackpkg.SlashCommand{UserID: testOwnerID, Text: "pattern delete abc"})
	if !strings.Contains(resp.Text, "not a valid pattern id") {
		t.Errorf("response = %q", resp.Text)
	}
}
