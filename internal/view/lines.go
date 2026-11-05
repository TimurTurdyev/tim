package view

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/report"
)

const (
	keyMax  = 14 // ключи длиннее не растягивают колонку значений
	areaMax = 12
	minWrap = 30
)

// Layout: Width - ширина строки, 0 - не переносить (вывод в трубу, его перенесёт читатель);
// SortBy - колонка таблиц для сортировки с 1, 0 - порядок отчёта; Runnable - экран сам запускает
// команды tim из шагов "дальше", в теле их не повторять.
type Layout struct {
	Width    int
	SortBy   int
	Runnable bool
}

// Lines: отчёт строками с цветами. Под заголовком и под Heading блоки сдвинуты на два пробела.
func Lines(r report.Report, l Layout) []string {
	var out []string
	indent := ""
	if r.Title != "" {
		head := ui.Label(r.Title)
		if r.Verdict.Text != "" {
			head += "  " + level.Lamp(r.Verdict.Level, r.Verdict.Text)
		}
		out = append(out, head)
		indent = "  "
	}
	for _, b := range r.Blocks {
		switch b := b.(type) {
		case report.Heading:
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, ui.Label(string(b)))
			indent = "  "
		case report.Facts:
			out = append(out, facts(b, indent, l.Width)...)
		case report.Table:
			out = append(out, table(b, indent, l.SortBy)...)
		case report.Findings:
			out = append(out, findings(b, indent, l.Width)...)
		case report.Note:
			out = append(out, muted(indent, string(b), l.Width)...)
		case report.Next:
			for _, s := range b {
				if !l.Runnable || !strings.HasPrefix(s.Command, "tim ") {
					out = append(out, muted(indent, stepText(s), l.Width)...)
				}
			}
		}
	}
	return out
}

func facts(list report.Facts, indent string, width int) []string {
	w := 0
	for _, f := range list {
		w = max(w, ui.Width(f.Key))
	}
	w = min(w, keyMax)
	pad := indent + strings.Repeat(" ", w+2)
	var out []string
	for _, f := range list {
		for i, part := range wrap(f.Value, width-len(pad)) {
			head := pad
			if i == 0 {
				head = indent + ui.Dim(ui.Pad(f.Key, w)) + "  "
			}
			out = append(out, head+paintFact(f.Level, part))
		}
	}
	return out
}

func paintFact(l report.Level, s string) string {
	if l == report.Info {
		return ui.Dim(s)
	}
	return level.Paint(l, s)
}

// findings: значок и область, текст; под ним почему и что сделать - мельче и с отступом.
func findings(list report.Findings, indent string, width int) []string {
	w := 0
	for _, f := range list {
		w = max(w, ui.Width(f.Area))
	}
	w = min(w, areaMax) + 2
	hang := indent + strings.Repeat(" ", w+2)
	sub := indent + "    "
	var out []string
	for _, f := range list {
		for i, part := range wrap(f.Text, width-len(hang)) {
			if i == 0 {
				out = append(out, indent+level.Lamp(f.Level, ui.Pad(f.Area, w)+part))
				continue
			}
			out = append(out, hang+level.Paint(f.Level, part))
		}
		for _, p := range [][2]string{{"почему: ", f.Why}, {"что сделать: ", f.Fix}} {
			if p[1] != "" {
				out = append(out, muted(sub, p[0]+p[1], width)...)
			}
		}
	}
	return out
}

func stepText(s report.Step) string {
	if s.Text == "" {
		return s.Command
	}
	return s.Text + ": " + s.Command
}

func muted(indent, s string, width int) []string {
	var out []string
	for _, part := range wrap(s, width-len(indent)) {
		out = append(out, ui.Dim(indent+part))
	}
	return out
}

// wrap: без ширины строка остаётся целой; узкое окно переносит не уже minWrap.
func wrap(s string, width int) []string {
	if width <= 0 || ui.Width(s) <= width {
		return []string{s}
	}
	return ui.Wrap(s, max(minWrap, width))
}

// table: колонки по самому длинному значению; числа вправо; пустая ячейка - пустая, а не прочерк.
func table(t report.Table, indent string, sortBy int) []string {
	rows := sortRows(t, sortBy)
	grid := make([][]string, 0, len(rows)+1)
	head := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		head[i] = c.Title
	}
	grid = append(grid, head)
	for _, r := range rows {
		line := make([]string, len(t.Columns))
		for i, c := range t.Columns {
			if i < len(r) {
				line[i] = cell(c, r[i])
			}
		}
		grid = append(grid, line)
	}
	widths := make([]int, len(t.Columns))
	for _, line := range grid {
		for i, c := range line {
			widths[i] = max(widths[i], ui.Width(c))
		}
	}
	out := make([]string, 0, len(grid))
	for n, line := range grid {
		out = append(out, indent+tableLine(t.Columns, line, widths, n == 0))
	}
	return out
}

func tableLine(cols []report.Column, line []string, widths []int, header bool) string {
	cells := make([]string, len(line))
	for i, c := range line {
		right := cols[i].Kind != report.Text
		switch {
		case right:
			c = strings.Repeat(" ", max(0, widths[i]-ui.Width(c))) + c
		case i < len(line)-1:
			c = ui.Pad(c, widths[i])
		}
		switch {
		case header || cols[i].Style == report.Muted:
			c = ui.Dim(c)
		case cols[i].Style == report.Key:
			c = ui.Label(c)
		}
		cells[i] = c
	}
	return strings.TrimRight(strings.Join(cells, "  "), " ")
}

// cell: значение ячейки словами; ноль числа или длительности - пусто.
func cell(c report.Column, v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Duration:
		if x == 0 {
			return ""
		}
		return ui.Short(x)
	case int:
		return number(c, int64(x))
	case int64:
		return number(c, x)
	}
	return fmt.Sprint(v)
}

func number(c report.Column, n int64) string {
	switch {
	case n == 0:
		return ""
	case c.Kind == report.Bytes:
		return ui.Bytes(n)
	}
	return strconv.FormatInt(n, 10)
}

// sortRows: текст по алфавиту, числа, байты и длительности - большие сверху; равные - как в отчёте.
func sortRows(t report.Table, sortBy int) [][]any {
	rows := slices.Clone(t.Rows)
	col := sortBy - 1
	if col < 0 || col >= len(t.Columns) {
		return rows
	}
	slices.SortStableFunc(rows, func(a, b []any) int {
		x, y := at(a, col), at(b, col)
		if t.Columns[col].Kind == report.Text {
			return natural(fmt.Sprint(x), fmt.Sprint(y))
		}
		return cmp.Compare(amount(y), amount(x))
	})
	return rows
}

// natural: строки с числом в начале - по числу (порт 5000 раньше 47812), остальное - по алфавиту.
func natural(a, b string) int {
	na, okA := leadingNumber(a)
	nb, okB := leadingNumber(b)
	if okA && okB && na != nb {
		return cmp.Compare(na, nb)
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

func leadingNumber(s string) (int, bool) {
	end := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(s)
	}
	n, err := strconv.Atoi(s[:end])
	return n, err == nil
}

func at(row []any, i int) any {
	if i < len(row) {
		return row[i]
	}
	return nil
}

func amount(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case time.Duration:
		return int64(x)
	}
	return 0
}
