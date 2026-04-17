// Package resolver contains the pure-function state resolver that maps
// the current time plus persisted overrides, calendar events, and schedule
// rules into the single DesiredState the applier should push to Slack.
//
// The resolver is intentionally free of I/O: given identical inputs it
// always returns identical outputs, which is what makes it exhaustively
// testable with table-driven and property-based tests.
package resolver

import (
	"sort"
	"strings"
	"time"
)

// Presence is a Slack presence mode expressible by this service.
type Presence string

// Presence modes the resolver can return. The applier maps:
//   - Auto      → Slack setPresence(auto)  + end snooze
//   - Available → Slack setPresence(auto)  + end snooze (same API as Auto
//     but a distinct semantic choice: "actively signal ready to receive";
//     Slack's own idle detection still controls the green/away dot on the
//     device side, which no API can override).
//   - Away      → Slack setPresence(away)  + set snooze
//   - DND       → Slack setPresence(auto)  + set snooze
const (
	PresenceAuto      Presence = "auto"
	PresenceAvailable Presence = "available"
	PresenceAway      Presence = "away"
	PresenceDND       Presence = "dnd"
)

// Source tags which layer of the priority hierarchy produced DesiredState,
// primarily for logging and observability.
type Source string

// Sources enumerate the four priority layers the resolver walks.
const (
	SourceOverride Source = "override"
	SourceCalendar Source = "calendar"
	SourceSchedule Source = "schedule"
	SourceDefault  Source = "default"
)

// Override is a manual ad-hoc override valid until ExpiresAt.
type Override struct {
	ExpiresAt   time.Time
	StatusEmoji string
	StatusText  string
	Presence    Presence
}

// Event is a calendar event active between [Start, End).
type Event struct {
	Title string
	Start time.Time
	End   time.Time
}

// Rule is a recurring schedule rule matching day-of-week + time-of-day window.
type Rule struct {
	ID          int64
	DaysOfWeek  uint8 // bit 0 = Monday, ..., bit 6 = Sunday
	StartMinute int   // minutes since local midnight, 0..1439
	EndMinute   int   // minutes since local midnight, 1..1440; must be > StartMinute
	StatusEmoji string
	StatusText  string
	Presence    Presence
	Priority    int
}

// MeetingPattern customises how a calendar event is mapped to DesiredState
// based on a case-insensitive substring match against the event title.
// The first pattern (highest Priority, then lowest ID) whose TitlePattern
// is contained in the event title wins; the event falls back to
// DefaultMeetingState if no pattern matches.
type MeetingPattern struct {
	ID           int64
	TitlePattern string
	StatusEmoji  string
	StatusText   string // empty means "use the event title as status text"
	Presence     Presence
	Priority     int
}

// DesiredState is the single concrete state the applier should push.
type DesiredState struct {
	StatusEmoji string
	StatusText  string
	Presence    Presence
	Source      Source
}

// DefaultMeetingState is the desired state for any active calendar event
// in the MVP — a future iteration can map event title patterns to richer
// states, but until that exists every meeting flips presence to DND.
// Emoji is :spiral_calendar_pad: (a built-in Slack emoji that maps to a
// Unicode codepoint) so it works in every workspace without customization.
var DefaultMeetingState = DesiredState{
	StatusEmoji: ":spiral_calendar_pad:",
	StatusText:  "In a meeting",
	Presence:    PresenceDND,
	Source:      SourceCalendar,
}

// IdleState is the fall-through when no override, event, or rule matches.
var IdleState = DesiredState{
	StatusEmoji: "",
	StatusText:  "",
	Presence:    PresenceAuto,
	Source:      SourceDefault,
}

// Resolve walks the priority hierarchy (override > calendar > schedule > default)
// and returns the first matching layer as DesiredState. Active calendar events
// are mapped through meetingPatterns (first match by priority DESC, id ASC)
// with DefaultMeetingState as the fallback.
func Resolve(now time.Time, overrides []Override, events []Event, rules []Rule, meetingPatterns []MeetingPattern) DesiredState {
	if override, ok := activeOverride(now, overrides); ok {
		return DesiredState{
			StatusEmoji: override.StatusEmoji,
			StatusText:  override.StatusText,
			Presence:    override.Presence,
			Source:      SourceOverride,
		}
	}
	if event, ok := activeEvent(now, events); ok {
		return eventToDesired(event, meetingPatterns)
	}
	if rule, ok := matchingRule(now, rules); ok {
		return DesiredState{
			StatusEmoji: rule.StatusEmoji,
			StatusText:  rule.StatusText,
			Presence:    rule.Presence,
			Source:      SourceSchedule,
		}
	}
	return IdleState
}

func eventToDesired(event Event, patterns []MeetingPattern) DesiredState {
	if pattern, ok := matchingPattern(event.Title, patterns); ok {
		text := pattern.StatusText
		if text == "" {
			text = event.Title
		}
		return DesiredState{
			StatusEmoji: pattern.StatusEmoji,
			StatusText:  text,
			Presence:    pattern.Presence,
			Source:      SourceCalendar,
		}
	}
	state := DefaultMeetingState
	if event.Title != "" {
		state.StatusText = event.Title
	}
	return state
}

func matchingPattern(title string, patterns []MeetingPattern) (MeetingPattern, bool) {
	if len(patterns) == 0 {
		return MeetingPattern{}, false
	}
	sorted := append([]MeetingPattern(nil), patterns...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		return sorted[i].ID < sorted[j].ID
	})
	titleLower := strings.ToLower(title)
	for _, p := range sorted {
		// An empty TitlePattern is a catch-all — strings.Contains(anything, "")
		// is always true, so the default pattern only wins when every
		// higher-priority non-empty pattern has already been ruled out.
		if strings.Contains(titleLower, strings.ToLower(p.TitlePattern)) {
			return p, true
		}
	}
	return MeetingPattern{}, false
}

func activeOverride(now time.Time, overrides []Override) (Override, bool) {
	latest := Override{}
	found := false
	for _, o := range overrides {
		if !now.Before(o.ExpiresAt) {
			continue
		}
		if !found || o.ExpiresAt.After(latest.ExpiresAt) {
			latest = o
			found = true
		}
	}
	return latest, found
}

func activeEvent(now time.Time, events []Event) (Event, bool) {
	for _, e := range events {
		if !now.Before(e.Start) && now.Before(e.End) {
			return e, true
		}
	}
	return Event{}, false
}

func matchingRule(now time.Time, rules []Rule) (Rule, bool) {
	sorted := append([]Rule(nil), rules...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		return sorted[i].ID < sorted[j].ID
	})

	dayBit := dayBitFor(now)
	minute := now.Hour()*60 + now.Minute()

	for _, r := range sorted {
		if r.DaysOfWeek&dayBit == 0 {
			continue
		}
		if minute < r.StartMinute || minute >= r.EndMinute {
			continue
		}
		return r, true
	}
	return Rule{}, false
}

// dayBitFor returns the bitmap bit for the weekday of now: bit 0 = Monday
// through bit 6 = Sunday, matching how rules encode their allowed days.
func dayBitFor(now time.Time) uint8 {
	weekday := now.Weekday()
	switch weekday {
	case time.Monday:
		return 1 << 0
	case time.Tuesday:
		return 1 << 1
	case time.Wednesday:
		return 1 << 2
	case time.Thursday:
		return 1 << 3
	case time.Friday:
		return 1 << 4
	case time.Saturday:
		return 1 << 5
	case time.Sunday:
		return 1 << 6
	}
	return 0
}
