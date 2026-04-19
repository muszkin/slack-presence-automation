package ui_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	slackgo "github.com/slack-go/slack"

	"github.com/muszkin/slack-presence-automation/internal/storage"
	"github.com/muszkin/slack-presence-automation/internal/ui"
)

type fakeViews struct {
	published   []slackgo.HomeTabViewRequest
	publishedTo []string
	opened      []slackgo.ModalViewRequest
	openTrigs   []string
	publishErr  error
	openErr     error
}

func (f *fakeViews) PublishHomeView(_ context.Context, userID string, view slackgo.HomeTabViewRequest) error {
	f.publishedTo = append(f.publishedTo, userID)
	f.published = append(f.published, view)
	return f.publishErr
}

func (f *fakeViews) OpenModal(_ context.Context, triggerID string, view slackgo.ModalViewRequest) error {
	f.openTrigs = append(f.openTrigs, triggerID)
	f.opened = append(f.opened, view)
	return f.openErr
}

func newCommandsWithViews(t *testing.T, store ui.Store, views ui.ViewsClient) (*ui.Commands, chan struct{}) {
	t.Helper()
	trigger := make(chan struct{}, 1)
	cmd := ui.NewCommands(store, views, testOwnerID, trigger, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return cmd, trigger
}

func TestHandleHomeOpenedForNonOwnerPublishesPrivacyNotice(t *testing.T) {
	t.Parallel()

	store := &fakeStore{applied: storage.GetAppliedStateRow{Presence: "auto", Source: "default"}}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cmd.HandleHomeOpened(t.Context(), "U_STRANGER")

	if len(views.publishedTo) != 1 || views.publishedTo[0] != "U_STRANGER" {
		t.Errorf("publishedTo = %v, want [U_STRANGER]", views.publishedTo)
	}
	if n := len(views.published[0].Blocks.BlockSet); n != 1 {
		t.Errorf("privacy view has %d blocks, want 1", n)
	}
}

func TestHandleInteractionRejectsBlockActionFromNonOwner(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeBlockActions,
		User: slackgo.User{ID: "U_STRANGER"},
		ActionCallback: slackgo.ActionCallbacks{
			BlockActions: []*slackgo.BlockAction{
				{ActionID: ui.ActionDeleteRule, Value: "1"},
			},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if !resp.Empty() {
		t.Errorf("block actions from non-owner should silently no-op, got %+v", resp)
	}
	if len(store.ruleDeleteIDs) != 0 {
		t.Error("non-owner must not be able to delete rules")
	}
}

func TestHandleInteractionRejectsViewSubmissionFromNonOwnerWithError(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeViewSubmission,
		User: slackgo.User{ID: "U_STRANGER"},
		View: slackgo.View{
			CallbackID: ui.CallbackAddRule,
			State: &slackgo.ViewState{Values: map[string]map[string]slackgo.BlockAction{
				"days": {"days": {SelectedOptions: []slackgo.OptionBlockObject{{Value: "mon"}}}},
			}},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if resp.Empty() {
		t.Fatal("expected validation-error response so Slack keeps the modal open")
	}
	if len(store.ruleInserted) != 0 {
		t.Error("non-owner must not insert a rule")
	}
}

func TestHandleHomeOpenedPublishesCurrentState(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		applied:  storage.GetAppliedStateRow{Presence: "auto", Source: "default"},
		rules:    []storage.ScheduleRule{{ID: 1, DaysOfWeek: 0b00011111, StartMinute: 540, EndMinute: 600, Presence: "auto"}},
		patterns: []storage.MeetingPattern{{ID: 1, TitlePattern: "Lunch", Presence: "away"}},
	}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cmd.HandleHomeOpened(t.Context(), "U1")

	if len(views.publishedTo) != 1 || views.publishedTo[0] != "U1" {
		t.Errorf("publishedTo = %v, want [U1]", views.publishedTo)
	}
	if len(views.published) != 1 {
		t.Fatalf("published = %d, want 1", len(views.published))
	}
	if views.published[0].Type != slackgo.VTHomeTab {
		t.Errorf("view type = %q, want home", views.published[0].Type)
	}
}

func TestHandleHomeOpenedSkippedWhenViewsNil(t *testing.T) {
	t.Parallel()

	store := &fakeStore{applied: storage.GetAppliedStateRow{Presence: "auto", Source: "default"}}
	cmd, _ := newCommands(t, store) // views == nil

	// Should not panic or error.
	cmd.HandleHomeOpened(t.Context(), "U1")
}

func TestHandleInteractionAddRuleOpensModal(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type:      slackgo.InteractionTypeBlockActions,
		TriggerID: "trig-123",
		User:      slackgo.User{ID: testOwnerID},
		ActionCallback: slackgo.ActionCallbacks{
			BlockActions: []*slackgo.BlockAction{
				{ActionID: ui.ActionAddRule},
			},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)
	if !resp.Empty() {
		t.Errorf("expected empty response for block action, got %+v", resp)
	}
	if len(views.opened) != 1 {
		t.Fatalf("modal opens = %d, want 1", len(views.opened))
	}
	if views.opened[0].CallbackID != ui.CallbackAddRule {
		t.Errorf("callback_id = %q, want %q", views.opened[0].CallbackID, ui.CallbackAddRule)
	}
	if views.openTrigs[0] != "trig-123" {
		t.Errorf("trigger_id = %q", views.openTrigs[0])
	}
}

func TestHandleInteractionDeleteRuleDeletesAndRepublishes(t *testing.T) {
	t.Parallel()

	store := &fakeStore{
		applied:        storage.GetAppliedStateRow{Presence: "auto", Source: "default"},
		ruleDeleteRows: 1,
	}
	views := &fakeViews{}
	cmd, trigger := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeBlockActions,
		User: slackgo.User{ID: testOwnerID},
		ActionCallback: slackgo.ActionCallbacks{
			BlockActions: []*slackgo.BlockAction{
				{ActionID: ui.ActionDeleteRule, Value: "7"},
			},
		},
	}
	_ = cmd.HandleInteraction(t.Context(), cb)

	if len(store.ruleDeleteIDs) != 1 || store.ruleDeleteIDs[0] != 7 {
		t.Errorf("ruleDeleteIDs = %v, want [7]", store.ruleDeleteIDs)
	}
	if len(views.published) != 1 {
		t.Errorf("expected home re-publish, got %d", len(views.published))
	}
	select {
	case <-trigger:
	default:
		t.Error("expected trigger fired after delete")
	}
}

func TestHandleInteractionClearOverridesCallsStore(t *testing.T) {
	t.Parallel()

	store := &fakeStore{applied: storage.GetAppliedStateRow{Presence: "auto", Source: "default"}, cleared: 2}
	views := &fakeViews{}
	cmd, trigger := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeBlockActions,
		User: slackgo.User{ID: testOwnerID},
		ActionCallback: slackgo.ActionCallbacks{
			BlockActions: []*slackgo.BlockAction{
				{ActionID: ui.ActionClearOverrides},
			},
		},
	}
	_ = cmd.HandleInteraction(t.Context(), cb)

	if len(views.published) != 1 {
		t.Error("expected home re-publish after clear")
	}
	select {
	case <-trigger:
	default:
		t.Error("trigger should fire after clear")
	}
}

func TestHandleViewSubmissionAddRuleInsertsAndRepublishes(t *testing.T) {
	t.Parallel()

	store := &fakeStore{applied: storage.GetAppliedStateRow{Presence: "auto", Source: "default"}}
	views := &fakeViews{}
	cmd, trigger := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeViewSubmission,
		User: slackgo.User{ID: testOwnerID},
		View: slackgo.View{
			CallbackID: ui.CallbackAddRule,
			State: &slackgo.ViewState{Values: map[string]map[string]slackgo.BlockAction{
				"days":       {"days": {SelectedOptions: []slackgo.OptionBlockObject{{Value: "mon"}, {Value: "fri"}}}},
				"start_time": {"start_time": {Value: "09:00"}},
				"end_time":   {"end_time": {Value: "10:30"}},
				"presence":   {"presence": {SelectedOption: slackgo.OptionBlockObject{Value: "dnd"}}},
				"emoji":      {"emoji": {SelectedOption: slackgo.OptionBlockObject{Value: ":brain:"}}},
				"text":       {"text": {Value: "deep work"}},
				"priority":   {"priority": {Value: "5"}},
			}},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if !resp.Empty() {
		t.Fatalf("expected empty response (no errors), got %+v", resp)
	}
	if len(store.ruleInserted) != 1 {
		t.Fatalf("expected 1 rule inserted, got %d", len(store.ruleInserted))
	}
	inserted := store.ruleInserted[0]
	if inserted.DaysOfWeek != (1<<0 | 1<<4) {
		t.Errorf("DaysOfWeek = %b, want bit0|bit4 (mon+fri)", inserted.DaysOfWeek)
	}
	if inserted.StartMinute != 9*60 {
		t.Errorf("StartMinute = %d, want 540", inserted.StartMinute)
	}
	if inserted.EndMinute != 10*60+30 {
		t.Errorf("EndMinute = %d, want 630", inserted.EndMinute)
	}
	if inserted.Presence != "dnd" {
		t.Errorf("Presence = %q", inserted.Presence)
	}
	if inserted.Priority != 5 {
		t.Errorf("Priority = %d", inserted.Priority)
	}
	if len(views.published) != 1 {
		t.Errorf("expected home re-publish, got %d", len(views.published))
	}
	select {
	case <-trigger:
	default:
		t.Error("trigger should fire after rule insert")
	}
}

func TestHandleViewSubmissionAddRuleValidatesEndAfterStart(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeViewSubmission,
		User: slackgo.User{ID: testOwnerID},
		View: slackgo.View{
			CallbackID: ui.CallbackAddRule,
			State: &slackgo.ViewState{Values: map[string]map[string]slackgo.BlockAction{
				"days":       {"days": {SelectedOptions: []slackgo.OptionBlockObject{{Value: "mon"}}}},
				"start_time": {"start_time": {Value: "14:00"}},
				"end_time":   {"end_time": {Value: "14:00"}},
				"presence":   {"presence": {SelectedOption: slackgo.OptionBlockObject{Value: "auto"}}},
			}},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if resp.Empty() {
		t.Fatal("expected validation error, got empty response")
	}
	if _, ok := resp.Errors["end_time"]; !ok {
		t.Errorf("errors = %+v, want end_time entry", resp.Errors)
	}
	if len(store.ruleInserted) != 0 {
		t.Error("rule must not be inserted on validation failure")
	}
}

func TestHandleViewSubmissionAddRuleRejectsMalformedTime(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeViewSubmission,
		User: slackgo.User{ID: testOwnerID},
		View: slackgo.View{
			CallbackID: ui.CallbackAddRule,
			State: &slackgo.ViewState{Values: map[string]map[string]slackgo.BlockAction{
				"days":       {"days": {SelectedOptions: []slackgo.OptionBlockObject{{Value: "mon"}}}},
				"start_time": {"start_time": {Value: "9am"}},
				"end_time":   {"end_time": {Value: "17:00"}},
				"presence":   {"presence": {SelectedOption: slackgo.OptionBlockObject{Value: "auto"}}},
			}},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if _, ok := resp.Errors["start_time"]; !ok {
		t.Errorf("errors = %+v, want start_time entry for malformed 24h value", resp.Errors)
	}
	if len(store.ruleInserted) != 0 {
		t.Error("no insert on malformed time")
	}
}

func TestHandleViewSubmissionAddRuleRequiresDays(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeViewSubmission,
		User: slackgo.User{ID: testOwnerID},
		View: slackgo.View{
			CallbackID: ui.CallbackAddRule,
			State: &slackgo.ViewState{Values: map[string]map[string]slackgo.BlockAction{
				"days":       {"days": {SelectedOptions: nil}},
				"start_time": {"start_time": {Value: "09:00"}},
				"end_time":   {"end_time": {Value: "10:00"}},
				"presence":   {"presence": {SelectedOption: slackgo.OptionBlockObject{Value: "auto"}}},
			}},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if _, ok := resp.Errors["days"]; !ok {
		t.Errorf("errors = %+v, want days entry", resp.Errors)
	}
}

func TestHandleViewSubmissionAddPatternInserts(t *testing.T) {
	t.Parallel()

	store := &fakeStore{applied: storage.GetAppliedStateRow{Presence: "auto", Source: "default"}}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeViewSubmission,
		User: slackgo.User{ID: testOwnerID},
		View: slackgo.View{
			CallbackID: ui.CallbackAddPattern,
			State: &slackgo.ViewState{Values: map[string]map[string]slackgo.BlockAction{
				"title_pattern": {"title_pattern": {Value: "Lunch"}},
				"presence":      {"presence": {SelectedOption: slackgo.OptionBlockObject{Value: "away"}}},
				"emoji":         {"emoji": {SelectedOption: slackgo.OptionBlockObject{Value: ":hamburger:"}}},
			}},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if !resp.Empty() {
		t.Fatalf("expected empty response, got %+v", resp)
	}
	if len(store.patternInserted) != 1 {
		t.Fatalf("expected 1 pattern inserted, got %d", len(store.patternInserted))
	}
	if store.patternInserted[0].TitlePattern != "Lunch" {
		t.Errorf("pattern = %q", store.patternInserted[0].TitlePattern)
	}
	if store.patternInserted[0].StatusEmoji != ":hamburger:" {
		t.Errorf("emoji persisted = %q, want :hamburger: (via external_select)", store.patternInserted[0].StatusEmoji)
	}
}

func TestHandleBlockSuggestionReturnsFilteredEmojis(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type:     slackgo.InteractionTypeBlockSuggestion,
		User:     slackgo.User{ID: testOwnerID},
		ActionID: "emoji",
		Value:    "brain",
	}
	resp := cmd.HandleInteraction(t.Context(), cb)
	if len(resp.Options) == 0 {
		t.Fatal("expected at least one suggestion for 'brain'")
	}
	first := resp.Options[0].Value
	if first != ":brain:" {
		t.Errorf("top suggestion value = %q, want :brain:", first)
	}
}

func TestHandleBlockSuggestionUnknownActionIDReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type:     slackgo.InteractionTypeBlockSuggestion,
		User:     slackgo.User{ID: testOwnerID},
		ActionID: "unknown",
		Value:    "anything",
	}
	resp := cmd.HandleInteraction(t.Context(), cb)
	if len(resp.Options) != 0 {
		t.Errorf("unknown action should return zero options, got %d", len(resp.Options))
	}
}

func TestHandleViewSubmissionAddPatternRequiresText(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	views := &fakeViews{}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeViewSubmission,
		User: slackgo.User{ID: testOwnerID},
		View: slackgo.View{
			CallbackID: ui.CallbackAddPattern,
			State: &slackgo.ViewState{Values: map[string]map[string]slackgo.BlockAction{
				"title_pattern": {"title_pattern": {Value: "  "}},
				"presence":      {"presence": {SelectedOption: slackgo.OptionBlockObject{Value: "dnd"}}},
			}},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if _, ok := resp.Errors["title_pattern"]; !ok {
		t.Errorf("errors = %+v, want title_pattern entry", resp.Errors)
	}
	if len(store.patternInserted) != 0 {
		t.Error("pattern must not be inserted for blank title")
	}
}

func TestHandleViewSubmissionPublishErrorDoesNotCrash(t *testing.T) {
	t.Parallel()

	store := &fakeStore{applied: storage.GetAppliedStateRow{Presence: "auto", Source: "default"}}
	views := &fakeViews{publishErr: errors.New("slack down")}
	cmd, _ := newCommandsWithViews(t, store, views)

	cb := slackgo.InteractionCallback{
		Type: slackgo.InteractionTypeViewSubmission,
		User: slackgo.User{ID: testOwnerID},
		View: slackgo.View{
			CallbackID: ui.CallbackAddPattern,
			State: &slackgo.ViewState{Values: map[string]map[string]slackgo.BlockAction{
				"title_pattern": {"title_pattern": {Value: "Lunch"}},
				"presence":      {"presence": {SelectedOption: slackgo.OptionBlockObject{Value: "away"}}},
			}},
		},
	}
	resp := cmd.HandleInteraction(t.Context(), cb)

	if !resp.Empty() {
		t.Errorf("publish failure should not be surfaced as a modal error: %+v", resp)
	}
	if len(store.patternInserted) != 1 {
		t.Error("insert should still happen even if subsequent publish fails")
	}
}

func TestBuildHomeViewIncludesAddButtons(t *testing.T) {
	t.Parallel()

	view := ui.BuildHomeView(
		storage.GetAppliedStateRow{Presence: "auto", Source: "default"},
		nil, nil, nil,
	)

	// Serialize via slack-go's JSON tags to inspect action IDs without
	// copying the entire block tree by type.
	if view.Type != slackgo.VTHomeTab {
		t.Fatalf("view type = %q", view.Type)
	}
	seen := map[string]bool{}
	collect := func(blocks []slackgo.Block) {
		for _, b := range blocks {
			if sec, ok := b.(*slackgo.SectionBlock); ok && sec.Accessory != nil {
				if btn := sec.Accessory.ButtonElement; btn != nil {
					seen[btn.ActionID] = true
				}
			}
		}
	}
	collect(view.Blocks.BlockSet)

	for _, id := range []string{ui.ActionAddRule, ui.ActionAddPattern} {
		if !seen[id] {
			t.Errorf("home view missing %s button", id)
		}
	}
}

func TestBuildAddRuleModalHasRequiredBlocks(t *testing.T) {
	t.Parallel()

	modal := ui.BuildAddRuleModal()
	if modal.CallbackID != ui.CallbackAddRule {
		t.Errorf("callback_id = %q", modal.CallbackID)
	}
	wantBlockIDs := []string{"days", "start_time", "end_time", "presence", "emoji", "text", "priority"}
	present := map[string]bool{}
	for _, b := range modal.Blocks.BlockSet {
		if ib, ok := b.(*slackgo.InputBlock); ok {
			present[ib.BlockID] = true
		}
	}
	for _, want := range wantBlockIDs {
		if !present[want] {
			t.Errorf("modal missing input block %q", want)
		}
	}
}
