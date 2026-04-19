// Package slack wraps the slack-go SDK with two narrow responsibilities:
// (1) applying a DesiredState to the signed-in user's Slack profile (status,
// presence, DND) and (2) running the Socket Mode event loop that dispatches
// slash commands to a caller-supplied handler.
package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	slackgo "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Presence is the subset of Slack presence modes this service drives.
type Presence string

// Presence modes recognised by the Slack layer. PresenceDND is not a native
// Slack presence — it is implemented as PresenceAuto + an active DND snooze.
// PresenceAvailable is a semantic alias of PresenceAuto at the API level
// (Slack has no "force active" endpoint); it exists so the resolver can
// express "never auto-escalate to DND/away" distinctly from the default.
const (
	PresenceAuto      Presence = "auto"
	PresenceAvailable Presence = "available"
	PresenceAway      Presence = "away"
	PresenceDND       Presence = "dnd"

	// snoozeMinutes is how long we snooze DND at a time. We rely on the
	// applier's dedup to re-extend it on the next tick if DND remains
	// desired, so picking a window slightly longer than the longest we
	// expect a single DND session avoids premature expiry.
	snoozeMinutes = 60
)

// Status is what the applier hands the Slack layer on every applied change.
type Status struct {
	Emoji    string
	Text     string
	Presence Presence
}

// userAPI is the small slice of slack-go's *Client that we call with the
// user token (xoxp-). Declared as an interface for test substitution.
type userAPI interface {
	SetUserCustomStatusContext(ctx context.Context, statusText, statusEmoji string, statusExpiration int64) error
	SetUserPresenceContext(ctx context.Context, presence string) error
	SetSnoozeContext(ctx context.Context, minutes int) (*slackgo.DNDStatus, error)
	EndSnoozeContext(ctx context.Context) (*slackgo.DNDStatus, error)
}

// StatusClient applies DesiredState to the signed-in user's Slack profile.
type StatusClient struct {
	api    userAPI
	logger *slog.Logger
}

// NewStatusClient builds a StatusClient backed by a user-token slack-go client.
func NewStatusClient(userToken string, logger *slog.Logger) *StatusClient {
	return &StatusClient{
		api:    slackgo.New(userToken),
		logger: logger,
	}
}

// newStatusClientWithAPI lets tests inject a fake userAPI.
func newStatusClientWithAPI(api userAPI, logger *slog.Logger) *StatusClient {
	return &StatusClient{api: api, logger: logger}
}

// Apply pushes the given Status to Slack. It always sets custom status first,
// then reconciles presence and DND based on the requested Presence mode.
//
//   - PresenceAuto: presence=auto and snooze ended (fully reachable).
//   - PresenceAway: presence=away and snooze active (away with muted
//     notifications — "I'm not here, don't page me").
//   - PresenceDND:  presence=auto and snooze active (here but busy —
//     "I'm here, just don't interrupt").
func (c *StatusClient) Apply(ctx context.Context, s Status) error {
	if err := c.api.SetUserCustomStatusContext(ctx, s.Text, s.Emoji, 0); err != nil {
		return fmt.Errorf("set custom status: %w", err)
	}

	switch s.Presence {
	case PresenceAuto, PresenceAvailable:
		if _, err := c.api.EndSnoozeContext(ctx); err != nil && !isSnoozeNotActive(err) {
			return fmt.Errorf("end snooze: %w", err)
		}
		if err := c.api.SetUserPresenceContext(ctx, string(PresenceAuto)); err != nil {
			return fmt.Errorf("set presence auto: %w", err)
		}
	case PresenceAway:
		if err := c.api.SetUserPresenceContext(ctx, string(PresenceAway)); err != nil {
			return fmt.Errorf("set presence away: %w", err)
		}
		if _, err := c.api.SetSnoozeContext(ctx, snoozeMinutes); err != nil {
			return fmt.Errorf("set snooze for away: %w", err)
		}
	case PresenceDND:
		if err := c.api.SetUserPresenceContext(ctx, string(PresenceAuto)); err != nil {
			return fmt.Errorf("set presence auto for DND: %w", err)
		}
		if _, err := c.api.SetSnoozeContext(ctx, snoozeMinutes); err != nil {
			return fmt.Errorf("set snooze for DND: %w", err)
		}
	default:
		return fmt.Errorf("unknown presence mode: %q", s.Presence)
	}
	return nil
}

// isSnoozeNotActive matches Slack's "snooze_not_active" response, which is
// the benign outcome of EndSnooze when DND is already off.
func isSnoozeNotActive(err error) bool {
	var resp slackgo.SlackErrorResponse
	return errors.As(err, &resp) && resp.Err == "snooze_not_active"
}

// SlashCommand is the caller-facing projection of a Slack slash command.
type SlashCommand struct {
	Command string
	Text    string
	UserID  string
}

// SlashResponse is returned by a CommandHandler and sent back to Slack.
type SlashResponse struct {
	Text string
}

// CommandHandler handles a single slash command invocation and returns the
// text Slack should show back to the user.
type CommandHandler func(ctx context.Context, cmd SlashCommand) SlashResponse

// HomeOpenedHandler is invoked when a user opens the app home tab; the
// handler is expected to call views.publish to render the current home view.
type HomeOpenedHandler func(ctx context.Context, userID string)

// InteractionResponse is what an InteractionHandler returns. The response
// is interpreted based on the originating interaction type:
//
//   - View submission: Errors keeps the modal open with per-block messages
//     attached; a non-empty Errors map maps block_id → message.
//   - Block suggestion (external_select populate): Options is the filtered
//     option list Slack should render in the dropdown.
//   - Block action: neither field is used; return value is ignored.
type InteractionResponse struct {
	Errors  map[string]string
	Options []slackgo.OptionBlockObject
}

// Empty reports whether the response carries no data (no validation errors
// and no suggestion options).
func (r InteractionResponse) Empty() bool {
	return len(r.Errors) == 0 && len(r.Options) == 0
}

// InteractionHandler is invoked for Block Kit interactions and view
// submissions. The raw InteractionCallback exposes the trigger_id (needed
// for views.open), user, action payload, and submitted view state.
type InteractionHandler func(ctx context.Context, callback slackgo.InteractionCallback) InteractionResponse

// SocketRunner owns the Socket Mode connection and dispatches slash
// commands, App Home events, and interactive callbacks to caller-supplied
// handlers. Handlers are optional and default to no-ops — unset events are
// still acked so Slack stops retrying.
type SocketRunner struct {
	client             *socketmode.Client
	slashHandler       CommandHandler
	homeOpenedHandler  HomeOpenedHandler
	interactionHandler InteractionHandler
	logger             *slog.Logger
}

// NewSocketRunner builds a runner with the given tokens and logger. All
// handlers start nil and must be wired through the Set* methods before Run.
func NewSocketRunner(appToken, botToken string, logger *slog.Logger) *SocketRunner {
	api := slackgo.New(botToken, slackgo.OptionAppLevelToken(appToken))
	return &SocketRunner{
		client: socketmode.New(api),
		logger: logger,
	}
}

// SetSlashHandler registers the handler for `/presence` invocations.
func (r *SocketRunner) SetSlashHandler(h CommandHandler) {
	r.slashHandler = h
}

// SetHomeOpenedHandler registers a handler called every time a user opens
// the app home tab. Only the last registered handler is used.
func (r *SocketRunner) SetHomeOpenedHandler(h HomeOpenedHandler) {
	r.homeOpenedHandler = h
}

// SetInteractionHandler registers a handler called for every Block Kit
// interaction and view submission payload.
func (r *SocketRunner) SetInteractionHandler(h InteractionHandler) {
	r.interactionHandler = h
}

// Client returns the underlying slack-go *Client used for Socket Mode so
// callers can issue views.publish / views.open / chat.postMessage calls
// using the same authenticated bot identity.
func (r *SocketRunner) Client() *slackgo.Client {
	return &r.client.Client
}

// Run blocks until ctx is cancelled or the Socket Mode connection errors out
// in a way the client cannot recover from.
func (r *SocketRunner) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- r.client.RunContext(ctx)
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			if err == nil {
				return nil
			}
			return fmt.Errorf("socket mode: %w", err)
		case evt, ok := <-r.client.Events:
			if !ok {
				return errors.New("socket mode events channel closed")
			}
			r.handleEvent(ctx, evt)
		}
	}
}

func (r *SocketRunner) handleEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		r.logger.InfoContext(ctx, "slack socket mode connecting")
	case socketmode.EventTypeConnected:
		r.logger.InfoContext(ctx, "slack socket mode connected")
	case socketmode.EventTypeDisconnect:
		r.logger.WarnContext(ctx, "slack socket mode disconnected")
	case socketmode.EventTypeSlashCommand:
		r.dispatchSlashCommand(ctx, evt)
	case socketmode.EventTypeEventsAPI:
		r.dispatchEventsAPI(ctx, evt)
	case socketmode.EventTypeInteractive:
		r.dispatchInteraction(ctx, evt)
	default:
		r.logger.DebugContext(ctx, "slack socket mode event ignored", slog.String("type", string(evt.Type)))
	}
}

func (r *SocketRunner) dispatchSlashCommand(ctx context.Context, evt socketmode.Event) {
	cmd, ok := evt.Data.(slackgo.SlashCommand)
	if !ok {
		r.logger.WarnContext(ctx, "slash command event with unexpected payload")
		return
	}
	if r.slashHandler == nil {
		r.ackIfNeeded(ctx, evt)
		return
	}
	response := r.slashHandler(ctx, SlashCommand{
		Command: cmd.Command,
		Text:    cmd.Text,
		UserID:  cmd.UserID,
	})
	payload := map[string]any{
		"response_type": "ephemeral",
		"text":          response.Text,
	}
	if err := r.client.Ack(*evt.Request, payload); err != nil {
		r.logger.WarnContext(ctx, "ack slash command", slog.Any("err", err))
	}
}

func (r *SocketRunner) dispatchEventsAPI(ctx context.Context, evt socketmode.Event) {
	payload, ok := evt.Data.(slackevents.EventsAPIEvent)
	if !ok {
		r.ackIfNeeded(ctx, evt)
		return
	}
	if payload.Type == slackevents.CallbackEvent {
		if home, ok := payload.InnerEvent.Data.(*slackevents.AppHomeOpenedEvent); ok && r.homeOpenedHandler != nil {
			r.homeOpenedHandler(ctx, home.User)
		}
	}
	r.ackIfNeeded(ctx, evt)
}

func (r *SocketRunner) dispatchInteraction(ctx context.Context, evt socketmode.Event) {
	callback, ok := evt.Data.(slackgo.InteractionCallback)
	if !ok {
		r.ackIfNeeded(ctx, evt)
		return
	}
	if r.interactionHandler == nil {
		r.ackIfNeeded(ctx, evt)
		return
	}
	resp := r.interactionHandler(ctx, callback)
	switch {
	case callback.Type == slackgo.InteractionTypeViewSubmission && len(resp.Errors) > 0:
		payload := map[string]any{
			"response_action": "errors",
			"errors":          resp.Errors,
		}
		if err := r.client.Ack(*evt.Request, payload); err != nil {
			r.logger.WarnContext(ctx, "ack view submission with errors", slog.Any("err", err))
		}
	case callback.Type == slackgo.InteractionTypeBlockSuggestion:
		payload := map[string]any{"options": resp.Options}
		if err := r.client.Ack(*evt.Request, payload); err != nil {
			r.logger.WarnContext(ctx, "ack block suggestion", slog.Any("err", err))
		}
	default:
		r.ackIfNeeded(ctx, evt)
	}
}

func (r *SocketRunner) ackIfNeeded(ctx context.Context, evt socketmode.Event) {
	if evt.Request == nil {
		return
	}
	if err := r.client.Ack(*evt.Request); err != nil {
		r.logger.WarnContext(ctx, "ack event", slog.Any("err", err))
	}
}
