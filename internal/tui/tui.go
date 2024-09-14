// Package tui: интерактивный выбор и подтверждение; единственное место, где живёт charm.
package tui

import (
	"errors"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/term"
)

var (
	ErrCanceled = errors.New("отменено")
	ErrNoTTY    = errors.New("нет терминала")
)

type Item struct {
	Title string
	Hint  string
}

// Available: можно спросить человека. stdout может быть перехвачен шимом, поэтому смотрим только stdin.
func Available() bool {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	f, err := openTTY()
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// Рисуем в /dev/tty, чтобы работать внутри $(tim go --print).
func openTTY() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

func Confirm(question string) (bool, error) {
	yes := false
	err := run(huh.NewConfirm().Title(question).Affirmative("да").Negative("нет").Value(&yes))
	return yes, err
}

// Input спрашивает строку; value - значение по умолчанию.
func Input(title, value string) (string, error) {
	err := run(huh.NewInput().Title(title).Value(&value))
	return value, err
}

// MultiSelect возвращает индексы отмеченных элементов; selected - отмеченные заранее.
func MultiSelect(title string, items []Item, selected []bool) ([]int, error) {
	opts := make([]huh.Option[int], len(items))
	for i, it := range items {
		label := it.Title
		if it.Hint != "" {
			label += "  " + it.Hint
		}
		opts[i] = huh.NewOption(label, i).Selected(i < len(selected) && selected[i])
	}
	var picked []int
	err := run(huh.NewMultiSelect[int]().Title(title).Options(opts...).Height(min(len(items)+2, 18)).Value(&picked))
	return picked, err
}

func run(field huh.Field) error {
	tty, err := openTTY()
	if err != nil {
		return ErrNoTTY
	}
	defer func() { _ = tty.Close() }()
	form := huh.NewForm(huh.NewGroup(field)).WithInput(tty).WithOutput(tty).WithShowHelp(false)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return ErrCanceled
		}
		return err
	}
	return nil
}
