package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// errPickCanceled: человек закрыл пикер, команда тихо завершается.
var errPickCanceled = errors.New("выбор отменён")

func pickProject() (*config.Project, error) {
	if len(cfg.Projects) == 0 {
		return nil, ui.Errorf("реестр пуст: tim config init или tim proj scan")
	}
	names := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		names[i] = p.Name
	}
	// Недавние сверху: без запроса это порядок списка, с запросом - порядок при равных очках.
	order := visits.Order(names, visits.Load(), time.Now())
	items := make([]tui.Item, len(order))
	for k, i := range order {
		items[k] = tui.Item{Title: cfg.Projects[i].Name, Hint: cfg.Projects[i].Path}
	}
	k, err := tui.Pick("проект", items)
	if err != nil {
		return nil, pickErr(err, "проект", items)
	}
	return &cfg.Projects[order[k]], nil
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
		return "", pickErr(err, "алиас", items)
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

// pickErr: без терминала выбирать некому - печатаем варианты в stdout, чтобы скрипт мог их взять.
func pickErr(err error, what string, items []tui.Item) error {
	if errors.Is(err, tui.ErrCanceled) {
		return errPickCanceled
	}
	if errors.Is(err, tui.ErrNoTTY) {
		for _, it := range items {
			fmt.Println(it.Title)
		}
		return ui.Errorf("выбрать не из чего без терминала: передай %s аргументом", what)
	}
	return err
}
