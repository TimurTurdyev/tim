package cmd

import (
	"errors"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// errPickCanceled: человек закрыл пикер, команда тихо завершается.
var errPickCanceled = errors.New("выбор отменён")

func pickProject() (*config.Project, error) {
	items := make([]tui.Item, len(cfg.Projects))
	for i, p := range cfg.Projects {
		items[i] = tui.Item{Title: p.Name, Hint: p.Path}
	}
	i, err := tui.Pick("проект", items)
	if err != nil {
		return nil, pickErr(err)
	}
	return &cfg.Projects[i], nil
}

func pickAlias() (string, error) {
	var aliases []string
	var items []tui.Item
	for _, a := range sshx.Aliases() {
		if a == "github.com" || strings.HasPrefix(a, "git.") {
			continue
		}
		t := sshx.Resolve(a)
		hint := t.User + "@" + t.Host
		if ps := projectsBySSH(a); len(ps) > 0 {
			hint += "  " + strings.Join(ps, " ")
		}
		aliases = append(aliases, a)
		items = append(items, tui.Item{Title: a, Hint: hint})
	}
	i, err := tui.Pick("ssh", items)
	if err != nil {
		return "", pickErr(err)
	}
	return aliases[i], nil
}

func projectsBySSH(alias string) []string {
	var out []string
	for _, p := range cfg.Projects {
		if p.SSH == alias {
			out = append(out, p.Name)
		}
	}
	return out
}

func pickErr(err error) error {
	if errors.Is(err, tui.ErrCanceled) {
		return errPickCanceled
	}
	return err
}
