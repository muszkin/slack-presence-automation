package slack

import (
	"context"
	"fmt"

	slackgo "github.com/slack-go/slack"
)

// ViewsClient wraps the bot-token slack-go Client with the two views.* calls
// App Home needs: publishing the home view and opening modals. It is a thin
// pass-through; test code in the ui package swaps it for a fake.
type ViewsClient struct {
	api *slackgo.Client
}

// NewViewsClient returns a ViewsClient backed by the given slack-go client.
func NewViewsClient(api *slackgo.Client) *ViewsClient {
	return &ViewsClient{api: api}
}

// PublishHomeView calls views.publish so the App Home tab for userID shows
// the given view. The external hash is left empty — we publish unconditionally
// because each reconcile already runs through applied_state dedup.
func (v *ViewsClient) PublishHomeView(ctx context.Context, userID string, view slackgo.HomeTabViewRequest) error {
	req := slackgo.PublishViewContextRequest{UserID: userID, View: view}
	if _, err := v.api.PublishViewContext(ctx, req); err != nil {
		return fmt.Errorf("publish home view: %w", err)
	}
	return nil
}

// OpenModal calls views.open with the given trigger_id, which must come from
// a fresh interactive payload and is valid for only ~3 seconds.
func (v *ViewsClient) OpenModal(ctx context.Context, triggerID string, view slackgo.ModalViewRequest) error {
	if _, err := v.api.OpenViewContext(ctx, triggerID, view); err != nil {
		return fmt.Errorf("open modal: %w", err)
	}
	return nil
}
