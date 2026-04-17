package ui

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	slackgo "github.com/slack-go/slack"

	slackpkg "github.com/muszkin/slack-presence-automation/internal/slack"
	"github.com/muszkin/slack-presence-automation/internal/storage"
)

// HandleHomeOpened loads the current state from storage and publishes the
// home view back to Slack. Called by the Socket Mode dispatcher every time
// the user opens the app home tab.
func (c *Commands) HandleHomeOpened(ctx context.Context, userID string) {
	if c.views == nil {
		return
	}
	if err := c.publishHome(ctx, userID); err != nil {
		c.logger.WarnContext(ctx, "publish home view", slog.String("user", userID), slog.Any("err", err))
	}
}

// HandleInteraction routes Block Kit interactions and view submissions to
// the appropriate per-action or per-modal handler.
func (c *Commands) HandleInteraction(ctx context.Context, cb slackgo.InteractionCallback) slackpkg.InteractionResponse {
	switch cb.Type {
	case slackgo.InteractionTypeBlockActions:
		return c.handleBlockAction(ctx, cb)
	case slackgo.InteractionTypeViewSubmission:
		return c.handleViewSubmission(ctx, cb)
	default:
		c.logger.DebugContext(ctx, "ignored interaction", slog.String("type", string(cb.Type)))
		return slackpkg.InteractionResponse{}
	}
}

// compile-time check that Commands satisfies the interaction handler shape.
var _ slackpkg.InteractionHandler = (*Commands)(nil).HandleInteraction

func (c *Commands) handleBlockAction(ctx context.Context, cb slackgo.InteractionCallback) slackpkg.InteractionResponse {
	if len(cb.ActionCallback.BlockActions) == 0 {
		return slackpkg.InteractionResponse{}
	}
	action := cb.ActionCallback.BlockActions[0]
	switch action.ActionID {
	case ActionAddRule:
		c.openModal(ctx, cb.TriggerID, BuildAddRuleModal())
	case ActionAddPattern:
		c.openModal(ctx, cb.TriggerID, BuildAddPatternModal())
	case ActionClearOverrides:
		if _, err := c.store.ClearOverrides(ctx); err != nil {
			c.logger.WarnContext(ctx, "clear overrides", slog.Any("err", err))
		}
		c.fireTrigger()
		c.refreshHome(ctx, cb.User.ID)
	case ActionDeleteRule:
		c.deleteByID(ctx, action.Value, "schedule rule", c.store.DeleteScheduleRule)
		c.fireTrigger()
		c.refreshHome(ctx, cb.User.ID)
	case ActionDeletePattern:
		c.deleteByID(ctx, action.Value, "meeting pattern", c.store.DeleteMeetingPattern)
		c.fireTrigger()
		c.refreshHome(ctx, cb.User.ID)
	default:
		c.logger.DebugContext(ctx, "unhandled block action", slog.String("action_id", action.ActionID))
	}
	return slackpkg.InteractionResponse{}
}

func (c *Commands) handleViewSubmission(ctx context.Context, cb slackgo.InteractionCallback) slackpkg.InteractionResponse {
	switch cb.View.CallbackID {
	case CallbackAddRule:
		return c.handleAddRuleSubmission(ctx, cb)
	case CallbackAddPattern:
		return c.handleAddPatternSubmission(ctx, cb)
	default:
		c.logger.DebugContext(ctx, "unknown view submission", slog.String("callback_id", cb.View.CallbackID))
		return slackpkg.InteractionResponse{}
	}
}

func (c *Commands) handleAddRuleSubmission(ctx context.Context, cb slackgo.InteractionCallback) slackpkg.InteractionResponse {
	values := cb.View.State.Values
	errors := map[string]string{}

	daysMask, ok := parseDaysOfWeek(values)
	if !ok {
		errors["days"] = "Pick at least one day."
	}

	startMin, err := parseTimeOfDay(values, "start_time")
	if err != nil {
		errors["start_time"] = err.Error()
	}
	endMin, err := parseTimeOfDay(values, "end_time")
	if err != nil {
		errors["end_time"] = err.Error()
	}
	if len(errors) == 0 && endMin <= startMin {
		errors["end_time"] = "End time must be after start time."
	}

	presence, ok := parseSelectedValue(values, "presence", "presence")
	if !ok {
		errors["presence"] = "Choose a presence mode."
	}

	priority, err := parseOptionalNumber(values, "priority", "priority")
	if err != nil {
		errors["priority"] = err.Error()
	}

	if len(errors) > 0 {
		return slackpkg.InteractionResponse{Errors: errors}
	}

	params := storage.InsertScheduleRuleParams{
		DaysOfWeek:  int64(daysMask),
		StartMinute: int64(startMin),
		EndMinute:   int64(endMin),
		StatusEmoji: readPlainInput(values, "emoji", "emoji"),
		StatusText:  readPlainInput(values, "text", "text"),
		Presence:    presence,
		Priority:    priority,
	}
	if _, err := c.store.InsertScheduleRule(ctx, params); err != nil {
		c.logger.WarnContext(ctx, "insert schedule rule", slog.Any("err", err))
		return slackpkg.InteractionResponse{Errors: map[string]string{"presence": "Database rejected the rule (check values)."}}
	}
	c.fireTrigger()
	c.refreshHome(ctx, cb.User.ID)
	return slackpkg.InteractionResponse{}
}

func (c *Commands) handleAddPatternSubmission(ctx context.Context, cb slackgo.InteractionCallback) slackpkg.InteractionResponse {
	values := cb.View.State.Values
	errors := map[string]string{}

	pattern := strings.TrimSpace(readPlainInput(values, "title_pattern", "title_pattern"))
	if pattern == "" {
		errors["title_pattern"] = "Pattern must not be empty."
	}
	presence, ok := parseSelectedValue(values, "presence", "presence")
	if !ok {
		errors["presence"] = "Choose a presence mode."
	}
	priority, err := parseOptionalNumber(values, "priority", "priority")
	if err != nil {
		errors["priority"] = err.Error()
	}

	if len(errors) > 0 {
		return slackpkg.InteractionResponse{Errors: errors}
	}

	params := storage.InsertMeetingPatternParams{
		TitlePattern: pattern,
		StatusEmoji:  readPlainInput(values, "emoji", "emoji"),
		StatusText:   "",
		Presence:     presence,
		Priority:     priority,
	}
	if _, err := c.store.InsertMeetingPattern(ctx, params); err != nil {
		c.logger.WarnContext(ctx, "insert meeting pattern", slog.Any("err", err))
		return slackpkg.InteractionResponse{Errors: map[string]string{"title_pattern": "Database rejected the pattern."}}
	}
	c.fireTrigger()
	c.refreshHome(ctx, cb.User.ID)
	return slackpkg.InteractionResponse{}
}

func (c *Commands) publishHome(ctx context.Context, userID string) error {
	applied, err := c.store.GetAppliedState(ctx)
	if err != nil {
		return fmt.Errorf("load applied state: %w", err)
	}
	overrides, err := c.store.ListActiveOverrides(ctx, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("list overrides: %w", err)
	}
	rules, err := c.store.ListScheduleRules(ctx)
	if err != nil {
		return fmt.Errorf("list rules: %w", err)
	}
	patterns, err := c.store.ListMeetingPatterns(ctx)
	if err != nil {
		return fmt.Errorf("list patterns: %w", err)
	}
	view := BuildHomeView(applied, overrides, rules, patterns)
	if err := c.views.PublishHomeView(ctx, userID, view); err != nil {
		return fmt.Errorf("publish home view: %w", err)
	}
	return nil
}

func (c *Commands) refreshHome(ctx context.Context, userID string) {
	if c.views == nil || userID == "" {
		return
	}
	if err := c.publishHome(ctx, userID); err != nil {
		c.logger.WarnContext(ctx, "refresh home view", slog.String("user", userID), slog.Any("err", err))
	}
}

func (c *Commands) openModal(ctx context.Context, triggerID string, modal slackgo.ModalViewRequest) {
	if c.views == nil {
		return
	}
	if err := c.views.OpenModal(ctx, triggerID, modal); err != nil {
		c.logger.WarnContext(ctx, "open modal",
			slog.String("callback_id", modal.CallbackID), slog.Any("err", err))
	}
}

func (c *Commands) deleteByID(ctx context.Context, raw, label string, fn func(context.Context, int64) (int64, error)) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.logger.WarnContext(ctx, "delete: invalid id", slog.String("label", label), slog.String("raw", raw))
		return
	}
	if _, err := fn(ctx, id); err != nil {
		c.logger.WarnContext(ctx, "delete failed",
			slog.String("label", label), slog.Int64("id", id), slog.Any("err", err))
	}
}

// parseDaysOfWeek extracts the selected days from the days multi-select.
// Returns the bitmap (bit 0 = Monday … bit 6 = Sunday) and true when at
// least one day was picked.
func parseDaysOfWeek(values map[string]map[string]slackgo.BlockAction) (uint8, bool) {
	action, ok := lookupAction(values, "days", "days")
	if !ok {
		return 0, false
	}
	var mask uint8
	for _, opt := range action.SelectedOptions {
		mask |= dayBit(opt.Value)
	}
	return mask, mask != 0
}

func dayBit(value string) uint8 {
	switch strings.ToLower(value) {
	case "mon":
		return 1 << 0
	case "tue":
		return 1 << 1
	case "wed":
		return 1 << 2
	case "thu":
		return 1 << 3
	case "fri":
		return 1 << 4
	case "sat":
		return 1 << 5
	case "sun":
		return 1 << 6
	}
	return 0
}

// parseTimeOfDay reads an HH:MM string from a plain_text input and converts
// it to minutes since midnight. Fails fast on empty or malformed values.
func parseTimeOfDay(values map[string]map[string]slackgo.BlockAction, blockID string) (int, error) {
	action, ok := lookupAction(values, blockID, blockID)
	if !ok {
		return 0, fmt.Errorf("enter time in HH:MM (24h)")
	}
	raw := strings.TrimSpace(action.Value)
	if raw == "" {
		return 0, fmt.Errorf("enter time in HH:MM (24h)")
	}
	parsed, err := time.Parse("15:04", raw)
	if err != nil {
		return 0, fmt.Errorf("%q is not valid HH:MM (24h)", raw)
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func parseSelectedValue(values map[string]map[string]slackgo.BlockAction, blockID, actionID string) (string, bool) {
	action, ok := lookupAction(values, blockID, actionID)
	if !ok || action.SelectedOption.Value == "" {
		return "", false
	}
	return action.SelectedOption.Value, true
}

func parseOptionalNumber(values map[string]map[string]slackgo.BlockAction, blockID, actionID string) (int64, error) {
	action, ok := lookupAction(values, blockID, actionID)
	if !ok {
		return 0, nil
	}
	raw := strings.TrimSpace(action.Value)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", raw)
	}
	return n, nil
}

func readPlainInput(values map[string]map[string]slackgo.BlockAction, blockID, actionID string) string {
	action, ok := lookupAction(values, blockID, actionID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(action.Value)
}

func lookupAction(values map[string]map[string]slackgo.BlockAction, blockID, actionID string) (slackgo.BlockAction, bool) {
	block, ok := values[blockID]
	if !ok {
		return slackgo.BlockAction{}, false
	}
	action, ok := block[actionID]
	return action, ok
}
