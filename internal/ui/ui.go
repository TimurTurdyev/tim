// Package ui: цвета и короткие сообщения в терминал.
package ui

import (
	"fmt"
	"os"
)

var enabled = isTerminal(os.Stdout)

func isTerminal(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
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

func Label(s string) string { return wrap("32", s) }
func Dim(s string) string   { return wrap("90", s) }
func Warn(s string) string  { return wrap("33", s) }
func Bad(s string) string   { return wrap("31", s) }
func Info(s string) string  { return wrap("36", s) }

func Printf(format string, a ...any) { fmt.Printf(format, a...) }
func Println(a ...any)               { fmt.Println(a...) }

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
