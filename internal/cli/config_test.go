package cli

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"

	"danny.vn/secops/config"
)

func TestConfigMouseClickFocusesInput(t *testing.T) {
	form, handler, _, _ := testConfigMouseForm(t, validConfigInstance())
	x, y := configViewLabelPoint(t, form.View(), "Region")

	got := handler.filter(form, leftClick(x, y))
	if _, ok := got.(configMouseFocusMsg); !ok {
		t.Fatalf("focus-only click returned %T, want configMouseFocusMsg", got)
	}
	applyConfigMouseResult(t, form, got)
	if key := form.GetFocusedField().GetKey(); key != configFieldRegion {
		t.Errorf("focused field = %q, want %q", key, configFieldRegion)
	}
}

func TestConfigMouseTitlesDoNotCollideWithValues(t *testing.T) {
	cur := validConfigInstance()
	cur.ProjectID = "Region"
	form, handler, _, _ := testConfigMouseForm(t, cur)
	x, y := configViewLabelPoint(t, form.View(), "Region")

	applyConfigMouseResult(t, form, handler.filter(form, leftClick(x, y)))
	if key := form.GetFocusedField().GetKey(); key != configFieldRegion {
		t.Errorf("focused field = %q, want %q", key, configFieldRegion)
	}
}

func TestConfigMouseClickSetsExactBooleanChoice(t *testing.T) {
	cur := validConfigInstance()
	form, handler, _, _ := testConfigMouseForm(t, cur)

	x, y := configViewButtonPoint(t, form.View(), "Yes", "No", "Yes")
	applyConfigMouseResult(t, form, handler.filter(form, leftClick(x, y)))
	if !cur.ForceIPv4 {
		t.Fatal("clicking Yes left ForceIPv4 false")
	}

	x, y = configViewButtonPoint(t, form.View(), "Yes", "No", "No")
	applyConfigMouseResult(t, form, handler.filter(form, leftClick(x, y)))
	if cur.ForceIPv4 {
		t.Fatal("clicking No left ForceIPv4 true")
	}
}

func TestConfigMouseSaveCannotBypassValidation(t *testing.T) {
	form, handler, save, _ := testConfigMouseForm(t, &config.Instance{})
	x, y := configViewButtonPoint(t, form.View(), "Save", "Cancel", "Save")

	got := handler.filter(form, leftClick(x, y))
	if _, ok := got.(configMouseFocusMsg); !ok {
		t.Fatalf("invalid Save click returned %T, want configMouseFocusMsg", got)
	}
	applyConfigMouseResult(t, form, got)
	if form.State != huh.StateNormal {
		t.Errorf("form state = %v, want normal", form.State)
	}
	if key := form.GetFocusedField().GetKey(); key != configFieldProjectID {
		t.Errorf("focused field = %q, want first invalid field %q", key, configFieldProjectID)
	}
	if !*save {
		t.Error("invalid Save click changed the Save value")
	}
}

func TestConfigMouseSaveCompletesValidForm(t *testing.T) {
	form, handler, save, _ := testConfigMouseForm(t, validConfigInstance())
	x, y := configViewButtonPoint(t, form.View(), "Save", "Cancel", "Save")

	msg := handler.filter(form, leftClick(x, y))
	key, ok := msg.(tea.KeyMsg)
	if !ok || key.String() != "y" {
		t.Fatalf("Save click returned %#v, want y key message", msg)
	}
	runConfigFormMessages(t, form, msg)
	if form.State != huh.StateCompleted {
		t.Errorf("form state = %v, want completed", form.State)
	}
	if !*save {
		t.Error("Save click left save=false")
	}
}

func TestConfigMouseCancelWorksWithInvalidFields(t *testing.T) {
	form, handler, save, mouseCancelled := testConfigMouseForm(t, &config.Instance{})
	x, y := configViewButtonPoint(t, form.View(), "Save", "Cancel", "Cancel")

	got := handler.filter(form, leftClick(x, y))
	key, ok := got.(tea.KeyMsg)
	if !ok || key.Type != tea.KeyCtrlC {
		t.Fatalf("Cancel click returned %#v, want Ctrl+C key message", got)
	}
	if *save {
		t.Error("Cancel click left save=true")
	}
	if !*mouseCancelled {
		t.Error("Cancel click did not mark the mouse-originated cancellation")
	}
}

func TestConfigMouseWorksInShortViewport(t *testing.T) {
	cur := validConfigInstance()
	form, handler, _, _ := testConfigMouseForm(t, cur)
	size := tea.WindowSizeMsg{Width: 80, Height: 24}
	model, _ := form.Update(handler.filter(form, size))
	form = model.(*huh.Form)

	for range 6 {
		msg := handler.filter(form, tea.MouseMsg{
			Button: tea.MouseButtonWheelDown,
			Action: tea.MouseActionPress,
		})
		applyConfigMouseResult(t, form, msg)
	}
	if key := form.GetFocusedField().GetKey(); key != configFieldForceIPv4 {
		t.Fatalf("focused field = %q, want %q", key, configFieldForceIPv4)
	}

	x, y := configViewButtonPoint(t, handler.renderedView(form.View()), "Yes", "No", "Yes")
	applyConfigMouseResult(t, form, handler.filter(form, leftClick(x, y)))
	if !cur.ForceIPv4 {
		t.Error("clicking a button in the scrolled viewport did not change its value")
	}
}

func TestConfigMouseAccountsForRendererTopClipping(t *testing.T) {
	form, handler, _, _ := testConfigMouseForm(t, validConfigInstance())
	size := tea.WindowSizeMsg{Width: 80, Height: 24}
	model, _ := form.Update(handler.filter(form, size))
	form = model.(*huh.Form)

	rawLines := strings.Split(form.View(), "\n")
	if len(rawLines) <= size.Height {
		t.Fatalf("test needs an over-height view; got %d lines for height %d", len(rawLines), size.Height)
	}
	rendered := handler.renderedView(form.View())
	x, y := configViewLabelPoint(t, rendered, "Region")

	applyConfigMouseResult(t, form, handler.filter(form, leftClick(x, y)))
	if key := form.GetFocusedField().GetKey(); key != configFieldRegion {
		t.Errorf("focused field = %q, want %q", key, configFieldRegion)
	}
}

func TestConfigMouseIgnoresNonClicks(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.MouseMsg
	}{
		{
			name: "release",
			msg: tea.MouseMsg{
				X: 1, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease,
			},
		},
		{
			name: "right click",
			msg: tea.MouseMsg{
				X: 1, Y: 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress,
			},
		},
		{
			name: "motion",
			msg: tea.MouseMsg{
				X: 1, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion,
			},
		},
		{
			name: "outside",
			msg: tea.MouseMsg{
				X: 999, Y: 999, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form, handler, _, _ := testConfigMouseForm(t, validConfigInstance())
			before := form.GetFocusedField().GetKey()
			if got := handler.filter(form, tc.msg); got != nil {
				t.Fatalf("filter returned %T, want nil", got)
			}
			if after := form.GetFocusedField().GetKey(); after != before {
				t.Errorf("focus changed from %q to %q", before, after)
			}
		})
	}
}

func TestConfigMouseWheelMovesBetweenFields(t *testing.T) {
	form, handler, _, _ := testConfigMouseForm(t, validConfigInstance())

	next := handler.filter(form, tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
		Action: tea.MouseActionPress,
	})
	applyConfigMouseResult(t, form, next)
	if key := form.GetFocusedField().GetKey(); key != configFieldProjectNumber {
		t.Errorf("wheel down focused %q, want %q", key, configFieldProjectNumber)
	}

	prev := handler.filter(form, tea.MouseMsg{
		Button: tea.MouseButtonWheelUp,
		Action: tea.MouseActionPress,
	})
	applyConfigMouseResult(t, form, prev)
	if key := form.GetFocusedField().GetKey(); key != configFieldProjectID {
		t.Errorf("wheel up focused %q, want %q", key, configFieldProjectID)
	}
}

func TestConfigMouseHitTestingUsesDisplayCells(t *testing.T) {
	view := "\x1b[35m┃ Force IPv4?\x1b[0m\n\n┃       Yes     No"
	target, choice, ok := configMouseTargetAt(view, 10, 2)
	if !ok {
		t.Fatal("button click did not resolve a target")
	}
	if target.key != configFieldForceIPv4 {
		t.Errorf("target = %q, want %q", target.key, configFieldForceIPv4)
	}
	if choice != 'y' {
		t.Errorf("choice = %q, want y", choice)
	}
}

func TestConfigMouseButtonsWorkWhenTitleIsClipped(t *testing.T) {
	view := "┃                       Yes     No\n\n  Save this config?"
	target, choice, ok := configMouseTargetAt(view, 26, 0)
	if !ok {
		t.Fatal("visible button with clipped title did not resolve a target")
	}
	if target.key != configFieldForceIPv4 {
		t.Errorf("target = %q, want %q", target.key, configFieldForceIPv4)
	}
	if choice != 'y' {
		t.Errorf("choice = %q, want y", choice)
	}
}

func TestConfigMouseWorksInVeryShortViewport(t *testing.T) {
	cur := validConfigInstance()
	form, handler, _, _ := testConfigMouseForm(t, cur)
	size := tea.WindowSizeMsg{Width: 80, Height: 7}
	model, _ := form.Update(handler.filter(form, size))
	form = model.(*huh.Form)

	for range 6 {
		msg := handler.filter(form, tea.MouseMsg{
			Button: tea.MouseButtonWheelDown,
			Action: tea.MouseActionPress,
		})
		applyConfigMouseResult(t, form, msg)
	}
	rendered := handler.renderedView(form.View())
	if strings.Contains(ansi.Strip(rendered), "Force IPv4?") {
		t.Fatalf("test needs the Force IPv4 title to be clipped:\n%s", ansi.Strip(rendered))
	}
	x, y := configViewButtonPoint(t, rendered, "Yes", "No", "Yes")

	msg := handler.filter(form, leftClick(x, y))
	runConfigFormMessages(t, form, msg)
	if !cur.ForceIPv4 {
		t.Error("clicking Yes with its title clipped did not enable ForceIPv4")
	}
	if key := form.GetFocusedField().GetKey(); key != configFieldSave {
		t.Errorf("focused field = %q, want %q after choosing Yes", key, configFieldSave)
	}
}

func TestConfigFormMasksAppKey(t *testing.T) {
	cur := validConfigInstance()
	cur.SOARAppKey = "do-not-render-this-secret"
	form, _, _, _ := testConfigMouseForm(t, cur)
	if strings.Contains(ansi.Strip(form.View()), cur.SOARAppKey) {
		t.Fatal("form rendered the SOAR AppKey verbatim")
	}
}

func TestRequiredMissingTreatsSOARAsOptional(t *testing.T) {
	cur := validConfigInstance()
	cur.SOARURL = ""
	cur.SOARAppKey = ""
	if missing := requiredMissing(cur); len(missing) != 0 {
		t.Errorf("requiredMissing = %v, want no missing fields", missing)
	}
}

func TestCheckConfigFieldsTreatsSOARAsOptionalPair(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		key         string
		wantOK      bool
		wantSkipped bool
		wantError   string
	}{
		{
			name:        "SIEM only",
			wantSkipped: true,
		},
		{
			name:   "both SOAR fields",
			url:    "https://tenant.example",
			key:    "secret",
			wantOK: true,
		},
		{
			name:      "URL only",
			url:       "https://tenant.example",
			wantError: "missing: soar_app_key",
		},
		{
			name:      "AppKey only",
			key:       "secret",
			wantError: "missing: soar_url",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checkConfigFields(&config.Instance{
				SOARURL:    tc.url,
				SOARAppKey: tc.key,
			})
			if got.OK != tc.wantOK ||
				got.Skipped != tc.wantSkipped ||
				got.Error != tc.wantError {
				t.Errorf(
					"checkConfigFields() = {OK:%v Skipped:%v Error:%q}, want {OK:%v Skipped:%v Error:%q}",
					got.OK, got.Skipped, got.Error,
					tc.wantOK, tc.wantSkipped, tc.wantError,
				)
			}
		})
	}
}

func TestNormalizeConfigValues(t *testing.T) {
	cur := &config.Instance{
		ProjectID:  " project ",
		Region:     " us ",
		CustomerID: " customer ",
		SOARURL:    " tenant.siemplify-soar.com/ ",
		SOARAppKey: " secret ",
	}
	cur.SetProjectNumber(" 000123 ")

	normalizeConfigValues(cur)

	if cur.ProjectID != "project" ||
		cur.ProjectNumberString() != "000123" ||
		cur.Region != "us" ||
		cur.CustomerID != "customer" ||
		cur.SOARURL != "https://tenant.siemplify-soar.com" ||
		cur.SOARAppKey != "secret" {
		t.Errorf("normalized config = %+v", cur)
	}
}

func testConfigMouseForm(
	t *testing.T,
	cur *config.Instance,
) (*huh.Form, *configMouseHandler, *bool, *bool) {
	t.Helper()
	projectNumber := cur.ProjectNumberString()
	save := true
	mouseCancelled := false
	form, fields := newConfigForm(cur, &projectNumber, &save)
	handler := &configMouseHandler{
		fields:         fields,
		save:           &save,
		mouseCancelled: &mouseCancelled,
	}
	size := tea.WindowSizeMsg{Width: 80, Height: 80}
	model, _ := form.Update(handler.filter(form, size))
	form = model.(*huh.Form)
	if !focusConfigFormField(form, configFieldProjectID) {
		t.Fatal("could not focus the first editable field")
	}
	return form, handler, &save, &mouseCancelled
}

func validConfigInstance() *config.Instance {
	cur := &config.Instance{
		ProjectID:  "project",
		Region:     "us",
		CustomerID: "customer",
	}
	cur.SetProjectNumber("000123")
	return cur
}

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{
		X:      x,
		Y:      y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
}

func applyConfigMouseResult(t *testing.T, form *huh.Form, msg tea.Msg) {
	t.Helper()
	if msg == nil {
		t.Fatal("mouse action returned no message")
	}
	model, _ := form.Update(msg)
	if model != form {
		t.Fatalf("form update returned %T, want the original form", model)
	}
}

func runConfigFormMessages(t *testing.T, form *huh.Form, first tea.Msg) {
	t.Helper()
	queue := []tea.Msg{first}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 32 {
			t.Fatal("form commands did not settle")
		}
		msg := queue[0]
		queue = queue[1:]
		model, cmd := form.Update(msg)
		if model != form {
			t.Fatalf("form update returned %T, want the original form", model)
		}
		queue = append(queue, configCommandMessages(cmd)...)
	}
}

func configCommandMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, child := range batch {
			messages = append(messages, configCommandMessages(child)...)
		}
		return messages
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func configViewLabelPoint(t *testing.T, view, label string) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(view), "\n") {
		if configMouseLineContent(line) == label {
			start := strings.Index(line, label)
			return ansi.StringWidth(line[:start]), y
		}
	}
	t.Fatalf("label %q not found in form view", label)
	return 0, 0
}

func configViewButtonPoint(
	t *testing.T,
	view, affirmative, negative, chosen string,
) (int, int) {
	t.Helper()
	for y, line := range strings.Split(ansi.Strip(view), "\n") {
		if !strings.Contains(line, affirmative) || !strings.Contains(line, negative) {
			continue
		}
		content := configMouseLineContent(line)
		if !strings.HasPrefix(content, affirmative) {
			continue
		}
		if start := strings.Index(line, chosen); start >= 0 {
			return ansi.StringWidth(line[:start]), y
		}
	}
	t.Fatalf("buttons %q/%q not found in form view", affirmative, negative)
	return 0, 0
}
