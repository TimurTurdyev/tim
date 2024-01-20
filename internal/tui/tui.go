// Package tui: интерактивный выбор и подтверждение; единственное место, где живёт charm.
package tui

import (
	"errors"
	"os"

	"charm.land/huh/v2"
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
	tty, err := openTTY()
	if err != nil {
		return false, ErrNoTTY
	}
	defer func() { _ = tty.Close() }()
	yes := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(question).Affirmative("да").Negative("нет").Value(&yes),
	)).WithInput(tty).WithOutput(tty).WithShowHelp(false)
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, ErrCanceled
		}
		return false, err
	}
	return yes, nil
}
