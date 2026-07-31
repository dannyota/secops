package cli

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
)

const (
	configFieldProjectID     = "project_id"
	configFieldProjectNumber = "project_number"
	configFieldRegion        = "region"
	configFieldCustomerID    = "customer_id"
	configFieldSOARURL       = "soar_url"
	configFieldSOARAppKey    = "soar_app_key"
	configFieldForceIPv4     = "force_ipv4"
	configFieldSave          = "save"
)

type configMouseTarget struct {
	key         string
	title       string
	affirmative string
	negative    string
}

var configMouseTargets = []configMouseTarget{
	{key: configFieldProjectID, title: "Project ID"},
	{key: configFieldProjectNumber, title: "Project number"},
	{key: configFieldRegion, title: "Region"},
	{key: configFieldCustomerID, title: "Customer ID"},
	{key: configFieldSOARURL, title: "SOAR URL"},
	{key: configFieldSOARAppKey, title: "SOAR AppKey"},
	{key: configFieldForceIPv4, title: "Force IPv4?", affirmative: "Yes", negative: "No"},
	{key: configFieldSave, title: "Save this config?", affirmative: "Save", negative: "Cancel"},
}

type configMouseHandler struct {
	fields         []huh.Field
	save           *bool
	mouseCancelled *bool
	width          int
	height         int
}

// configMouseFocusMsg asks Huh to rebuild its viewport after the filter has
// moved focus directly. Returning nil would mutate the model without a redraw.
type configMouseFocusMsg struct{}

// filter translates mouse input into the same key messages Huh already uses,
// keeping validation and keyboard behavior in one place.
func (h *configMouseHandler) filter(model tea.Model, msg tea.Msg) tea.Msg {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width, h.height = msg.Width, msg.Height
		return msg
	case tea.MouseMsg:
		return h.filterMouse(model, msg)
	default:
		return msg
	}
}

func (h *configMouseHandler) filterMouse(model tea.Model, mouse tea.MouseMsg) tea.Msg {
	form, ok := model.(*huh.Form)
	if !ok {
		return mouse
	}

	switch {
	case mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonWheelUp:
		return configMouseWheelMsg(form, -1)
	case mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonWheelDown:
		return configMouseWheelMsg(form, 1)
	case mouse.Action != tea.MouseActionPress || mouse.Button != tea.MouseButtonLeft:
		return nil
	}

	target, choice, ok := configMouseTargetAt(h.renderedView(form.View()), mouse.X, mouse.Y)
	if !ok {
		return nil
	}

	// Cancel must remain available even when an earlier required field is
	// invalid. Route it through Huh's normal abort cleanup, then translate that
	// one mouse-originated abort to a clean cancellation in runConfigForm.
	if target.key == configFieldSave && choice == 'n' {
		*h.save = false
		*h.mouseCancelled = true
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}

	if !focusConfigFormField(form, target.key) {
		return nil
	}

	if target.key == configFieldSave && choice == 'y' {
		if invalid := h.firstInvalidField(); invalid != "" {
			focusConfigFormField(form, invalid)
			return configMouseFocusMsg{}
		}
	}
	if choice == 0 {
		return configMouseFocusMsg{}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{choice}}
}

func (h *configMouseHandler) firstInvalidField() string {
	for i, field := range h.fields {
		if field.Error() != nil {
			return configMouseTargets[i].key
		}
	}
	return ""
}

// renderedView mirrors Bubble Tea's standard renderer: an over-height view is
// clipped from the top, then each remaining line is truncated to the terminal
// width. Mouse coordinates are relative to this final on-screen view.
func (h *configMouseHandler) renderedView(view string) string {
	lines := strings.Split(view, "\n")
	if h.height > 0 && len(lines) > h.height {
		lines = lines[len(lines)-h.height:]
	}
	if h.width > 0 {
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, h.width, "")
		}
	}
	return strings.Join(lines, "\n")
}

func configMouseWheelMsg(form *huh.Form, direction int) tea.Msg {
	current := configMouseTargetIndex(form.GetFocusedField().GetKey())
	if direction < 0 && current > 0 {
		return huh.PrevField()
	}
	if direction > 0 && current >= 0 && current < len(configMouseTargets)-1 {
		return huh.NextField()
	}
	return nil
}

func focusConfigFormField(form *huh.Form, target string) bool {
	targetIndex := configMouseTargetIndex(target)
	if targetIndex < 0 {
		return false
	}
	for range configMouseTargets {
		currentIndex := configMouseTargetIndex(form.GetFocusedField().GetKey())
		if currentIndex == targetIndex {
			return true
		}
		if currentIndex < 0 || currentIndex < targetIndex {
			form.NextField()
		} else {
			form.PrevField()
		}
	}
	return form.GetFocusedField().GetKey() == target
}

func configMouseTargetIndex(key string) int {
	for i, target := range configMouseTargets {
		if target.key == key {
			return i
		}
	}
	return -1
}

// configMouseTargetAt derives hitboxes from the rendered viewport on every
// click, so resize, wrapping, validation errors, and Huh's scrolling stay in
// sync. Coordinates are display cells because the form contains Unicode borders.
func configMouseTargetAt(view string, x, y int) (configMouseTarget, rune, bool) {
	if x < 0 || y < 0 {
		return configMouseTarget{}, 0, false
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	if y >= len(lines) {
		return configMouseTarget{}, 0, false
	}

	// A very short viewport can clip a confirm's title while leaving its
	// centered buttons visible. The two button pairs are unique, so resolve
	// them directly before building title-based field ranges.
	for _, target := range configMouseTargets {
		if choice := configMouseButtonChoice(target, lines[y], x); choice != 0 {
			return target, choice, true
		}
	}

	type location struct {
		target configMouseTarget
		row    int
	}
	locations := make([]location, 0, len(configMouseTargets))
	for _, target := range configMouseTargets {
		for row, line := range lines {
			if configMouseLineContent(line) == target.title {
				locations = append(locations, location{target: target, row: row})
				break
			}
		}
	}

	for i, loc := range locations {
		end := len(lines)
		if i+1 < len(locations) {
			end = locations[i+1].row
		}
		if y < loc.row || y >= end {
			continue
		}
		return loc.target, configMouseButtonChoice(loc.target, lines[y], x), true
	}
	return configMouseTarget{}, 0, false
}

func configMouseButtonChoice(target configMouseTarget, line string, x int) rune {
	if target.affirmative == "" || target.negative == "" ||
		!strings.Contains(line, target.affirmative) ||
		!strings.Contains(line, target.negative) {
		return 0
	}
	content := configMouseLineContent(line)
	if !strings.HasPrefix(content, target.affirmative) {
		return 0
	}
	for _, button := range []struct {
		label string
		key   rune
	}{
		{label: target.affirmative, key: 'y'},
		{label: target.negative, key: 'n'},
	} {
		byteStart := strings.Index(line, button.label)
		if byteStart < 0 {
			continue
		}
		cellStart := ansi.StringWidth(line[:byteStart])
		const buttonPadding = 2
		if x >= cellStart-buttonPadding &&
			x < cellStart+ansi.StringWidth(button.label)+buttonPadding {
			return button.key
		}
	}
	return 0
}

func configMouseLineContent(line string) string {
	content := strings.TrimSpace(line)
	return strings.TrimSpace(strings.TrimPrefix(content, "┃"))
}
