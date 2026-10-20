package sites

import (
	"fmt"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/audit"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// noURLHint: у проектов редко заполнен адрес сайта - в .env обычно локальный APP_URL.
const noURLHint = "tim sync --remote найдёт домены в конфигах nginx серверов, или tim audit <домен>"

// runAudit: проверка сайта снаружи; цель - домен, url или проект, без цели - проект текущей папки.
func runAudit(env *app.Env, arg string, asJSON bool) error {
	target, err := auditTarget(env, arg)
	if err != nil {
		return err
	}
	if !asJSON {
		ui.Println(ui.Dim("проверяю " + target + ": главная, заголовки, куки и " + fmt.Sprint(len(audit.ProbePaths())) + " путей"))
	}
	r, err := audit.Run(target)
	if err != nil {
		return ui.Errorf("%s: %v", target, err)
	}
	if asJSON {
		return ui.JSON(r)
	}
	printAudit(r)
	return nil
}

// auditTarget: адрес сайта по аргументу. Домен без схемы - https.
func auditTarget(env *app.Env, arg string) (string, error) {
	switch {
	case strings.Contains(arg, "://"):
		return arg, nil
	case arg == "":
		p, err := env.Project("")
		if err != nil {
			return "", err
		}
		return projectURL(p)
	}
	if p, ok := env.Cfg.Project(arg); ok {
		return projectURL(p)
	}
	if strings.Contains(arg, ".") {
		return "https://" + arg, nil
	}
	like := env.Cfg.ProjectsLike(arg)
	if len(like) == 1 {
		p, _ := env.Cfg.Project(like[0])
		return projectURL(p)
	}
	if len(like) == 0 {
		return "", ui.Errorf("%s - не домен и не проект: tim audit example.ru", arg)
	}
	return "", ui.Errorf("%s - не проект, похожие: %s; выбери один или проверь домен: tim audit <домен>", arg, strings.Join(like, ", "))
}

func projectURL(p *config.Project) (string, error) {
	if p.URL == "" {
		return "", ui.Errorf("у проекта %s нет адреса сайта: %s", p.Name, noURLHint)
	}
	return p.URL, nil
}

// printAudit: вердикт строкой, потом находки - худшие сверху, у каждой почему и что сделать.
func printAudit(r *audit.Report) {
	ui.Println(ui.Label(r.Final) + "  " + level.Lamp(r.Verdict(), verdictText(r)))
	// В трубе ширины нет - строки не переносятся, их перенесёт то, что читает.
	width := 1 << 20
	if cols := ui.Cols(); cols > 0 {
		width = max(60, cols-8)
	}
	for _, f := range r.Findings {
		ui.Println("  " + level.Lamp(f.Level, ui.Pad(f.Area, 11)+f.Text))
		for _, part := range [][2]string{{"почему: ", f.Why}, {"что сделать: ", f.Fix}} {
			if part[1] == "" {
				continue
			}
			for _, l := range ui.Wrap(part[0]+part[1], width) {
				ui.Println(ui.Dim("      " + l))
			}
		}
	}
	ui.Println(ui.Dim("проверено: " + strings.Join(r.Checked, ", ")))
}

func verdictText(r *audit.Report) string {
	var bad, warn int
	for _, f := range r.Findings {
		switch f.Level {
		case level.Bad:
			bad++
		case level.Warn:
			warn++
		}
	}
	switch {
	case bad > 0:
		return fmt.Sprintf("плохо: %d %s", bad, ui.Plural(bad, "серьёзная находка", "серьёзные находки", "серьёзных находок"))
	case warn > 0:
		return fmt.Sprintf("есть что поправить: %d", warn)
	}
	return "в порядке"
}
