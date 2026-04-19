package ui

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

type fakeEmojiLister struct {
	emoji    map[string]string
	err      error
	calls    int
	failOnce bool
}

func (f *fakeEmojiLister) ListEmoji(_ context.Context) (map[string]string, error) {
	f.calls++
	if f.err != nil {
		if f.failOnce {
			f.err = nil
		}
		return nil, errors.New("boom")
	}
	return f.emoji, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCatalogFilterBuiltinOnlyWhenListerNil(t *testing.T) {
	t.Parallel()

	cat := NewCatalog(nil, time.Minute, discardLogger())
	got := cat.Filter(t.Context(), "brain")
	if len(got) == 0 {
		t.Fatal("expected :brain: to be in built-in catalogue")
	}
	if got[0].Value != ":brain:" {
		t.Errorf("top match = %q, want :brain:", got[0].Value)
	}
}

func TestCatalogFilterIncludesCustomEmoji(t *testing.T) {
	t.Parallel()

	lister := &fakeEmojiLister{emoji: map[string]string{
		"company-logo": "https://example.com/logo.png",
		"team-banner":  "https://example.com/banner.png",
	}}
	cat := NewCatalog(lister, time.Minute, discardLogger())

	got := cat.Filter(t.Context(), "company")
	if len(got) == 0 {
		t.Fatal("expected company-logo match")
	}
	found := false
	for _, o := range got {
		if o.Value == ":company-logo:" {
			found = true
			if strings.Contains(o.Text.Text, " ") {
				// Custom emoji label must not add a Unicode char prefix since there is none.
				if strings.HasPrefix(o.Text.Text, " ") {
					t.Errorf("custom emoji label starts with space: %q", o.Text.Text)
				}
			}
			break
		}
	}
	if !found {
		t.Errorf("custom emoji not in results: %+v", got)
	}
}

func TestCatalogCachesWithinTTL(t *testing.T) {
	t.Parallel()

	lister := &fakeEmojiLister{emoji: map[string]string{"foo": "https://x"}}
	cat := NewCatalog(lister, time.Hour, discardLogger())

	cat.Filter(t.Context(), "foo")
	cat.Filter(t.Context(), "foo")
	cat.Filter(t.Context(), "brain")

	if lister.calls != 1 {
		t.Errorf("emoji.list calls = %d, want 1 (TTL must coalesce)", lister.calls)
	}
}

func TestCatalogRefreshesAfterTTL(t *testing.T) {
	t.Parallel()

	lister := &fakeEmojiLister{emoji: map[string]string{"foo": "https://x"}}
	cat := NewCatalog(lister, time.Millisecond, discardLogger())

	cat.Filter(t.Context(), "foo")
	time.Sleep(5 * time.Millisecond)
	cat.Filter(t.Context(), "foo")

	if lister.calls < 2 {
		t.Errorf("emoji.list calls = %d, want >= 2 after TTL elapsed", lister.calls)
	}
}

func TestCatalogSurvivesListerError(t *testing.T) {
	t.Parallel()

	lister := &fakeEmojiLister{err: errors.New("slack 503")}
	cat := NewCatalog(lister, time.Hour, discardLogger())

	// Filter must still return built-in matches even if emoji.list failed.
	got := cat.Filter(t.Context(), "brain")
	if len(got) == 0 {
		t.Fatal("expected built-in :brain: even when emoji.list fails")
	}
}

func TestCatalogEmptyQueryReturnsSomething(t *testing.T) {
	t.Parallel()

	cat := NewCatalog(nil, time.Minute, discardLogger())
	got := cat.Filter(t.Context(), "")
	if len(got) == 0 {
		t.Fatal("empty query should still show the first page of the catalogue")
	}
	if len(got) > emojiSuggestionLimit {
		t.Errorf("len=%d exceeds limit %d", len(got), emojiSuggestionLimit)
	}
}

func TestCatalogPrefixBeatsSubstring(t *testing.T) {
	t.Parallel()

	cat := NewCatalog(nil, time.Minute, discardLogger())
	got := cat.Filter(t.Context(), "bra")
	if len(got) == 0 {
		t.Fatal("no matches for 'bra'")
	}
	first := strings.Trim(got[0].Value, ":")
	if !strings.HasPrefix(first, "bra") {
		t.Errorf("first match = %q, expected a prefix match for 'bra'", first)
	}
}

func TestCatalogLimitRespectedEvenForBroadQuery(t *testing.T) {
	t.Parallel()

	cat := NewCatalog(nil, time.Minute, discardLogger())
	got := cat.Filter(t.Context(), "a")
	if len(got) > emojiSuggestionLimit {
		t.Errorf("len=%d exceeds limit %d", len(got), emojiSuggestionLimit)
	}
}
