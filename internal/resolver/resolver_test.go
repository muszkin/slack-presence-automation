package resolver_test

import (
	"testing"
	"time"

	"github.com/muszkin/slack-presence-automation/internal/resolver"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("time.Parse %q: %v", s, err)
	}
	return ts
}

func TestResolveReturnsDefaultWhenNothingMatches(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T08:00:00+02:00") // Friday 08:00
	got := resolver.Resolve(now, nil, nil, nil, nil)
	if got != resolver.IdleState {
		t.Errorf("Resolve() = %+v, want IdleState %+v", got, resolver.IdleState)
	}
}

func TestResolveOverrideBeatsCalendarBeatsRule(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T14:30:00+02:00") // Friday 14:30

	override := resolver.Override{
		ExpiresAt:   now.Add(time.Hour),
		StatusEmoji: ":focus:",
		StatusText:  "focus",
		Presence:    resolver.PresenceDND,
	}
	event := resolver.Event{
		Title: "Design sync",
		Start: now.Add(-10 * time.Minute),
		End:   now.Add(20 * time.Minute),
	}
	rule := resolver.Rule{
		ID:          1,
		DaysOfWeek:  1 << 4, // Friday only
		StartMinute: 14 * 60,
		EndMinute:   16 * 60,
		StatusEmoji: ":rule:",
		StatusText:  "office hours",
		Presence:    resolver.PresenceAuto,
	}

	got := resolver.Resolve(now, []resolver.Override{override}, []resolver.Event{event}, []resolver.Rule{rule}, nil)
	if got.Source != resolver.SourceOverride {
		t.Fatalf("Source = %q, want override", got.Source)
	}
	if got.StatusEmoji != ":focus:" {
		t.Errorf("emoji = %q, want :focus:", got.StatusEmoji)
	}
}

func TestResolveIgnoresExpiredOverride(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T10:00:00+02:00")

	override := resolver.Override{
		ExpiresAt: now.Add(-time.Second),
		Presence:  resolver.PresenceDND,
	}
	got := resolver.Resolve(now, []resolver.Override{override}, nil, nil, nil)
	if got.Source != resolver.SourceDefault {
		t.Errorf("expected default when only override is expired, got %q", got.Source)
	}
}

func TestResolvePrefersLatestExpiringOverride(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T10:00:00+02:00")

	shorter := resolver.Override{
		ExpiresAt:   now.Add(30 * time.Minute),
		StatusEmoji: ":short:",
		Presence:    resolver.PresenceAway,
	}
	longer := resolver.Override{
		ExpiresAt:   now.Add(2 * time.Hour),
		StatusEmoji: ":long:",
		Presence:    resolver.PresenceDND,
	}
	got := resolver.Resolve(now, []resolver.Override{shorter, longer}, nil, nil, nil)
	if got.StatusEmoji != ":long:" {
		t.Errorf("expected override with later ExpiresAt to win, got emoji=%q", got.StatusEmoji)
	}
}

func TestResolveCalendarEventUsesTitleAsStatusText(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T11:00:00+02:00")

	event := resolver.Event{
		Title: "Budget review",
		Start: now.Add(-time.Minute),
		End:   now.Add(59 * time.Minute),
	}
	got := resolver.Resolve(now, nil, []resolver.Event{event}, nil, nil)
	if got.Source != resolver.SourceCalendar {
		t.Fatalf("Source = %q, want calendar", got.Source)
	}
	if got.StatusText != "Budget review" {
		t.Errorf("StatusText = %q, want event title", got.StatusText)
	}
	if got.Presence != resolver.PresenceDND {
		t.Errorf("Presence = %q, want dnd default for meetings", got.Presence)
	}
}

func TestResolveEventEndIsExclusive(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T10:00:00+02:00")

	event := resolver.Event{
		Title: "Ending exactly now",
		Start: now.Add(-time.Hour),
		End:   now,
	}
	got := resolver.Resolve(now, nil, []resolver.Event{event}, nil, nil)
	if got.Source != resolver.SourceDefault {
		t.Errorf("event ending at exactly now should not match, got source=%q", got.Source)
	}
}

func TestResolveMeetingPatternMatchesByPriorityAndSubstring(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T11:00:00+02:00")

	event := resolver.Event{
		Title: "Lunch with Marta",
		Start: now.Add(-time.Minute),
		End:   now.Add(59 * time.Minute),
	}

	patterns := []resolver.MeetingPattern{
		{ID: 1, TitlePattern: "sync", Presence: resolver.PresenceDND, Priority: 1},
		{ID: 2, TitlePattern: "lunch", StatusEmoji: ":hamburger:", Presence: resolver.PresenceAway, Priority: 5},
	}
	got := resolver.Resolve(now, nil, []resolver.Event{event}, nil, patterns)
	if got.Source != resolver.SourceCalendar {
		t.Fatalf("Source = %q, want calendar", got.Source)
	}
	if got.Presence != resolver.PresenceAway {
		t.Errorf("Presence = %q, want away", got.Presence)
	}
	if got.StatusEmoji != ":hamburger:" {
		t.Errorf("Emoji = %q, want :hamburger:", got.StatusEmoji)
	}
	if got.StatusText != "Lunch with Marta" {
		t.Errorf("StatusText = %q, want event title (because pattern has empty text)", got.StatusText)
	}
}

func TestResolveMeetingPatternExplicitTextOverridesEventTitle(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T11:00:00+02:00")

	event := resolver.Event{
		Title: "1:1 with bob",
		Start: now.Add(-time.Minute),
		End:   now.Add(30 * time.Minute),
	}

	patterns := []resolver.MeetingPattern{
		{ID: 1, TitlePattern: "1:1", StatusEmoji: ":speech_balloon:", StatusText: "one on one", Presence: resolver.PresenceDND},
	}
	got := resolver.Resolve(now, nil, []resolver.Event{event}, nil, patterns)
	if got.StatusText != "one on one" {
		t.Errorf("StatusText = %q, want configured pattern text", got.StatusText)
	}
}

func TestResolveMeetingPatternEmptyPatternIsCatchAll(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T11:00:00+02:00")

	event := resolver.Event{
		Title: "Anything goes",
		Start: now.Add(-time.Minute),
		End:   now.Add(30 * time.Minute),
	}

	patterns := []resolver.MeetingPattern{
		{ID: 1, TitlePattern: "lunch", Presence: resolver.PresenceAway, Priority: 10},
		{ID: 2, TitlePattern: "", StatusEmoji: ":calendar:", StatusText: "In a meeting", Presence: resolver.PresenceDND, Priority: -1000},
	}
	got := resolver.Resolve(now, nil, []resolver.Event{event}, nil, patterns)
	if got.Source != resolver.SourceCalendar {
		t.Fatalf("Source = %q", got.Source)
	}
	if got.StatusEmoji != ":calendar:" {
		t.Errorf("Emoji = %q, want :calendar: (default catch-all)", got.StatusEmoji)
	}
	if got.StatusText != "In a meeting" {
		t.Errorf("StatusText = %q, want configured default text", got.StatusText)
	}
}

func TestResolveMeetingPatternHigherPriorityBeatsCatchAll(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T11:00:00+02:00")

	event := resolver.Event{
		Title: "Lunch with Marta",
		Start: now.Add(-time.Minute),
		End:   now.Add(30 * time.Minute),
	}

	patterns := []resolver.MeetingPattern{
		{ID: 1, TitlePattern: "", StatusEmoji: ":calendar:", Presence: resolver.PresenceDND, Priority: -1000},
		{ID: 2, TitlePattern: "lunch", StatusEmoji: ":hamburger:", Presence: resolver.PresenceAway, Priority: 0},
	}
	got := resolver.Resolve(now, nil, []resolver.Event{event}, nil, patterns)
	if got.StatusEmoji != ":hamburger:" {
		t.Errorf("Emoji = %q, want :hamburger: (specific pattern beats catch-all)", got.StatusEmoji)
	}
	if got.Presence != resolver.PresenceAway {
		t.Errorf("Presence = %q, want away", got.Presence)
	}
}

func TestResolveMeetingPatternFallsBackToDefault(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T11:00:00+02:00")

	event := resolver.Event{
		Title: "Project kickoff",
		Start: now.Add(-time.Minute),
		End:   now.Add(59 * time.Minute),
	}
	patterns := []resolver.MeetingPattern{
		{ID: 1, TitlePattern: "lunch", Presence: resolver.PresenceAway},
	}
	got := resolver.Resolve(now, nil, []resolver.Event{event}, nil, patterns)
	if got.Presence != resolver.PresenceDND {
		t.Errorf("Presence = %q, want default dnd", got.Presence)
	}
	if got.StatusText != "Project kickoff" {
		t.Errorf("StatusText = %q, want event title", got.StatusText)
	}
}

func TestResolveMeetingPatternMatchIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T11:00:00+02:00")

	event := resolver.Event{
		Title: "LUNCH with Marta",
		Start: now.Add(-time.Minute),
		End:   now.Add(59 * time.Minute),
	}
	patterns := []resolver.MeetingPattern{
		{ID: 1, TitlePattern: "lunch", Presence: resolver.PresenceAway},
	}
	got := resolver.Resolve(now, nil, []resolver.Event{event}, nil, patterns)
	if got.Presence != resolver.PresenceAway {
		t.Errorf("Presence = %q, want away (pattern matching must be case-insensitive)", got.Presence)
	}
}

func TestResolveRuleMatchesOnWeekdayAndTimeWindow(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T09:30:00+02:00")

	rule := resolver.Rule{
		ID:          1,
		DaysOfWeek:  0b00011111, // Mon-Fri
		StartMinute: 9 * 60,
		EndMinute:   10 * 60,
		StatusEmoji: ":coffee:",
		StatusText:  "morning",
		Presence:    resolver.PresenceAuto,
	}
	got := resolver.Resolve(now, nil, nil, []resolver.Rule{rule}, nil)
	if got.Source != resolver.SourceSchedule {
		t.Fatalf("Source = %q, want schedule", got.Source)
	}
	if got.StatusText != "morning" {
		t.Errorf("StatusText = %q", got.StatusText)
	}
}

func TestResolveRuleSkippedOutsideDayOrWindow(t *testing.T) {
	t.Parallel()

	rule := resolver.Rule{
		ID:          1,
		DaysOfWeek:  0b00011111, // Mon-Fri
		StartMinute: 9 * 60,
		EndMinute:   10 * 60,
		StatusEmoji: ":coffee:",
		Presence:    resolver.PresenceAuto,
	}

	weekend := mustParse(t, "2026-04-18T09:30:00+02:00") // Saturday
	if got := resolver.Resolve(weekend, nil, nil, []resolver.Rule{rule}, nil); got.Source != resolver.SourceDefault {
		t.Errorf("weekend should fall through, got source=%q", got.Source)
	}

	outsideWindow := mustParse(t, "2026-04-17T08:59:00+02:00") // Friday 08:59
	if got := resolver.Resolve(outsideWindow, nil, nil, []resolver.Rule{rule}, nil); got.Source != resolver.SourceDefault {
		t.Errorf("before window should fall through, got source=%q", got.Source)
	}

	endBoundary := mustParse(t, "2026-04-17T10:00:00+02:00") // Friday 10:00 (end exclusive)
	if got := resolver.Resolve(endBoundary, nil, nil, []resolver.Rule{rule}, nil); got.Source != resolver.SourceDefault {
		t.Errorf("end boundary should be exclusive, got source=%q", got.Source)
	}
}

func TestResolveRulePriorityBreaksTies(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T14:00:00+02:00")

	lowPriority := resolver.Rule{
		ID:          1,
		DaysOfWeek:  0b00011111,
		StartMinute: 13 * 60,
		EndMinute:   15 * 60,
		StatusEmoji: ":low:",
		Presence:    resolver.PresenceAuto,
		Priority:    1,
	}
	highPriority := resolver.Rule{
		ID:          2,
		DaysOfWeek:  0b00011111,
		StartMinute: 13 * 60,
		EndMinute:   15 * 60,
		StatusEmoji: ":high:",
		Presence:    resolver.PresenceDND,
		Priority:    10,
	}
	got := resolver.Resolve(now, nil, nil, []resolver.Rule{lowPriority, highPriority}, nil)
	if got.StatusEmoji != ":high:" {
		t.Errorf("expected higher-priority rule to win, got emoji=%q", got.StatusEmoji)
	}
}

func TestResolveRuleLowerIDWinsTiesWhenPriorityEqual(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T14:00:00+02:00")

	first := resolver.Rule{
		ID:          1,
		DaysOfWeek:  0b00011111,
		StartMinute: 13 * 60,
		EndMinute:   15 * 60,
		StatusEmoji: ":first:",
		Presence:    resolver.PresenceAuto,
		Priority:    5,
	}
	second := resolver.Rule{
		ID:          2,
		DaysOfWeek:  0b00011111,
		StartMinute: 13 * 60,
		EndMinute:   15 * 60,
		StatusEmoji: ":second:",
		Presence:    resolver.PresenceDND,
		Priority:    5,
	}
	got := resolver.Resolve(now, nil, nil, []resolver.Rule{second, first}, nil)
	if got.StatusEmoji != ":first:" {
		t.Errorf("expected lower-ID rule to win tie, got emoji=%q", got.StatusEmoji)
	}
}

func TestResolveIsDeterministic(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T09:30:00+02:00")
	rule := resolver.Rule{
		ID:          1,
		DaysOfWeek:  0b00011111,
		StartMinute: 9 * 60,
		EndMinute:   10 * 60,
		StatusEmoji: ":coffee:",
		Presence:    resolver.PresenceAuto,
	}

	first := resolver.Resolve(now, nil, nil, []resolver.Rule{rule}, nil)
	second := resolver.Resolve(now, nil, nil, []resolver.Rule{rule}, nil)
	if first != second {
		t.Errorf("expected deterministic result, got %+v vs %+v", first, second)
	}
}

func TestResolveDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	now := mustParse(t, "2026-04-17T14:00:00+02:00")
	rules := []resolver.Rule{
		{ID: 2, DaysOfWeek: 0b00011111, StartMinute: 13 * 60, EndMinute: 15 * 60, Priority: 1},
		{ID: 1, DaysOfWeek: 0b00011111, StartMinute: 13 * 60, EndMinute: 15 * 60, Priority: 10},
	}
	snapshot := append([]resolver.Rule(nil), rules...)

	_ = resolver.Resolve(now, nil, nil, rules, nil)

	for i := range rules {
		if rules[i] != snapshot[i] {
			t.Errorf("Resolve mutated rules[%d]: got %+v, want %+v", i, rules[i], snapshot[i])
		}
	}
}
