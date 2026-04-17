package calendar

import (
	"testing"
	"time"

	calendarv3 "google.golang.org/api/calendar/v3"
)

func TestParseEventTimeReturnsRFC3339DateTime(t *testing.T) {
	t.Parallel()

	got, err := parseEventTime(&calendarv3.EventDateTime{DateTime: "2026-04-17T10:30:00+02:00"})
	if err != nil {
		t.Fatalf("parseEventTime: %v", err)
	}
	want, _ := time.Parse(time.RFC3339, "2026-04-17T10:30:00+02:00")
	if !got.Equal(want) {
		t.Errorf("parseEventTime = %v, want %v", got, want)
	}
}

func TestParseEventTimeRejectsAllDayEvent(t *testing.T) {
	t.Parallel()

	if _, err := parseEventTime(&calendarv3.EventDateTime{Date: "2026-04-17"}); err == nil {
		t.Error("expected error for all-day event, got nil")
	}
}

func TestParseEventTimeRejectsMalformedDateTime(t *testing.T) {
	t.Parallel()

	if _, err := parseEventTime(&calendarv3.EventDateTime{DateTime: "yesterday"}); err == nil {
		t.Error("expected error for malformed DateTime, got nil")
	}
}
