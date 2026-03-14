package tui

import (
	"charm.land/bubbles/v2/key"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/TimurTurdyev/tim/internal/brand"
)

// Фирменные цвета из пакета brand: те же, что у остального вывода tim. Терминалу без truecolor
// lipgloss подберёт ближайшие сам.
var (
	colAccent = lipgloss.Color(brand.Teal.Hex())
	colOK     = lipgloss.Color(brand.Green.Hex())
	colInfo   = lipgloss.Color(brand.Blue.Hex())
	colDim    = lipgloss.Color(brand.Gray.Hex())
	colBad    = lipgloss.Color(brand.Red.Hex())
	colOnAcc  = lipgloss.Color("#0E1A19")
	colRow    = lipgloss.Color(brand.Shade.Hex())
)

// Символы интерфейса записаны кодами, чтобы исходник оставался набранным с клавиатуры.
const (
	symPointer = "\u203a"
	symCheck   = "\u2713"
	symDot     = "\u2022"
	symBar     = "\u258c"
	symUpDown  = "\u2191\u2193"
	symLR      = "\u2190\u2192"
)

func timTheme(isDark bool) *huh.Styles {
	t := huh.ThemeBase(isDark)
	f := &t.Focused
	f.Base = f.Base.BorderForeground(colAccent)
	f.Card = f.Base
	f.Title = f.Title.Foreground(colAccent).Bold(true)
	f.NoteTitle = f.NoteTitle.Foreground(colAccent).Bold(true)
	f.Description = f.Description.Foreground(colDim)
	f.SelectSelector = lipgloss.NewStyle().Foreground(colAccent).SetString(symPointer + " ")
	f.MultiSelectSelector = f.SelectSelector
	f.SelectedOption = f.SelectedOption.Foreground(colOK)
	f.SelectedPrefix = lipgloss.NewStyle().Foreground(colOK).SetString(symCheck + " ")
	f.UnselectedPrefix = lipgloss.NewStyle().Foreground(colDim).SetString(symDot + " ")
	f.FocusedButton = lipgloss.NewStyle().Foreground(colOnAcc).Background(colAccent).Bold(true).Padding(0, 2).MarginRight(1)
	f.BlurredButton = lipgloss.NewStyle().Foreground(colDim).Background(colRow).Padding(0, 2).MarginRight(1)
	f.TextInput.Prompt = f.TextInput.Prompt.Foreground(colAccent)
	f.TextInput.Cursor = f.TextInput.Cursor.Foreground(colAccent)
	f.TextInput.Placeholder = f.TextInput.Placeholder.Foreground(colDim)
	f.ErrorIndicator = f.ErrorIndicator.Foreground(colBad)
	f.ErrorMessage = f.ErrorMessage.Foreground(colBad)
	t.Blurred = t.Focused
	t.Blurred.Base = t.Blurred.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Help.ShortKey = t.Help.ShortKey.Foreground(colInfo)
	t.Help.ShortDesc = t.Help.ShortDesc.Foreground(colDim)
	t.Help.ShortSeparator = t.Help.ShortSeparator.Foreground(colDim)
	return t
}

// ruKeys: подсказки по клавишам по-русски; д и н работают как y и n, чтобы не переключать раскладку.
func ruKeys() *huh.KeyMap {
	k := huh.NewDefaultKeyMap()
	k.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "отмена"))
	k.Confirm.Toggle.SetHelp(symLR, "выбор")
	k.Confirm.Submit.SetHelp("enter", "ответить")
	k.Confirm.Accept = key.NewBinding(key.WithKeys("y", "Y", "д", "Д"), key.WithHelp("д", "да"))
	k.Confirm.Reject = key.NewBinding(key.WithKeys("n", "N", "н", "Н"), key.WithHelp("н", "нет"))
	k.Input.Submit.SetHelp("enter", "дальше")
	k.Input.AcceptSuggestion.SetHelp("tab", "подставить")
	k.MultiSelect.Toggle.SetHelp("пробел", "отметить")
	k.MultiSelect.Submit.SetHelp("enter", "готово")
	k.MultiSelect.SelectAll.SetHelp("ctrl+a", "все")
	k.MultiSelect.SelectNone.SetHelp("ctrl+a", "ни одного")
	k.MultiSelect.Filter.SetHelp("/", "поиск")
	k.MultiSelect.Up.SetHelp(symUpDown, "выбор")
	k.MultiSelect.Down.SetHelp(symUpDown, "выбор")
	k.Select.Submit.SetHelp("enter", "выбрать")
	return k
}
