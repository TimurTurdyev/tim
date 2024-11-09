package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/fill"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/registry"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	checkLocal bool
	checkNet   bool
	checkHours float64
)

// checkProjects: проекты для обхода. Без аргументов - весь реестр.
func checkProjects(args []string) ([]*config.Project, error) {
	if len(args) == 0 {
		out := make([]*config.Project, 0, len(cfg.Projects))
		for i := range cfg.Projects {
			out = append(out, &cfg.Projects[i])
		}
		return out, nil
	}
	var out []*config.Project
	for _, name := range args {
		p, err := registry.Pick(cfg, name)
		if err != nil {
			// Домен, которого нет в реестре, проверяем как есть: сайт, сертификат, почта, регистрация.
			if strings.Contains(name, ".") && !strings.Contains(name, "/") {
				out = append(out, &config.Project{Name: name, URL: "https://" + strings.TrimPrefix(strings.TrimPrefix(name, "https://"), "http://")})
				continue
			}
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func checkGit(dir string) health.Check {
	c := health.Check{Name: "git", Level: health.Skip, Text: "-"}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return c
	}
	g, ok := gitState(dir)
	if !ok {
		return c
	}
	var parts []string
	if g.Changes > 0 {
		parts = append(parts, strconv.Itoa(g.Changes)+" изм")
	}
	if g.Ahead > 0 {
		parts = append(parts, strconv.Itoa(g.Ahead)+" не отпр")
	}
	if len(parts) == 0 {
		c.Level, c.Text = health.OK, "чисто"
		return c
	}
	// Незакоммиченное - обычное состояние работы, а вот неотправленное живёт только на этой машине.
	c.Level, c.Text = health.OK, strings.Join(parts, ", ")
	if g.Ahead > 0 {
		c.Level = health.Warn
	}
	return c
}

func checkSite(url string) health.Check {
	c := health.Check{Name: "сайт", Level: health.OK}
	page, err := inspect.HTTP(url)
	if err != nil {
		c.Level, c.Text, c.Detail = health.Bad, "нет ответа", "сайт не отвечает: "+shortErr(err)
		return c
	}
	c.Text = strconv.Itoa(page.Status)
	switch {
	case page.Status >= 500:
		c.Level, c.Detail = health.Bad, fmt.Sprintf("сайт отвечает %d", page.Status)
	case page.Status >= 400:
		c.Level, c.Detail = health.Warn, fmt.Sprintf("сайт отвечает %d", page.Status)
	}
	return c
}

func checkCert(host string) health.Check {
	info, err := inspect.Cert(host, "443")
	if err != nil {
		return health.Check{Name: "сертификат", Level: health.Bad, Text: "нет", Detail: "сертификат не получен: " + shortErr(err)}
	}
	c := health.Deadline("сертификат", info.Leaf().NotAfter, 21, 7)
	if info.VerifyErr != nil {
		c.Level = health.Bad
		c.Detail = "сертификат не проходит проверку: " + shortErr(info.VerifyErr)
	}
	return c
}

func checkMail(domain string) health.Check {
	m := inspect.MailCheck(domain, inspect.DKIMSelectors)
	level, reasons := m.Verdict()
	c := health.Check{Name: "почта"}
	switch level {
	case "bad":
		c.Level, c.Text = health.Bad, "плохо"
	case "warn":
		c.Level, c.Text = health.Warn, "неполно"
	default:
		c.Level, c.Text = health.OK, "ок"
	}
	if c.Level >= health.Warn && len(reasons) > 0 {
		c.Detail = "почта: " + strings.Join(reasons, "; ")
	}
	return c
}

func checkDomain(domain string) health.Check {
	d, err := health.Registration(domain)
	if err != nil {
		return health.Check{Name: "домен", Level: health.Skip, Text: "-"}
	}
	if d.Expires.IsZero() {
		return health.Check{Name: "домен", Level: health.Skip, Text: "-"}
	}
	c := health.Deadline("регистрация домена", d.Expires, 30, 10)
	c.Name = "домен"
	return c
}

// checkRow: все проверки одного проекта. Сетевые идут параллельно.
func checkRow(p *config.Project) health.Row {
	r := health.Row{Name: p.Name}
	var mu sync.Mutex
	add := func(c health.Check) {
		mu.Lock()
		r.Checks = append(r.Checks, c)
		mu.Unlock()
	}
	var wg sync.WaitGroup
	if !checkNet {
		add(checkGit(cfg.ProjectDir(p)))
	}
	host, _ := hostPort(p.URL, "")
	if !checkLocal && p.URL != "" && host != "" && !fill.Local(host) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(checkSite(p.URL))
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(checkCert(host))
		}()
		base := registrable(host)
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(checkMail(base))
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			add(checkDomain(base))
		}()
	}
	wg.Wait()
	return r
}

var checkCols = []struct {
	name  string
	width int
}{
	{"git", 18},
	{"сайт", 12},
	{"сертификат", 14},
	{"почта", 8},
	{"домен", 10},
}

func checkPaint(c health.Check, text string) string {
	switch c.Level {
	case health.Bad:
		return ui.Bad(text)
	case health.Warn:
		return ui.Warn(text)
	case health.Skip:
		return ui.Dim(text)
	}
	return text
}

func printCheck(rows []health.Row) {
	// Колонку, в которой нечего показать ни одному проекту, не печатаем.
	cols := checkCols[:0:0]
	for _, col := range checkCols {
		for _, r := range rows {
			if r.Get(col.name).Level != health.Skip {
				cols = append(cols, col)
				break
			}
		}
	}
	head := ui.Pad("проект", 22)
	for _, col := range cols {
		head += ui.Pad(col.name, col.width)
	}
	ui.Println(ui.Dim(head))
	for _, r := range rows {
		line := ui.Pad(r.Name, 22)
		for _, col := range cols {
			c := r.Get(col.name)
			text := clip(c.Text, col.width-1)
			line += checkPaint(c, text) + strings.Repeat(" ", col.width-len([]rune(text)))
		}
		ui.Println(strings.TrimRight(line, " "))
		for _, c := range r.Problems() {
			ui.Println("  " + ui.Dim(c.Detail))
		}
	}
}

func clip(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "~"
}

// checkMachine: замечания не о проектах, а о самой машине.
func checkMachine() []string {
	var out []string
	limit := time.Duration(checkHours * float64(time.Hour))
	var forgotten []string
	for _, r := range listenRows() {
		if r.project != "" && !strings.HasSuffix(r.project, "?") && r.Uptime >= limit {
			forgotten = append(forgotten, fmt.Sprintf("%s:%s", r.project, r.Port))
		}
	}
	if len(forgotten) > 0 {
		out = append(out, ui.Warn(fmt.Sprintf("dev-серверы старше %g ч: ", checkHours))+strings.Join(forgotten, " ")+ui.Dim("  (tim stale --kill)"))
	}
	if df, err := runner.Output("df", "-k", "/System/Volumes/Data"); err == nil {
		if d, ok := sysinfo.ParseDF(df); ok && d.Percent() >= 85 {
			out = append(out, ui.Warn(fmt.Sprintf("диск занят на %d%%", d.Percent()))+ui.Dim("  (tim disk)"))
		}
	}
	return out
}

var checkCmd = &cobra.Command{
	Use:   "check [проект...]",
	Short: "Обход хозяйства: git, сайт, сертификат, почта, срок домена по всем проектам",
	RunE: func(cmd *cobra.Command, args []string) error {
		projects, err := checkProjects(args)
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			return ui.Errorf("в реестре нет проектов: tim proj scan")
		}
		rows := make([]health.Row, len(projects))
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for i, p := range projects {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				rows[i] = checkRow(p)
			}()
		}
		wg.Wait()
		// Проекты, по которым проверить было нечего, в таблице только шумят.
		kept := rows[:0]
		for _, r := range rows {
			if r.Level() != health.Skip {
				kept = append(kept, r)
			}
		}
		rows = kept
		if len(rows) == 0 {
			return ui.Errorf("проверять нечего: ни у одного проекта нет git и адреса сайта (tim sync --remote)")
		}
		health.Sort(rows)
		printCheck(rows)
		ok, warn, bad := health.Counts(rows)
		ui.Println(ui.Dim(fmt.Sprintf("всего %d: ок %d, внимание %d", len(rows), ok, warn)))
		if lines := checkMachine(); len(lines) > 0 {
			ui.Println("")
			for _, l := range lines {
				ui.Println(l)
			}
		}
		if bad > 0 {
			return ui.Errorf("требуют внимания: %d из %d", bad, len(rows))
		}
		return nil
	},
}

func init() {
	checkCmd.Flags().BoolVar(&checkLocal, "local", false, "только локальные проверки, без сети")
	checkCmd.Flags().BoolVar(&checkNet, "net", false, "только сетевые проверки")
	checkCmd.Flags().Float64Var(&checkHours, "hours", 8, "с какого возраста dev-сервер считается забытым")
	root.AddCommand(checkCmd)
}
