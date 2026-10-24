package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Строка ввода на месте строки состояния: экран остаётся виден, ввод - одна строка внизу. Сеть
// добавляет так цель обхода.

// inline: наложение, которое рисуется в строке состояния, а не поверх тела.
type inline interface {
	line(width int) string
}

type askOverlay struct {
	input textinput.Model
	hint  string
	done  func(text string) tea.Cmd
}

// Ask: строка ввода внизу экрана; hint - что будет по enter, серым справа.
func (a *App) Ask(placeholder, hint string, done func(text string) tea.Cmd) tea.Cmd {
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = placeholder
	in.SetWidth(60)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	in.Focus()
	a.overlay = &askOverlay{input: in, hint: hint, done: done}
	return textinput.Blink
}

func (o *askOverlay) update(msg tea.KeyPressMsg, app *App) tea.Cmd {
	switch msg.String() {
	case "esc":
		app.overlay = nil
		return nil
	case "enter":
		text := strings.TrimSpace(o.input.Value())
		app.overlay = nil
		if text == "" {
			return nil
		}
		return o.done(text)
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return cmd
}

func (o *askOverlay) compose(base []string, _, _ int) []string { return base }

func (o *askOverlay) keys() []Key { return nil }

func (o *askOverlay) line(width int) string {
	left := " " + o.input.View()
	right := styleDim.Render(o.hint+" "+symSep+" esc") + " "
	if ui.Width(left)+ui.Width(right)+2 > width {
		right = ""
	}
	return left + strings.Repeat(" ", max(1, width-ui.Width(left)-ui.Width(right))) + right
}
