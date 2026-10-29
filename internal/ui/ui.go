// Package ui: цвета и короткие сообщения в терминал.
package ui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"github.com/TimurTurdyev/tim/internal/brand"
)

// Вывод и цвет меняются на ходу (Capture из экрана, UseStderr у шима), а читаются из разных
// горутин - поэтому под замком и атомарно.
var (
	outMu   sync.Mutex
	out     io.Writer = os.Stdout
	enabled atomic.Bool
	depth   = brand.DetectDepth(os.Getenv("COLORTERM"), os.Getenv("TERM"))
	// captureMu: один захват вывода за раз.
	captureMu sync.Mutex
)

func init() { enabled.Store(isTerminal(os.Stdout)) }

// UseStderr: весь вывод в stderr, цвета по нему; для хуков, у которых stdout перехвачен оболочкой.
func UseStderr() {
	outMu.Lock()
	defer outMu.Unlock()
	out = os.Stderr
	enabled.Store(isTerminal(os.Stderr))
}

func isTerminal(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	// FORCE_COLOR и CLICOLOR_FORCE: цвет и в трубу, например для less -R или CI.
	for _, k := range []string{"FORCE_COLOR", "CLICOLOR_FORCE"} {
		if v := os.Getenv(k); v != "" && v != "0" {
			return true
		}
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// paint: палитра выбирается, только когда цвет и правда нужен - без цвета фон не важен.
func paint(s, bold string, c brand.Color, basic string) string {
	if !enabled.Load() {
		return s
	}
	return "\x1b[" + bold + Palette()[c].FG(depth, basic) + "m" + s + "\x1b[0m"
}

// Divert: вывод ui уходит в w, пока экран занимает терминал; вернуть - вызвать то, что вернулось.
func Divert(w io.Writer) func() {
	outMu.Lock()
	prev := out
	out = w
	outMu.Unlock()
	return func() {
		outMu.Lock()
		out = prev
		outMu.Unlock()
	}
}

// Capture выполняет f и возвращает всё, что ушло в ui, с цветами: вывод покажется на экране.
// Захваты не вкладываются: второй ждёт, пока закончится первый.
func Capture(f func()) string {
	captureMu.Lock()
	defer captureMu.Unlock()
	var b lockedBuffer
	outMu.Lock()
	prevOut, prevEnabled := out, enabled.Load()
	out = &b
	outMu.Unlock()
	enabled.Store(true)
	defer func() {
		outMu.Lock()
		out = prevOut
		outMu.Unlock()
		enabled.Store(prevEnabled)
	}()
	f()
	return b.String()
}

// lockedBuffer: буфер, в который пишут из нескольких горутин команды.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Роли текста в фирменных цветах под фон терминала; в терминале на 16 цветов - ближайшие стандартные.
func Label(s string) string { return paint(s, "", brand.Teal, "36") }
func OK(s string) string    { return paint(s, "", brand.Green, "32") }
func Dim(s string) string   { return paint(s, "", brand.Gray, "90") }
func Warn(s string) string  { return paint(s, "", brand.Amber, "33") }
func Bad(s string) string   { return paint(s, "", brand.Red, "31") }
func Info(s string) string  { return paint(s, "", brand.Blue, "34") }
func ghost(s string) string { return paint(s, "", brand.Ghost, "90") }

// Title: заголовок раздела - жирный фирменный.
func Title(s string) string { return paint(s, "1;", brand.Teal, "36") }

// Strong: главное слово строки - основным цветом жирным.
func Strong(s string) string { return paint(s, "1;", brand.Text, "39") }

// Colors: цвета включены и сколько их; для картинок вроде маскота.
func Colors() (bool, brand.Depth) { return enabled.Load(), depth }

// Cols: ширина терминала, куда идёт вывод; 0 - не терминал: труба или захват экраном.
func Cols() int {
	f, ok := writer().(*os.File)
	if !ok {
		return 0
	}
	w, _, err := term.GetSize(f.Fd())
	if err != nil {
		return 0
	}
	return w
}

// Writer: вывод ui для чужого кода, например справки cobra; при захвате экраном пишет в захват.
func Writer() io.Writer { return proxy{} }

type proxy struct{}

func (proxy) Write(p []byte) (int, error) { return writer().Write(p) }

func Printf(format string, a ...any) { _, _ = fmt.Fprintf(writer(), format, a...) }
func Println(a ...any)               { _, _ = fmt.Fprintln(writer(), a...) }
func Print(a ...any)                 { _, _ = fmt.Fprint(writer(), a...) }

func writer() io.Writer {
	outMu.Lock()
	defer outMu.Unlock()
	return out
}

func Errorf(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}

// Verdict: отчёт нашёл беды. Код выхода ненулевой - для скриптов, но вызов верный: примеры команды под
// такой строкой только сбивают с толку.
type Verdict struct{ Msg string }

func (v Verdict) Error() string { return v.Msg }

// Verdictf: итог отчёта с бедами.
func Verdictf(format string, a ...any) error { return Verdict{fmt.Sprintf(format, a...)} }

// Pad добивает строку пробелами до width колонок для таблиц; цвет и широкие символы учитываются.
func Pad(s string, width int) string {
	n := ansi.StringWidth(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}

// ShortErr: последняя, самая содержательная часть обёрнутой ошибки.
func ShortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

// Plural: слово по числу - 1 копия, 2 копии, 5 копий.
func Plural(n int, one, few, many string) string {
	switch {
	case n%100 >= 11 && n%100 <= 14:
		return many
	case n%10 == 1:
		return one
	case n%10 >= 2 && n%10 <= 4:
		return few
	}
	return many
}
