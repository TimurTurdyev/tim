package projects

import (
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/worklog"
)

// commitsOf: свои коммиты проекта с начала периода. Почта своя у каждого репозитория:
// рабочие и личные проекты часто коммитятся с разных адресов.
func commitsOf(dir string, since time.Time, anyone bool) []worklog.Commit {
	if !gitx.IsRepo(dir) {
		return nil
	}
	author := ""
	if !anyone {
		if author = gitx.Email(dir); author == "" {
			return nil
		}
	}
	out, err := gitx.Log(dir, since, author, worklog.Format)
	if err != nil {
		return nil
	}
	return worklog.Parse(out)
}

// today: свои коммиты по проектам с полуночи, с понедельника или с даты.
func today(env *app.Env, args []string, o todayOptions) error {
	projects, err := env.Named(args)
	if err != nil {
		return err
	}
	since := worklog.Since(time.Now(), o.week)
	if o.since != "" {
		if since, err = time.ParseInLocation("2006-01-02", o.since, time.Local); err != nil {
			return ui.Errorf("дата в виде 2025-10-01: %s", o.since)
		}
	}
	lists := par.Map(projects, par.Limit, func(p *config.Project) []worklog.Commit {
		return commitsOf(env.Cfg.ProjectDir(p), since, o.anyone)
	})
	by := map[string][]worklog.Commit{}
	order := make([]string, len(projects))
	for i, p := range projects {
		order[i] = p.Name
		by[p.Name] = lists[i]
	}
	groups := worklog.Group(by, order)
	if len(groups) == 0 {
		ui.Println(ui.Dim("с " + since.Format("2006-01-02 15:04") + " своих коммитов нет"))
		return nil
	}
	if o.md {
		printWorklogMD(groups)
		return nil
	}
	total := 0
	for _, g := range groups {
		total += len(g.Commits)
		ui.Println(ui.Label(g.Name) + ui.Dim("  "+strconv.Itoa(len(g.Commits))))
		for _, c := range g.Commits {
			ui.Println("  " + ui.Dim(c.When.Local().Format(whenLayout(since))) + "  " + c.Subject)
		}
	}
	ui.Println(ui.Dim("всего " + strconv.Itoa(total) + " в " + strconv.Itoa(len(groups)) + " проектах с " + since.Format("2006-01-02")))
	return nil
}

// whenLayout: за сегодня хватает времени, за период нужна и дата.
func whenLayout(since time.Time) string {
	if time.Since(since) <= 24*time.Hour {
		return "15:04"
	}
	return "02.01 15:04"
}

func printWorklogMD(groups []worklog.Project) {
	for i, g := range groups {
		if i > 0 {
			ui.Println()
		}
		ui.Println("**" + g.Name + "**")
		for _, c := range g.Commits {
			ui.Println("- " + c.Subject)
		}
	}
}
