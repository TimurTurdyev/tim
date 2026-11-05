// Package view: отчёт report.Report в терминал или в JSON. Раскладка одна: её печатают команды и рисует
// окно результата в экранах tim, поэтому вид правится в одном месте.
package view

import (
	"bytes"
	"encoding/json"
	"os"
	"sync"

	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/report"
)

var (
	catchMu  sync.Mutex
	catching bool
	caught   *report.Report
)

// Show: отчёт - весь результат команды. Внутри экрана его забирает окно результата, иначе - печать.
func Show(r report.Report, asJSON bool) error {
	if asJSON {
		return JSON(r)
	}
	if take(r) {
		return nil
	}
	Print(r)
	return nil
}

// Print: отчёт текстом в общий вывод - когда после него команда спрашивает или печатает что-то ещё.
func Print(r report.Report) {
	for _, l := range Lines(r, Layout{Width: ui.Cols()}) {
		ui.Println(l)
	}
}

// JSON: Data отчёта с отступами; у объекта первым полем идёт schema.
func JSON(r report.Report) error {
	raw, err := json.Marshal(r.Data)
	if err != nil {
		return err
	}
	if r.Schema != "" && len(raw) > 1 && raw[0] == '{' {
		schema, _ := json.Marshal(r.Schema)
		sep := ","
		if string(raw) == "{}" {
			sep = ""
		}
		raw = append([]byte(`{"schema":`+string(schema)+sep), raw[1:]...)
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", "  "); err != nil {
		return err
	}
	b.WriteByte('\n')
	_, err = os.Stdout.Write(b.Bytes())
	return err
}

// Capture выполняет f, как ui.Capture, и отдаёт отдельно отчёт, показанный через Show.
func Capture(f func()) (string, *report.Report) {
	var got *report.Report
	out := ui.Capture(func() {
		catchMu.Lock()
		catching, caught = true, nil
		catchMu.Unlock()
		defer func() {
			catchMu.Lock()
			got, catching, caught = caught, false, nil
			catchMu.Unlock()
		}()
		f()
	})
	return out, got
}

// take: отчёт уходит экрану, если команда идёт внутри него; второй отчёт той же команды дописывается.
func take(r report.Report) bool {
	catchMu.Lock()
	defer catchMu.Unlock()
	if !catching {
		return false
	}
	if caught == nil {
		caught = &r
		return true
	}
	if r.Title != "" {
		caught.Blocks = append(caught.Blocks, report.Heading(r.Title))
	}
	caught.Blocks = append(caught.Blocks, r.Blocks...)
	return true
}
