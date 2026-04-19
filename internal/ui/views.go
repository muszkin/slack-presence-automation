package ui

import (
	"fmt"
	"strings"
	"time"

	slackgo "github.com/slack-go/slack"

	"github.com/muszkin/slack-presence-automation/internal/storage"
)

// Action IDs used by block interactions in the App Home tab. IDs are
// exported because the interaction dispatcher matches on them.
const (
	ActionAddRule        = "add_rule"
	ActionAddPattern     = "add_pattern"
	ActionDeleteOverride = "delete_override"
	ActionDeleteRule     = "delete_rule"
	ActionDeletePattern  = "delete_pattern"
	ActionClearOverrides = "clear_overrides"
)

// Callback IDs used by view submissions (modals).
const (
	CallbackAddRule    = "add_rule_modal"
	CallbackAddPattern = "add_pattern_modal"
)

// BuildHomeView renders the current desired state plus lists of overrides,
// schedule rules, and meeting patterns as a HomeTabViewRequest ready for
// views.publish.
func BuildHomeView(
	applied storage.GetAppliedStateRow,
	overrides []storage.Override,
	rules []storage.ScheduleRule,
	patterns []storage.MeetingPattern,
) slackgo.HomeTabViewRequest {
	blocks := []slackgo.Block{
		headerBlock("Presence"),
		appliedStateBlock(applied),
		slackgo.NewDividerBlock(),
		overridesSection(overrides),
		slackgo.NewDividerBlock(),
	}
	blocks = append(blocks, rulesSection(rules)...)
	blocks = append(blocks, slackgo.NewDividerBlock())
	blocks = append(blocks, patternsSection(patterns)...)

	return slackgo.HomeTabViewRequest{
		Type:   slackgo.VTHomeTab,
		Blocks: slackgo.Blocks{BlockSet: blocks},
	}
}

func headerBlock(text string) slackgo.Block {
	return slackgo.NewHeaderBlock(slackgo.NewTextBlockObject(slackgo.PlainTextType, text, false, false))
}

func appliedStateBlock(applied storage.GetAppliedStateRow) slackgo.Block {
	md := fmt.Sprintf("*Applied state*\nemoji: %s  text: %s\npresence: `%s`  source: `%s`",
		displayOrDash(applied.StatusEmoji), displayOrDash(applied.StatusText),
		applied.Presence, applied.Source)
	return slackgo.NewSectionBlock(
		slackgo.NewTextBlockObject(slackgo.MarkdownType, md, false, false),
		nil, nil)
}

func overridesSection(overrides []storage.Override) slackgo.Block {
	if len(overrides) == 0 {
		return slackgo.NewSectionBlock(
			slackgo.NewTextBlockObject(slackgo.MarkdownType, "*Active overrides*\n_none_", false, false),
			nil, nil)
	}

	var sb strings.Builder
	sb.WriteString("*Active overrides*")
	for _, o := range overrides {
		fmt.Fprintf(&sb, "\n• id=%d presence=`%s` emoji=%s text=%s expires=`%s`",
			o.ID, o.Presence,
			displayOrDash(o.StatusEmoji), displayOrDash(o.StatusText),
			time.Unix(o.ExpiresAt, 0).Format(time.RFC3339))
	}

	clearBtn := slackgo.NewButtonBlockElement(ActionClearOverrides, "",
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "Clear all", false, false))
	clearBtn.Style = slackgo.StyleDanger
	actions := slackgo.NewActionBlock("overrides_actions", clearBtn)

	return slackgo.NewSectionBlock(
		slackgo.NewTextBlockObject(slackgo.MarkdownType, sb.String(), false, false),
		nil, slackgo.NewAccessory(actions.Elements.ElementSet[0]))
}

func rulesSection(rules []storage.ScheduleRule) []slackgo.Block {
	header := slackgo.NewSectionBlock(
		slackgo.NewTextBlockObject(slackgo.MarkdownType, "*Schedule rules*", false, false),
		nil,
		slackgo.NewAccessory(addButton(ActionAddRule, "Add rule")))

	if len(rules) == 0 {
		empty := slackgo.NewSectionBlock(
			slackgo.NewTextBlockObject(slackgo.MarkdownType, "_No schedule rules yet._", false, false),
			nil, nil)
		return []slackgo.Block{header, empty}
	}

	out := []slackgo.Block{header}
	for _, r := range rules {
		md := fmt.Sprintf("id=%d priority=%d presence=`%s` emoji=%s text=%s\ndays=`%s` time=`%s–%s`",
			r.ID, r.Priority, r.Presence,
			displayOrDash(r.StatusEmoji), displayOrDash(r.StatusText),
			formatDays(uint8(r.DaysOfWeek)),
			formatMinutes(int(r.StartMinute)), formatMinutes(int(r.EndMinute)))
		btn := slackgo.NewButtonBlockElement(ActionDeleteRule, fmt.Sprintf("%d", r.ID),
			slackgo.NewTextBlockObject(slackgo.PlainTextType, "Delete", false, false))
		btn.Style = slackgo.StyleDanger
		out = append(out, slackgo.NewSectionBlock(
			slackgo.NewTextBlockObject(slackgo.MarkdownType, md, false, false),
			nil, slackgo.NewAccessory(btn)))
	}
	return out
}

func patternsSection(patterns []storage.MeetingPattern) []slackgo.Block {
	header := slackgo.NewSectionBlock(
		slackgo.NewTextBlockObject(slackgo.MarkdownType, "*Meeting patterns*", false, false),
		nil,
		slackgo.NewAccessory(addButton(ActionAddPattern, "Add pattern")))

	if len(patterns) == 0 {
		empty := slackgo.NewSectionBlock(
			slackgo.NewTextBlockObject(slackgo.MarkdownType, "_No meeting patterns yet._", false, false),
			nil, nil)
		return []slackgo.Block{header, empty}
	}

	out := []slackgo.Block{header}
	for _, p := range patterns {
		matchDesc := fmt.Sprintf("matches title containing %q", p.TitlePattern)
		if p.TitlePattern == "" {
			matchDesc = "matches *any* meeting (default fallback)"
		}
		md := fmt.Sprintf("id=%d priority=%d presence=`%s` emoji=%s\n%s",
			p.ID, p.Priority, p.Presence,
			displayOrDash(p.StatusEmoji), matchDesc)
		btn := slackgo.NewButtonBlockElement(ActionDeletePattern, fmt.Sprintf("%d", p.ID),
			slackgo.NewTextBlockObject(slackgo.PlainTextType, "Delete", false, false))
		btn.Style = slackgo.StyleDanger
		out = append(out, slackgo.NewSectionBlock(
			slackgo.NewTextBlockObject(slackgo.MarkdownType, md, false, false),
			nil, slackgo.NewAccessory(btn)))
	}
	return out
}

func addButton(actionID, label string) *slackgo.ButtonBlockElement {
	btn := slackgo.NewButtonBlockElement(actionID, "",
		slackgo.NewTextBlockObject(slackgo.PlainTextType, label, false, false))
	btn.Style = slackgo.StylePrimary
	return btn
}

func formatDays(bitmap uint8) string {
	names := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	var active []string
	for i, n := range names {
		if bitmap&(1<<uint(i)) != 0 {
			active = append(active, n)
		}
	}
	if len(active) == 0 {
		return "—"
	}
	return strings.Join(active, ",")
}

func formatMinutes(m int) string {
	return fmt.Sprintf("%02d:%02d", m/60, m%60)
}

// BuildAddRuleModal builds the modal for creating a schedule rule. State
// submitted by the user is read back in interactions.go.
func BuildAddRuleModal() slackgo.ModalViewRequest {
	daysOptions := []*slackgo.OptionBlockObject{
		dayOption("mon", "Monday"),
		dayOption("tue", "Tuesday"),
		dayOption("wed", "Wednesday"),
		dayOption("thu", "Thursday"),
		dayOption("fri", "Friday"),
		dayOption("sat", "Saturday"),
		dayOption("sun", "Sunday"),
	}
	daysSelect := slackgo.NewOptionsMultiSelectBlockElement(
		slackgo.MultiOptTypeStatic,
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "Pick one or more", false, false),
		"days", daysOptions...)

	presenceSelect := slackgo.NewOptionsSelectBlockElement(
		slackgo.OptTypeStatic,
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "Choose presence", false, false),
		"presence", presenceOptions()...)

	emojiInput := emojiExternalSelect("emoji")
	textInput := slackgo.NewPlainTextInputBlockElement(
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "deep work", false, false),
		"text")
	priorityInput := slackgo.NewNumberInputBlockElement(
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "0", false, false),
		"priority", true)

	startInput := slackgo.NewPlainTextInputBlockElement(
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "09:00", false, false),
		"start_time")
	endInput := slackgo.NewPlainTextInputBlockElement(
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "17:00", false, false),
		"end_time")

	blocks := []slackgo.Block{
		optional("emoji", "Status emoji", "Leave blank for no emoji.", emojiInput),
		optional("text", "Status text", "Leave blank for no text.", textInput),
		required("presence", "Presence", "", presenceSelect),
		required("days", "Days of week", "", daysSelect),
		required("start_time", "Start time", "24h format HH:MM (e.g. 09:00).", startInput),
		required("end_time", "End time", "24h format HH:MM. Must be after start.", endInput),
		optional("priority", "Priority", "Higher wins when multiple rules match. Default 0.", priorityInput),
	}

	return slackgo.ModalViewRequest{
		Type:       slackgo.VTModal,
		CallbackID: CallbackAddRule,
		Title:      slackgo.NewTextBlockObject(slackgo.PlainTextType, "Add schedule rule", false, false),
		Submit:     slackgo.NewTextBlockObject(slackgo.PlainTextType, "Create", false, false),
		Close:      slackgo.NewTextBlockObject(slackgo.PlainTextType, "Cancel", false, false),
		Blocks:     slackgo.Blocks{BlockSet: blocks},
	}
}

// BuildAddPatternModal builds the modal for creating a meeting pattern.
func BuildAddPatternModal() slackgo.ModalViewRequest {
	presenceSelect := slackgo.NewOptionsSelectBlockElement(
		slackgo.OptTypeStatic,
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "Choose presence", false, false),
		"presence", presenceOptions()...)
	emojiInput := emojiExternalSelect("emoji")
	patternInput := slackgo.NewPlainTextInputBlockElement(
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "Lunch", false, false),
		"title_pattern")
	priorityInput := slackgo.NewNumberInputBlockElement(
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "0", false, false),
		"priority", true)

	blocks := []slackgo.Block{
		required("title_pattern", "Title substring", "Case-insensitive match against event titles.", patternInput),
		required("presence", "Presence", "", presenceSelect),
		optional("emoji", "Status emoji", "Leave blank for no emoji.", emojiInput),
		optional("priority", "Priority", "Higher wins when multiple patterns match. Default 0.", priorityInput),
	}
	return slackgo.ModalViewRequest{
		Type:       slackgo.VTModal,
		CallbackID: CallbackAddPattern,
		Title:      slackgo.NewTextBlockObject(slackgo.PlainTextType, "Add meeting pattern", false, false),
		Submit:     slackgo.NewTextBlockObject(slackgo.PlainTextType, "Create", false, false),
		Close:      slackgo.NewTextBlockObject(slackgo.PlainTextType, "Cancel", false, false),
		Blocks:     slackgo.Blocks{BlockSet: blocks},
	}
}

func required(blockID, label, hint string, element slackgo.BlockElement) *slackgo.InputBlock {
	return inputBlock(blockID, label, hint, element, false)
}

func optional(blockID, label, hint string, element slackgo.BlockElement) *slackgo.InputBlock {
	return inputBlock(blockID, label, hint, element, true)
}

func inputBlock(blockID, label, hint string, element slackgo.BlockElement, optional bool) *slackgo.InputBlock {
	ib := slackgo.NewInputBlock(
		blockID,
		slackgo.NewTextBlockObject(slackgo.PlainTextType, label, false, false),
		nil,
		element)
	if hint != "" {
		ib.Hint = slackgo.NewTextBlockObject(slackgo.PlainTextType, hint, false, false)
	}
	ib.Optional = optional
	return ib
}

// emojiExternalSelect builds an external_select backed by the block_suggestion
// handler in interactions.go. Slack fetches options live as the user types,
// so the built-in Unicode emoji catalogue is searchable without baking all
// ~1800 shortcodes into the view.
func emojiExternalSelect(actionID string) *slackgo.SelectBlockElement {
	el := slackgo.NewOptionsSelectBlockElement(
		slackgo.OptTypeExternal,
		slackgo.NewTextBlockObject(slackgo.PlainTextType, "Type to search, e.g. :brain:", false, false),
		actionID,
	)
	one := 1
	el.MinQueryLength = &one
	return el
}

func presenceOptions() []*slackgo.OptionBlockObject {
	return []*slackgo.OptionBlockObject{
		{Value: "auto", Text: slackgo.NewTextBlockObject(slackgo.PlainTextType, "auto (Slack decides)", false, false)},
		{Value: "available", Text: slackgo.NewTextBlockObject(slackgo.PlainTextType, "available (never auto-escalate)", false, false)},
		{Value: "away", Text: slackgo.NewTextBlockObject(slackgo.PlainTextType, "away (away + muted)", false, false)},
		{Value: "dnd", Text: slackgo.NewTextBlockObject(slackgo.PlainTextType, "dnd (here but muted)", false, false)},
	}
}

func dayOption(value, label string) *slackgo.OptionBlockObject {
	return &slackgo.OptionBlockObject{
		Value: value,
		Text:  slackgo.NewTextBlockObject(slackgo.PlainTextType, label, false, false),
	}
}
