package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Entry: строка обозревателя диска.
type Entry struct {
	Name, Path, Hint string
	Size             int64
	Dir              bool // можно зайти внутрь
}

// BrowseOpts: что показывать. Kids может считать долго (du) - зовётся в фоне.
type BrowseOpts struct {
	Root  Entry
	Kids  func(path string) []Entry
	Open  func(path string)
	Total int64 // занято на диске: от него проценты в заголовке
}

type level struct {
	entry   Entry
	kids    []Entry
	cursor  int
	top     int
	loading bool
}

type kidsMsg struct {
	path string
	kids []Entry
}

type browser struct {
	opts   BrowseOpts
	stack  []*level
	marked map[string]Entry
	height int
	width  int
	done   bool
	trash  bool
	flash  string
}

var (
	styleBar    = lipgloss.NewStyle().Foreground(colAccent)
	styleSize   = lipgloss.NewStyle().Foreground(colOK)
	styleMark   = lipgloss.NewStyle().Foreground(colBad).Bold(true)
	styleDirNm  = lipgloss.NewStyle().Bold(true)
	styleCrumbs = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
)

// Browse: папки по размеру с заходом внутрь, как в программах-чистильщиках. Пробел отмечает,
// d - закончить и убрать отмеченное (вернётся trash=true), q - выйти, ничего не трогая.
func Browse(o BrowseOpts) (marked []Entry, trash bool, err error) {
	if !Available() {
		return nil, false, ErrNoTTY
	}
	b := &browser{opts: o, marked: map[string]Entry{}, height: 24, width: 100}
	b.stack = []*level{{entry: o.Root, loading: true}}
	if _, err := tea.NewProgram(b, tea.WithAltScreen()).Run(); err != nil {
		return nil, false, err
	}
	for _, e := range b.marked {
		marked = append(marked, e)
	}
	return marked, b.trash, nil
}

func (b *browser) load(path string) tea.Cmd {
	return func() tea.Msg { return kidsMsg{path, b.opts.Kids(path)} }
}

func (b *browser) cur() *level { return b.stack[len(b.stack)-1] }

func (b *browser) Init() tea.Cmd { return b.load(b.opts.Root.Path) }

func (b *browser) rows() int { return max(5, b.height-9) }

func (b *browser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.height, b.width = msg.Height, msg.Width
	case kidsMsg:
		for _, l := range b.stack {
			if l.entry.Path == msg.path && l.loading {
				l.kids, l.loading = msg.kids, false
			}
		}
	case tea.KeyMsg:
		b.flash = ""
		l := b.cur()
		switch msg.String() {
		case "q", "esc", "ctrl+c", "й":
			b.done = true
			return b, tea.Quit
		case "up", "k", "л":
			l.cursor = max(0, l.cursor-1)
		case "down", "j", "о":
			l.cursor = min(len(l.kids)-1, l.cursor+1)
		case "pgup":
			l.cursor = max(0, l.cursor-b.rows())
		case "pgdown":
			l.cursor = min(len(l.kids)-1, l.cursor+b.rows())
		case "home", "g":
			l.cursor = 0
		case "end", "G":
			l.cursor = len(l.kids) - 1
		case "enter", "right", "l", "д":
			if l.loading || len(l.kids) == 0 {
				break
			}
			e := l.kids[l.cursor]
			if !e.Dir {
				b.flash = "внутрь не зайти: это файлы или место без папок"
				break
			}
			b.stack = append(b.stack, &level{entry: e, loading: true})
			return b, b.load(e.Path)
		case "left", "h", "backspace", "р":
			if len(b.stack) > 1 {
				b.stack = b.stack[:len(b.stack)-1]
			}
		case " ":
			if l.loading || len(l.kids) == 0 {
				break
			}
			e := l.kids[l.cursor]
			if e.Path == "" {
				b.flash = "это не папка - отметить нельзя"
				break
			}
			if _, ok := b.marked[e.Path]; ok {
				delete(b.marked, e.Path)
			} else {
				b.marked[e.Path] = e
			}
			l.cursor = min(len(l.kids)-1, l.cursor+1)
		case "o", "щ":
			if !l.loading && len(l.kids) > 0 && l.kids[l.cursor].Path != "" && b.opts.Open != nil {
				b.opts.Open(l.kids[l.cursor].Path)
				b.flash = "открыто в Finder"
			}
		case "d", "в":
			if len(b.marked) == 0 {
				b.flash = "сначала отметь пробелом, что убрать"
				break
			}
			b.trash, b.done = true, true
			return b, tea.Quit
		}
		l.cursor = max(0, min(l.cursor, len(l.kids)-1))
		if l.cursor < l.top {
			l.top = l.cursor
		}
		if l.cursor >= l.top+b.rows() {
			l.top = l.cursor - b.rows() + 1
		}
	}
	return b, nil
}

func human(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
}

func (b *browser) View() string {
	if b.done {
		return ""
	}
	l := b.cur()
	var s strings.Builder
	var crumbs []string
	for _, x := range b.stack {
		crumbs = append(crumbs, x.entry.Name)
	}
	head := styleCrumbs.Render(strings.Join(crumbs, " "+symPointer+" "))
	head += "  " + styleSize.Render(human(l.entry.Size))
	if b.opts.Total > 0 {
		head += styleHint.Render(fmt.Sprintf("  %d%% занятого", l.entry.Size*100/b.opts.Total))
	}
	s.WriteString(head + "\n\n")
	if l.loading {
		s.WriteString(styleEmpty.Render("считаю, что внутри...") + "\n")
	}
	nameW := max(20, min(46, b.width-60))
	for i := l.top; i < len(l.kids) && i < l.top+b.rows(); i++ {
		e := l.kids[i]
		pct := 0
		if l.entry.Size > 0 {
			pct = int(e.Size * 100 / l.entry.Size)
		}
		filled := max(0, min(16, int(e.Size*16/max(1, l.entry.Size))))
		bar := styleBar.Render(strings.Repeat("\u2588", filled)) + styleHint.Render(strings.Repeat("\u2591", 16-filled))
		mark := "  "
		if _, ok := b.marked[e.Path]; ok && e.Path != "" {
			mark = styleMark.Render(symCheck + " ")
		}
		name := clipTitle(e.Name, nameW)
		if e.Dir {
			name = styleDirNm.Render(name)
		} else {
			name = styleHint.Render(name)
		}
		pad := strings.Repeat(" ", max(0, nameW-len([]rune(clipTitle(e.Name, nameW)))))
		line := fmt.Sprintf("%s%s %s %s  %s%s", mark, styleSize.Render(fmt.Sprintf("%9s", human(e.Size))), bar,
			styleHint.Render(fmt.Sprintf("%3d%%", pct)), name, pad)
		if e.Hint != "" {
			line += "  " + styleHint.Render(clipTitle(e.Hint, max(10, b.width-nameW-40)))
		}
		if i == l.cursor {
			line = styleRow.Render(symPointer) + line
		} else {
			line = " " + line
		}
		s.WriteString(line + "\n")
	}
	if rest := len(l.kids) - (l.top + b.rows()); rest > 0 {
		s.WriteString(styleHint.Render("   и ещё "+strconv.Itoa(rest)+" ниже") + "\n")
	}
	s.WriteString("\n")
	if len(b.marked) > 0 {
		var sum int64
		for _, e := range b.marked {
			sum += e.Size
		}
		s.WriteString(styleMark.Render(fmt.Sprintf("отмечено %d на %s", len(b.marked), human(sum))) + styleHint.Render("  d - убрать в Корзину") + "\n")
	}
	if b.flash != "" {
		s.WriteString(styleHint.Render(b.flash) + "\n")
	}
	keys := []string{symUpDown + " выбор", "enter внутрь", "\u2190 назад", "пробел отметить", "o показать в Finder", "d убрать", "q выход"}
	for i, k := range keys {
		key, desc, _ := strings.Cut(k, " ")
		keys[i] = styleKey.Render(key) + " " + styleHint.Render(desc)
	}
	return styleFrame.Render(strings.TrimRight(s.String(), "\n")) + "\n" + strings.Join(keys, styleHint.Render(" "+symDot+" "))
}
