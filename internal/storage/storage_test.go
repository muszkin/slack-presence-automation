package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/muszkin/slack-presence-automation/internal/storage"
)

func newStore(t *testing.T) *storage.Store {
	t.Helper()
	ctx := t.Context()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close: %v", err)
		}
	})
	return store
}

func TestOpenRunsMigrationsAndSeedsAppliedState(t *testing.T) {
	t.Parallel()

	store := newStore(t)

	rules, err := store.ListScheduleRules(t.Context())
	if err != nil {
		t.Fatalf("ListScheduleRules on fresh DB: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected empty rules, got %d", len(rules))
	}

	applied, err := store.GetAppliedState(t.Context())
	if err != nil {
		t.Fatalf("GetAppliedState on fresh DB: %v", err)
	}
	if applied.Source != "default" {
		t.Errorf("seeded applied_state.source = %q, want %q", applied.Source, "default")
	}
	if applied.Presence != "auto" {
		t.Errorf("seeded applied_state.presence = %q, want %q", applied.Presence, "auto")
	}
}

func TestInsertAndListScheduleRulesOrdersByPriorityDesc(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	ctx := t.Context()

	low, err := store.InsertScheduleRule(ctx, storage.InsertScheduleRuleParams{
		DaysOfWeek:  0b0011111, // Mon-Fri
		StartMinute: 9 * 60,
		EndMinute:   10 * 60,
		StatusEmoji: ":coffee:",
		StatusText:  "morning",
		Presence:    "auto",
		Priority:    1,
	})
	if err != nil {
		t.Fatalf("InsertScheduleRule low: %v", err)
	}

	high, err := store.InsertScheduleRule(ctx, storage.InsertScheduleRuleParams{
		DaysOfWeek:  0b0011111,
		StartMinute: 14 * 60,
		EndMinute:   16 * 60,
		StatusEmoji: ":focus:",
		StatusText:  "deep work",
		Presence:    "dnd",
		Priority:    10,
	})
	if err != nil {
		t.Fatalf("InsertScheduleRule high: %v", err)
	}

	rules, err := store.ListScheduleRules(ctx)
	if err != nil {
		t.Fatalf("ListScheduleRules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].ID != high.ID {
		t.Errorf("expected highest-priority rule first, got id=%d (want %d)", rules[0].ID, high.ID)
	}
	if rules[1].ID != low.ID {
		t.Errorf("expected lower-priority rule second, got id=%d (want %d)", rules[1].ID, low.ID)
	}
}

func TestListActiveOverridesExcludesExpired(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	ctx := t.Context()

	const cutoff = int64(1_000_000)

	if _, err := store.InsertOverride(ctx, storage.InsertOverrideParams{
		ExpiresAt:   cutoff + 60,
		StatusEmoji: ":active:",
		StatusText:  "still active",
		Presence:    "dnd",
	}); err != nil {
		t.Fatalf("InsertOverride active: %v", err)
	}
	if _, err := store.InsertOverride(ctx, storage.InsertOverrideParams{
		ExpiresAt:   cutoff - 60,
		StatusEmoji: ":expired:",
		StatusText:  "should be filtered",
		Presence:    "away",
	}); err != nil {
		t.Fatalf("InsertOverride expired: %v", err)
	}

	active, err := store.ListActiveOverrides(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListActiveOverrides: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active override, got %d", len(active))
	}
	if active[0].StatusEmoji != ":active:" {
		t.Errorf("active override emoji = %q, want %q", active[0].StatusEmoji, ":active:")
	}
}

func TestDeleteExpiredOverridesRemovesOnlyExpired(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	ctx := t.Context()

	const cutoff = int64(2_000_000)

	if _, err := store.InsertOverride(ctx, storage.InsertOverrideParams{
		ExpiresAt: cutoff + 100,
		Presence:  "auto",
	}); err != nil {
		t.Fatalf("InsertOverride active: %v", err)
	}
	if _, err := store.InsertOverride(ctx, storage.InsertOverrideParams{
		ExpiresAt: cutoff - 100,
		Presence:  "away",
	}); err != nil {
		t.Fatalf("InsertOverride expired: %v", err)
	}

	removed, err := store.DeleteExpiredOverrides(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteExpiredOverrides: %v", err)
	}
	if removed != 1 {
		t.Errorf("DeleteExpiredOverrides returned %d, want 1", removed)
	}

	remaining, err := store.ListActiveOverrides(ctx, cutoff)
	if err != nil {
		t.Fatalf("ListActiveOverrides after cleanup: %v", err)
	}
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining active override, got %d", len(remaining))
	}
}

func TestSetAppliedStateUpdatesSingleRow(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	ctx := t.Context()

	if err := store.SetAppliedState(ctx, storage.SetAppliedStateParams{
		StatusEmoji: ":meeting:",
		StatusText:  "in a meeting",
		Presence:    "dnd",
		Source:      "calendar",
	}); err != nil {
		t.Fatalf("SetAppliedState: %v", err)
	}

	state, err := store.GetAppliedState(ctx)
	if err != nil {
		t.Fatalf("GetAppliedState: %v", err)
	}
	if state.StatusEmoji != ":meeting:" {
		t.Errorf("status_emoji = %q, want %q", state.StatusEmoji, ":meeting:")
	}
	if state.Source != "calendar" {
		t.Errorf("source = %q, want %q", state.Source, "calendar")
	}
	if state.Presence != "dnd" {
		t.Errorf("presence = %q, want %q", state.Presence, "dnd")
	}
}

func TestScheduleRuleCheckConstraintsRejectInvalidRows(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	ctx := t.Context()

	invalid := []struct {
		name   string
		params storage.InsertScheduleRuleParams
	}{
		{
			name: "end_minute_not_greater_than_start_minute",
			params: storage.InsertScheduleRuleParams{
				DaysOfWeek: 1, StartMinute: 600, EndMinute: 600,
				Presence: "auto",
			},
		},
		{
			name: "days_of_week_zero",
			params: storage.InsertScheduleRuleParams{
				DaysOfWeek: 0, StartMinute: 0, EndMinute: 60,
				Presence: "auto",
			},
		},
		{
			name: "unknown_presence",
			params: storage.InsertScheduleRuleParams{
				DaysOfWeek: 1, StartMinute: 0, EndMinute: 60,
				Presence: "busy",
			},
		},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := store.InsertScheduleRule(ctx, tc.params); err == nil {
				t.Errorf("expected CHECK constraint violation for %s, got nil", tc.name)
			}
		})
	}
}

func TestAppliedStateSingleRowConstraint(t *testing.T) {
	t.Parallel()

	store := newStore(t)

	_, err := store.GetAppliedState(t.Context())
	if err != nil {
		t.Fatalf("expected seeded applied_state row to exist: %v", err)
	}
}
