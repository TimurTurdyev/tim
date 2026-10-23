package tui

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Цвета и стили экранов под фон терминала; их задаёт setStyles. Терминалу без truecolor lipgloss
// подберёт ближайшие цвета сам.
var (
	colAccent, colOK, colInfo, colDim, colBad, colWarn               color.Color
	colRow, colText, colLine, colKeyBg, colGhost, colOnAcc, colOnBad color.Color

	styleBrand, styleCrumb, styleCrumbNow, styleStatus                           lipgloss.Style
	styleKeyMain, styleKeyMainDesc, styleKeyCap, styleKeyDesc, styleKeyQuiet     lipgloss.Style
	styleSayOK, styleSayWarn, styleSayBad, styleSayWork                          lipgloss.Style
	styleBtnDanger, styleBtnMain, styleBtnQuiet                                  lipgloss.Style
	styleTitle, styleCount, styleHit, styleHint, styleRow, styleKey, styleEmpty  lipgloss.Style
	stylePrompt, styleChosen, styleEchoKey, styleFrame, styleLine                lipgloss.Style
	styleDim, styleText, styleBold, styleAccent, styleAmber, styleBad, styleGood lipgloss.Style
	styleGhost, stylePaneT, styleTabOn, styleTabOff, styleHead                   lipgloss.Style
)

// Символы интерфейса записаны кодами, чтобы исходник оставался набранным с клавиатуры.
const (
	symPointer = "\u203a"
	symCheck   = "\u2713"
	symDot     = "\u2022"
	symSep     = "\u00b7"
	symBar     = "\u258c"
	symUpDown  = "\u2191\u2193"
	symUp      = "\u2191"
	symDown    = "\u2193"
	symLR      = "\u2190\u2192"
	symLeft    = "\u2190"
	symWarn    = "\u25b2"
	symCross   = "\u2715"
	symLamp    = "\u25cf"
	symArrow   = "\u2192"
	symBlock   = "\u2588"
	symShade   = "\u2591"
)

// spinFrames: кадры работы - четвертинки, алфавит маскота-контейнера; первый же стоит в тексте
// статуса как знак "идёт", каркас подменяет его текущим кадром.
var spinFrames = []string{"\u2596", "\u2598", "\u259d", "\u2597"}

// keyVerbs: одна буква - одно намерение на всех экранах. Подпись может назвать предмет
// ("o редактор", "o Finder"), но не сменить смысл; тест сверяет с таблицей все экраны.
// enter - главное действие экрана, подпись у него своя.
var keyVerbs = map[string][]string{
	"space": {"отметить"},
	"x":     {"убрать", "закрыть"},
	"X":     {"принудительно"},
	"r":     {"обновить", "повторить"},
	"/":     {"найти"},
	"tab":   {"вкладка", "дополнить", "другая"},
	"p":     {"порядок"},
	"n":     {"добавить"},
	"o":     {"редактор", "Finder"},
	"w":     {"браузер"},
	"b":     {"база"},
	"d":     {"dev-сервер"},
	"l":     {"журнал"},
	"c":     {"проверить"},
	"e":     {".env"},
	"f":     {"починить"},
	"a":     {"кто ломится"},
	"s":     {"ssh"},
	"t":     {"трафик"},
	"h":     {"хвосты"},
	"y":     {"скопировать"},
	"u":     {"перезапуск"},
	"z":     {"пауза"},
	"v":     {"все"},
	"1-3":   {"дальше"},
	symLeft: {"вверх"},
}

// ruLayout: та же клавиша в русской раскладке. Переводит каркас и только там, где нет живой
// строки ввода: на главном русская буква - это поиск.
var ruLayout = map[rune]rune{
	'й': 'q', 'ц': 'w', 'у': 'e', 'к': 'r', 'е': 't', 'н': 'y', 'г': 'u', 'ш': 'i', 'щ': 'o', 'з': 'p',
	'ф': 'a', 'ы': 's', 'в': 'd', 'а': 'f', 'п': 'g', 'р': 'h', 'о': 'j', 'л': 'k', 'д': 'l',
	'я': 'z', 'ч': 'x', 'с': 'c', 'м': 'v', 'и': 'b', 'т': 'n', 'ь': 'm',
}

// shadeSwap: серый на выделении читается плохо (3.4:1), поэтому в выбранной строке серый текст
// становится основным. Строки красят и ui, и lipgloss - оба пишут цвет одной и той же записью.
var shadeSwap *strings.Replacer

// shadeOn: фон выделения записью терминала. lipgloss красит фоном только до первого сброса внутри
// строки, а строки списков собраны из кусков с цветом - фон приходится возвращать после каждого.
var shadeOn string

// Тёмные до того, как экран узнает фон: определять его здесь - читать файл на старте каждой команды.
func init() { setStyles(false) }

// setStyles: цвета и стили из палитры под фон.
func setStyles(light bool) {
	p := brand.Dark
	if light {
		p = brand.Light
	}
	c := func(r brand.Color) color.Color { return lipgloss.Color(p[r].Hex()) }
	colAccent, colOK, colInfo = c(brand.Teal), c(brand.Green), c(brand.Blue)
	colDim, colBad, colWarn = c(brand.Gray), c(brand.Red), c(brand.Amber)
	colRow, colText, colLine = c(brand.Shade), c(brand.Text), c(brand.Line)
	colKeyBg, colGhost, colOnAcc, colOnBad = c(brand.KeyCap), c(brand.Ghost), c(brand.OnAccent), c(brand.OnBad)
	shadeSwap = grayToText(p)
	shadeOn = ""
	if _, depth := ui.Colors(); depth != brand.Basic {
		shadeOn = "\x1b[" + p[brand.Shade].BG(depth) + "m"
	}
	chromeStyles()
	textStyles()
}

// chromeStyles: каркас - верхняя строка, строка клавиш, строка состояния, кнопки окон. Фона у
// верхней и нижней строк нет: подложка в 1.03:1 к фону терминала всё равно не видна.
func chromeStyles() {
	styleBrand = lipgloss.NewStyle().Background(colAccent).Foreground(colOnAcc).Bold(true)
	styleCrumb = lipgloss.NewStyle().Foreground(colDim)
	styleCrumbNow = lipgloss.NewStyle().Foreground(colText).Bold(true)
	styleStatus = lipgloss.NewStyle().Foreground(colDim)
	styleKeyMain = lipgloss.NewStyle().Background(colAccent).Foreground(colOnAcc).Bold(true)
	styleKeyMainDesc = lipgloss.NewStyle().Foreground(colText)
	styleKeyCap = lipgloss.NewStyle().Background(colKeyBg).Foreground(colText)
	styleKeyDesc = lipgloss.NewStyle().Foreground(colDim)
	styleKeyQuiet = lipgloss.NewStyle().Foreground(colDim)
	styleSayOK = lipgloss.NewStyle().Foreground(colOK)
	styleSayWarn = lipgloss.NewStyle().Foreground(colWarn)
	styleSayBad = lipgloss.NewStyle().Foreground(colBad)
	styleSayWork = lipgloss.NewStyle().Foreground(colDim)
	styleBtnDanger = lipgloss.NewStyle().Background(colBad).Foreground(colOnBad).Bold(true).Padding(0, 2)
	styleBtnMain = lipgloss.NewStyle().Background(colAccent).Foreground(colOnAcc).Bold(true).Padding(0, 2)
	styleBtnQuiet = lipgloss.NewStyle().Background(colKeyBg).Foreground(colText).Padding(0, 2)
	styleTabOn = lipgloss.NewStyle().Background(colAccent).Foreground(colOnAcc).Bold(true)
	styleTabOff = lipgloss.NewStyle().Foreground(colDim)
	styleLine = lipgloss.NewStyle().Foreground(colLine)
	// Та же полоса слева, что у вопросов huh: все окна визарда выглядят одинаково.
	styleFrame = lipgloss.NewStyle().Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(colAccent).PaddingLeft(1)
}

// textStyles: текст списков, панелей и выбора.
func textStyles() {
	styleTitle = lipgloss.NewStyle().Foreground(colText).Bold(true)
	styleCount = lipgloss.NewStyle().Foreground(colDim)
	styleHit = lipgloss.NewStyle().Foreground(colOK).Bold(true)
	styleHint = lipgloss.NewStyle().Foreground(colDim)
	styleRow = lipgloss.NewStyle().Background(colRow).Bold(true)
	styleKey = lipgloss.NewStyle().Foreground(colText)
	styleEmpty = lipgloss.NewStyle().Foreground(colDim)
	stylePrompt = lipgloss.NewStyle().Foreground(colAccent)
	styleChosen = lipgloss.NewStyle().Foreground(colOK)
	styleEchoKey = lipgloss.NewStyle().Foreground(colDim)
	styleDim = lipgloss.NewStyle().Foreground(colDim)
	styleText = lipgloss.NewStyle().Foreground(colText)
	styleBold = lipgloss.NewStyle().Foreground(colText).Bold(true)
	styleAccent = lipgloss.NewStyle().Foreground(colAccent)
	styleAmber = lipgloss.NewStyle().Foreground(colWarn)
	styleBad = lipgloss.NewStyle().Foreground(colBad)
	styleGood = lipgloss.NewStyle().Foreground(colOK)
	styleGhost = lipgloss.NewStyle().Foreground(colGhost)
	stylePaneT = lipgloss.NewStyle().Foreground(colText).Bold(true)
	styleHead = lipgloss.NewStyle().Foreground(colDim)
}

func grayToText(p brand.Palette) *strings.Replacer {
	g, t := p[brand.Gray], p[brand.Text]
	return strings.NewReplacer(
		fmt.Sprintf("38;2;%d;%d;%d", g.R, g.G, g.B), fmt.Sprintf("38;2;%d;%d;%d", t.R, t.G, t.B),
		fmt.Sprintf("38;5;%dm", g.Code256()), fmt.Sprintf("38;5;%dm", t.Code256()),
	)
}

// onShade: строка на фоне выделения - без серого, фон держится через все сбросы внутри.
func onShade(s string) string {
	s = shadeSwap.Replace(s)
	if shadeOn == "" {
		return s
	}
	for _, reset := range []string{"\x1b[0m", "\x1b[m", "\x1b[49m"} {
		s = strings.ReplaceAll(s, reset, reset+shadeOn)
	}
	return shadeOn + s + "\x1b[49m"
}

// useBackground: терминал ответил, какой у него фон. Палитра - по ответу, фон запоминается для
// обычного вывода и следующего экрана; true - палитра сменилась и нарисованное надо пересобрать.
func useBackground(msg tea.BackgroundColorMsg) bool {
	if ui.ThemeFixed() {
		return false
	}
	light := !msg.IsDark()
	rememberBackground(light)
	if light == ui.Light() {
		return false
	}
	ui.SetLight(light)
	setStyles(light)
	return true
}

func rememberBackground(light bool) {
	word := "dark"
	if light {
		word = "light"
	}
	path := ui.BackgroundFile()
	if old, err := os.ReadFile(path); runner.DryRun || err == nil && string(old) == word {
		return
	}
	_ = runner.WriteAtomic(path, []byte(word), 0o644)
}

// timTheme: huh сам фон не спрашивает и всегда передаёт false, поэтому фон тот же, что у всего tim.
func timTheme(bool) *huh.Styles {
	t := huh.ThemeBase(!ui.Light())
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
