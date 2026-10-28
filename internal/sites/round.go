package sites

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/fill"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/notify"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/servers"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Итог обхода - одно сообщение: только новое со степенью ✕ или ▲ с прошлого уведомления. Уведомление
// каждое утро "всё в порядке" или с теми же строками быстро приучает его не читать.

// seriousCols: колонки, где ▲ - повод написать; git - дело рабочего стола, сервер пишет свои строки.
var seriousCols = []string{"сайт", "сертификат", "почта", "домен", "секреты", "зависимости"}

// alert: строка уведомления и её ключ - по ключу понятно, что о ней уже писали, даже если числа в ней сменились.
type alert struct{ key, line string }

// roundDomains: домены проектов для наблюдения записей и выпусков сертификатов.
func roundDomains(projects []*config.Project) []string {
	var out []string
	for _, p := range projects {
		host, _ := netinfo.HostPort(p.URL, "")
		if p.URL == "" || host == "" || fill.Local(host) {
			continue
		}
		if base := inspect.Registrable(host); !slices.Contains(out, base) {
			out = append(out, base)
		}
	}
	return out
}

// watchDomains: записи и сертификаты доменов обхода - сравниваются с прошлым разом.
func watchDomains(domains []string) {
	par.Map(domains, 4, watchDomain)
}

// roundAliases: серверы проектов обхода.
func roundAliases(projects []*config.Project) []string {
	var out []string
	for _, p := range projects {
		if p.SSH != "" && !slices.Contains(out, p.SSH) {
			out = append(out, p.SSH)
		}
	}
	return out
}

func roundAlerts(rows []health.Row, domains, aliases []string) []alert {
	var out []alert
	for _, r := range rows {
		for _, c := range r.Checks {
			if c.Level == level.Bad && c.Name != "сервер" || c.Level == level.Warn && slices.Contains(seriousCols, c.Name) {
				out = append(out, alert{r.Name + "|" + c.Name + "|" + c.Text, mark(c.Level) + " " + r.Name + ": " + c.Detail})
			}
		}
	}
	for _, a := range aliases {
		for _, l := range servers.RoundLines(a) {
			key := l
			if strings.Contains(l, "не отвечает") {
				key = "miss " + a
			}
			out = append(out, alert{key, l})
		}
	}
	for _, d := range domains {
		if news := domainNews(d); len(news) > 0 {
			l := "▲ " + d + ": " + strings.Join(news, ", ")
			out = append(out, alert{l, l})
		}
	}
	return out
}

func mark(l level.Level) string {
	if l == level.Bad {
		return "✕"
	}
	return "▲"
}

// notifyRound: только то, о чём ещё не писали; написанное, которое ушло, забывается - вернётся, напишем снова.
func notifyRound(cfg *config.Config, rows []health.Row, domains, aliases []string) {
	all := roundAlerts(rows, domains, aliases)
	path := cache.Path(paths.State("notified"), "round")
	sent, _ := cache.Load[[]string](path)
	var fresh []string
	keys := make([]string, 0, len(all))
	for _, a := range all {
		keys = append(keys, a.key)
		if !slices.Contains(sent.Value, a.key) {
			fresh = append(fresh, a.line)
		}
	}
	if !runner.DryRun {
		_ = cache.Store(path, keys, 0o600)
	}
	if len(fresh) == 0 {
		return
	}
	title := fmt.Sprintf("tim · обход %s · %s", time.Now().Format("15:04"), roundScope(rows, domains, aliases))
	schedule.Notify(title, strings.Join(fresh[:min(3, len(fresh))], "\n"))
	if len(notify.Configured(cfg.Notify)) == 0 {
		return
	}
	if err := notify.Send(cfg.Notify, title, strings.Join(fresh, "\n")+"\nоткрыть: tim"); err != nil {
		ui.Println(ui.Warn("уведомление не ушло: " + err.Error()))
	}
}

func roundScope(rows []health.Row, domains, aliases []string) string {
	var parts []string
	if n := len(aliases); n > 0 {
		parts = append(parts, ui.Count(n, "сервер", "сервера", "серверов"))
	}
	if n := len(domains); n > 0 {
		parts = append(parts, ui.Count(n, "домен", "домена", "доменов"))
	}
	parts = append(parts, ui.Count(len(rows), "проект", "проекта", "проектов"))
	return strings.Join(parts, ", ")
}

// notifyStatus: куда уходит итог обхода.
func notifyStatus(cfg *config.Config) error {
	where := notify.Configured(cfg.Notify)
	if len(where) == 0 {
		ui.Println("итог обхода - только уведомлением системы; Telegram и ntfy - раздел [notify] в конфиге, пример: tim notify --help")
		return nil
	}
	ui.Println(ui.Label("итог обхода уходит: ") + strings.Join(where, ", ") + ui.Dim(" и уведомлением системы"))
	return nil
}

// notifyTest: пробное сообщение.
func notifyTest(cfg *config.Config) error {
	where := notify.Configured(cfg.Notify)
	if len(where) == 0 {
		return ui.Errorf("слать некуда: в конфиге нет [notify] с telegram и chat или ntfy")
	}
	if runner.DryRun {
		ui.Println(ui.Dim("dry-run: пробное сообщение в " + strings.Join(where, ", ") + " не отправлено"))
		return nil
	}
	if err := notify.Send(cfg.Notify, "tim · проверка уведомлений", "так будет выглядеть итог обхода\nоткрыть: tim"); err != nil {
		return ui.Errorf("не ушло: %v", err)
	}
	ui.Println(ui.Label("ушло: ") + strings.Join(where, ", "))
	return nil
}
