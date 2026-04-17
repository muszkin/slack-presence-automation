// Package calendar reads events from a single Google Calendar using a
// service-account JWT. It is deliberately narrow: the only production
// caller needs a list of events between two timestamps, sorted by start.
package calendar

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/oauth2/google"
	calendarv3 "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Event is the slim projection of a calendar event used downstream by the
// resolver — the richer google.golang.org/api/calendar/v3.Event type is kept
// inside this package.
type Event struct {
	Title string
	Start time.Time
	End   time.Time
}

// Client fetches events for a specific calendar.
type Client struct {
	svc        *calendarv3.Service
	calendarID string
}

// NewClient constructs a Client backed by a service-account credentials JSON.
// The credentials must grant the service account read access to calendarID
// (the operator shares their Google Calendar with the service account email).
func NewClient(ctx context.Context, credentialsJSON, calendarID string) (*Client, error) {
	if calendarID == "" {
		return nil, fmt.Errorf("calendar id is empty")
	}
	config, err := google.JWTConfigFromJSON([]byte(credentialsJSON), calendarv3.CalendarReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("parse service-account credentials: %w", err)
	}
	svc, err := calendarv3.NewService(ctx, option.WithHTTPClient(config.Client(ctx)))
	if err != nil {
		return nil, fmt.Errorf("build calendar service: %w", err)
	}
	return &Client{svc: svc, calendarID: calendarID}, nil
}

// FetchEvents returns events active in [from, to), expanded from recurring
// series, sorted by start time. All-day events are skipped because they do
// not carry time-of-day semantics the resolver can use.
func (c *Client) FetchEvents(ctx context.Context, from, to time.Time) ([]Event, error) {
	resp, err := c.svc.Events.List(c.calendarID).
		TimeMin(from.Format(time.RFC3339)).
		TimeMax(to.Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(250).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list events %s..%s: %w", from.Format(time.RFC3339), to.Format(time.RFC3339), err)
	}

	events := make([]Event, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item.Start == nil || item.End == nil {
			continue
		}
		start, err := parseEventTime(item.Start)
		if err != nil {
			continue
		}
		end, err := parseEventTime(item.End)
		if err != nil {
			continue
		}
		events = append(events, Event{
			Title: item.Summary,
			Start: start,
			End:   end,
		})
	}
	return events, nil
}

// parseEventTime prefers DateTime (has time-of-day) over Date (all-day event)
// and returns an error for the all-day case so the caller can skip it.
func parseEventTime(t *calendarv3.EventDateTime) (time.Time, error) {
	if t.DateTime != "" {
		ts, err := time.Parse(time.RFC3339, t.DateTime)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse %q: %w", t.DateTime, err)
		}
		return ts, nil
	}
	return time.Time{}, fmt.Errorf("all-day events are not supported")
}
