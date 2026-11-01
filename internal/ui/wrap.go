package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// wrapBreaks: где переносить, по убыванию предпочтения; keep - сколько клеток разделителя остаётся
// в конце строки: запятая остаётся, точка-разделитель и пробелы пропадают.
var wrapBreaks = []struct {
	sep  string
	keep int
}{{sep, 0}, {", ", 1}, {"  ", 0}, {" ", 0}}

// WrapHanging: строка не шире width, продолжения - с висячим отступом hang, под значением после подписи.
// Переносит между частями: по точке-разделителю, иначе после запятой или по двойному пробелу, иначе
// по пробелу; в первых hang колонках первой строки не переносит - там подпись. Цвет на продолжениях
// тот же. width 0 - не переносить.
func WrapHanging(s string, width, hang int) []string {
	if width <= 0 || width-hang < 10 || Width(s) <= width {
		return []string{s}
	}
	plain := StripANSI(s)
	total := Width(plain)
	pad := strings.Repeat(" ", hang)
	var out []string
	start, end, prefix := 0, width, ""
	for total-start > end-start {
		cut, next := breakAt(plain, start, end, hang)
		out = append(out, prefix+ansi.Cut(s, start, cut)+"\x1b[0m")
		start, prefix = next, pad
		end = start + width - hang
	}
	return append(out, prefix+ansi.Cut(s, start, total))
}

// breakAt: клетка конца строки и клетка начала следующей; строка - с from, не дальше to.
func breakAt(plain string, from, to, hang int) (cut, next int) {
	for _, b := range wrapBreaks {
		best := -1
		for i := 0; ; i += len(b.sep) {
			j := strings.Index(plain[i:], b.sep)
			if j < 0 {
				break
			}
			i += j
			p := Width(plain[:i])
			if p+b.keep > to {
				break
			}
			if p > from && p > hang {
				best = p
			}
		}
		if best > 0 {
			return best + b.keep, best + Width(b.sep)
		}
	}
	return to, to
}
