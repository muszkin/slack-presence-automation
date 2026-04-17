package slack

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	slackgo "github.com/slack-go/slack"
)

type fakeUserAPI struct {
	customText, customEmoji string
	customExpiration        int64
	setPresenceCalls        []string
	setSnoozeMinutes        []int
	endSnoozeCalls          int

	// injectable errors
	customStatusErr error
	presenceErr     error
	snoozeErr       error
	endSnoozeErr    error
}

func (f *fakeUserAPI) SetUserCustomStatusContext(_ context.Context, text, emoji string, exp int64) error {
	f.customText = text
	f.customEmoji = emoji
	f.customExpiration = exp
	return f.customStatusErr
}

func (f *fakeUserAPI) SetUserPresenceContext(_ context.Context, presence string) error {
	f.setPresenceCalls = append(f.setPresenceCalls, presence)
	return f.presenceErr
}

func (f *fakeUserAPI) SetSnoozeContext(_ context.Context, minutes int) (*slackgo.DNDStatus, error) {
	f.setSnoozeMinutes = append(f.setSnoozeMinutes, minutes)
	return &slackgo.DNDStatus{}, f.snoozeErr
}

func (f *fakeUserAPI) EndSnoozeContext(_ context.Context) (*slackgo.DNDStatus, error) {
	f.endSnoozeCalls++
	return &slackgo.DNDStatus{}, f.endSnoozeErr
}

func newTestClient(api *fakeUserAPI) *StatusClient {
	return newStatusClientWithAPI(api, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestApplySetsCustomStatusAndPresenceAutoEndsSnooze(t *testing.T) {
	t.Parallel()

	fake := &fakeUserAPI{}
	client := newTestClient(fake)

	err := client.Apply(t.Context(), Status{Emoji: ":wave:", Text: "hi", Presence: PresenceAuto})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if fake.customEmoji != ":wave:" || fake.customText != "hi" {
		t.Errorf("custom status = (%q,%q)", fake.customEmoji, fake.customText)
	}
	if got := fake.setPresenceCalls; len(got) != 1 || got[0] != "auto" {
		t.Errorf("presence calls = %v, want [auto]", got)
	}
	if fake.endSnoozeCalls != 1 {
		t.Errorf("EndSnooze calls = %d, want 1", fake.endSnoozeCalls)
	}
	if len(fake.setSnoozeMinutes) != 0 {
		t.Errorf("SetSnooze should not be called for auto, got %v", fake.setSnoozeMinutes)
	}
}

func TestApplyAvailableBehavesLikeAutoAndEndsSnooze(t *testing.T) {
	t.Parallel()

	fake := &fakeUserAPI{}
	client := newTestClient(fake)

	err := client.Apply(t.Context(), Status{Presence: PresenceAvailable})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := fake.setPresenceCalls; len(got) != 1 || got[0] != "auto" {
		t.Errorf("presence calls = %v, want [auto] for available", got)
	}
	if fake.endSnoozeCalls != 1 {
		t.Errorf("EndSnooze calls = %d, want 1 (available must never leave DND active)", fake.endSnoozeCalls)
	}
	if len(fake.setSnoozeMinutes) != 0 {
		t.Errorf("SetSnooze must not be called for available, got %v", fake.setSnoozeMinutes)
	}
}

func TestApplyDNDSetsSnoozeAndKeepsPresenceAuto(t *testing.T) {
	t.Parallel()

	fake := &fakeUserAPI{}
	client := newTestClient(fake)

	err := client.Apply(t.Context(), Status{Emoji: ":focus:", Text: "deep work", Presence: PresenceDND})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(fake.setSnoozeMinutes) != 1 || fake.setSnoozeMinutes[0] != snoozeMinutes {
		t.Errorf("SetSnooze calls = %v, want [%d]", fake.setSnoozeMinutes, snoozeMinutes)
	}
	if got := fake.setPresenceCalls; len(got) != 1 || got[0] != "auto" {
		t.Errorf("presence calls = %v, want [auto] while DND", got)
	}
	if fake.endSnoozeCalls != 0 {
		t.Errorf("EndSnooze should not be called when entering DND")
	}
}

func TestApplyAwaySetsPresenceAwayAndSnooze(t *testing.T) {
	t.Parallel()

	fake := &fakeUserAPI{}
	client := newTestClient(fake)

	err := client.Apply(t.Context(), Status{Emoji: ":palm_tree:", Text: "away", Presence: PresenceAway})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got := fake.setPresenceCalls; len(got) != 1 || got[0] != "away" {
		t.Errorf("presence calls = %v, want [away]", got)
	}
	if len(fake.setSnoozeMinutes) != 1 || fake.setSnoozeMinutes[0] != snoozeMinutes {
		t.Errorf("SetSnooze calls = %v, want [%d] (away mutes notifications)", fake.setSnoozeMinutes, snoozeMinutes)
	}
	if fake.endSnoozeCalls != 0 {
		t.Errorf("EndSnooze should not be called on away, got %d", fake.endSnoozeCalls)
	}
}

func TestApplyIgnoresBenignSnoozeNotActive(t *testing.T) {
	t.Parallel()

	fake := &fakeUserAPI{
		endSnoozeErr: slackgo.SlackErrorResponse{Err: "snooze_not_active"},
	}
	client := newTestClient(fake)

	err := client.Apply(t.Context(), Status{Presence: PresenceAuto})
	if err != nil {
		t.Fatalf("Apply should swallow snooze_not_active, got: %v", err)
	}
}

func TestApplyPropagatesUnexpectedEndSnoozeError(t *testing.T) {
	t.Parallel()

	fake := &fakeUserAPI{
		endSnoozeErr: slackgo.SlackErrorResponse{Err: "server_error"},
	}
	client := newTestClient(fake)

	err := client.Apply(t.Context(), Status{Presence: PresenceAuto})
	if err == nil {
		t.Fatal("expected server_error to surface, got nil")
	}
	var resp slackgo.SlackErrorResponse
	if !errors.As(err, &resp) || resp.Err != "server_error" {
		t.Errorf("error should wrap server_error SlackErrorResponse, got: %v", err)
	}
}

func TestApplyRejectsUnknownPresence(t *testing.T) {
	t.Parallel()

	fake := &fakeUserAPI{}
	client := newTestClient(fake)

	err := client.Apply(t.Context(), Status{Presence: Presence("invisible")})
	if err == nil {
		t.Fatal("expected error for unknown presence")
	}
}
