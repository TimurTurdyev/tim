package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// ErrCanceled: человек закрыл выбор - команда тихо завершается.
var ErrCanceled = errors.New("выбор отменён")

// PickAlias: выбор ssh-алиаса.
func (e *Env) PickAlias() (string, error) {
	var aliases []string
	var items []tui.Item
	for _, a := range sshx.Aliases() {
		if a == "github.com" || strings.HasPrefix(a, "git.") {
			continue
		}
		t := sshx.Resolve(a)
		hint := t.User + "@" + t.Host
		aliases = append(aliases, a)
		items = append(items, tui.Item{Title: a, Hint: hint})
	}
	i, err := tui.Pick("ssh", items)
	if err != nil {
		return "", PickErr(err, "алиас", items)
	}
	return aliases[i], nil
}

// PickErr: без терминала выбирать некому - варианты печатаются в stdout, чтобы скрипт мог их взять.
func PickErr(err error, what string, items []tui.Item) error {
	if errors.Is(err, tui.ErrCanceled) {
		return ErrCanceled
	}
	if errors.Is(err, tui.ErrNoTTY) {
		for _, it := range items {
			fmt.Println(it.Title)
		}
		return ui.Errorf("выбрать не из чего без терминала: передай %s аргументом", what)
	}
	return err
}

// Option: вариант выбора - то, что видно, и подсказка серым.
type Option struct{ Title, Hint string }

// Choose: выбор одного варианта; what - как назвать его в подсказке, если выбирать некому.
func Choose(title, what string, opts []Option) (int, error) {
	items := make([]tui.Item, len(opts))
	for i, o := range opts {
		items[i] = tui.Item{Title: o.Title, Hint: o.Hint}
	}
	i, err := tui.Pick(title, items)
	if err != nil {
		return 0, PickErr(err, what, items)
	}
	return i, nil
}

// Interactive: есть терминал, где можно открыть экран или спросить выбором.
func Interactive() bool { return tui.Available() }

// ChooseMany: несколько вариантов галочками.
func ChooseMany(title string, opts []Option) ([]int, error) { return ChooseManyFrom(title, opts, nil) }

// Ask: вопрос да или нет окном в терминале.
func Ask(question string, def bool) (bool, error) { return tui.Confirm(question, def) }

// ChooseManyFrom: несколько вариантов галочками, часть отмечена заранее.
func ChooseManyFrom(title string, opts []Option, selected []bool) ([]int, error) {
	items := make([]tui.Item, len(opts))
	for i, o := range opts {
		items[i] = tui.Item{Title: o.Title, Hint: o.Hint}
	}
	return tui.MultiSelect(title, items, selected)
}

// Prompt: строка ввода с подсказкой; flag - как передать то же самое без терминала.
func Prompt(question, def, flag string) (string, error) {
	s, err := tui.Input(question, def)
	if err != nil {
		return "", PickErr(err, flag, nil)
	}
	return s, nil
}
