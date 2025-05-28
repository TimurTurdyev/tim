// Package ui: цвета и короткие сообщения в терминал.
package ui

import (
	"fmt"

	"io"
	"os"
	"strings"

	"github.com/TimurTurdyev/tim/internal/brand"
)

var (
	enabled           = isTerminal(os.Stdout)
	out     io.Writer = os.Stdout
	depth             = brand.DetectDepth(os.Getenv("COLORTERM"), os.Getenv("TERM"))
)

// UseStderr: весь вывод в stderr, цвета по нему; для хуков, у которых stdout перехвачен оболочкой.
func UseStderr() {
	out = os.Stderr
	enabled = isTerminal(os.Stderr)
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

func wrap(code, s string) string {
	if !enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

// Роли текста в фирменных цветах; в терминале на 16 цветов - ближайшие стандартные.
func Label(s string) string { return wrap(brand.Teal.FG(depth, "36"), s) }
func OK(s string) string    { return wrap(brand.Green.FG(depth, "32"), s) }
func Dim(s string) string   { return wrap(brand.Gray.FG(depth, "90"), s) }
func Warn(s string) string  { return wrap(brand.Amber.FG(depth, "33"), s) }
func Bad(s string) string   { return wrap(brand.Red.FG(depth, "31"), s) }
func Info(s string) string  { return wrap(brand.Blue.FG(depth, "34"), s) }

// Title: заголовок раздела - жирный фирменный.
func Title(s string) string { return wrap("1;"+brand.Teal.FG(depth, "36"), s) }

// Colors: цвета включены и сколько их; для картинок вроде маскота.
func Colors() (bool, brand.Depth) { return enabled, depth }

func Printf(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
func Println(a ...any)               { _, _ = fmt.Fprintln(out, a...) }

func Errorf(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}

// Pad добивает строку пробелами до width для таблиц.
func Pad(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	b := make([]rune, width-n)
	for i := range b {
		b[i] = ' '
	}
	return s + string(b)
}

// ShortErr: последняя, самая содержательная часть обёрнутой ошибки.
func ShortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}
