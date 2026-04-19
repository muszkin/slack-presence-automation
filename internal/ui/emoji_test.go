package ui

import (
	"strings"
	"testing"
)

func TestFilterEmojisEmptyQueryReturnsSomething(t *testing.T) {
	t.Parallel()

	got := filterEmojis("")
	if len(got) == 0 {
		t.Fatal("expected some options for empty query so dropdown isn't blank")
	}
	if len(got) > emojiSuggestionLimit {
		t.Errorf("got %d options, want <= %d", len(got), emojiSuggestionLimit)
	}
}

func TestFilterEmojisStripsColonsFromQuery(t *testing.T) {
	t.Parallel()

	withColons := filterEmojis(":brain")
	noColons := filterEmojis("brain")
	if len(withColons) == 0 || len(noColons) == 0 {
		t.Fatalf("both queries should return options (got %d / %d)", len(withColons), len(noColons))
	}
	if withColons[0].Value != noColons[0].Value {
		t.Errorf("colon-prefixed query should match plain query; top = %q vs %q",
			withColons[0].Value, noColons[0].Value)
	}
}

func TestFilterEmojisPrefixBeatsSubstring(t *testing.T) {
	t.Parallel()

	got := filterEmojis("bra")
	if len(got) == 0 {
		t.Fatal("no matches for 'bra'")
	}
	first := strings.Trim(got[0].Value, ":")
	if !strings.HasPrefix(first, "bra") {
		t.Errorf("first match = %q, expected a prefix match for 'bra'", first)
	}
}

func TestFilterEmojisCaseInsensitive(t *testing.T) {
	t.Parallel()

	lower := filterEmojis("pizza")
	upper := filterEmojis("PIZZA")
	if len(lower) == 0 || len(upper) == 0 {
		t.Fatalf("both queries should match pizza (got %d / %d)", len(lower), len(upper))
	}
	if lower[0].Value != upper[0].Value {
		t.Errorf("case should not affect ordering: %q vs %q", lower[0].Value, upper[0].Value)
	}
}

func TestFilterEmojisLabelContainsCharacterAndShortcode(t *testing.T) {
	t.Parallel()

	got := filterEmojis("brain")
	if len(got) == 0 {
		t.Fatal("no matches")
	}
	label := got[0].Text.Text
	if !strings.Contains(label, ":brain:") {
		t.Errorf("label = %q, want it to contain :brain: shortcode", label)
	}
	// Label starts with the Unicode character; the shortcode follows. The
	// Unicode character is not ASCII so byte length > shortcode length.
	if len(label) <= len(":brain:") {
		t.Errorf("label %q looks like it's missing the emoji character", label)
	}
}

func TestFilterEmojisRespectsLimit(t *testing.T) {
	t.Parallel()

	// "a" will match a lot of shortcodes; the filter must still cap at
	// emojiSuggestionLimit so Slack doesn't reject the response.
	got := filterEmojis("a")
	if len(got) > emojiSuggestionLimit {
		t.Errorf("len=%d exceeds limit %d", len(got), emojiSuggestionLimit)
	}
}
