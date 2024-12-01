// Package worklog: что сделано по проектам за период - разбор git log и группировка.
package worklog

import (
	"sort"
	"strings"
	"time"
)

// Format: формат git log, который понимает Parse. Разделитель полей - символ 0x1f, его не бывает в темах.
const Format = "--format=%H%x1f%aI%x1f%s"

// Commit: один коммит.
type Commit struct {
	Hash    string
	When    time.Time
	Subject string
}

// Parse разбирает вывод git log с Format. Строки с ошибками пропускаются.
func Parse(out string) []Commit {
	var list []Commit
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x1f")
		if len(f) != 3 || f[0] == "" {
			continue
		}
		when, err := time.Parse(time.RFC3339, f[1])
		if err != nil {
			continue
		}
		list = append(list, Commit{Hash: f[0], When: when, Subject: strings.TrimSpace(f[2])})
	}
	return list
}

// Project: коммиты одного проекта за период, свежие сверху.
type Project struct {
	Name    string
	Commits []Commit
}

// Group убирает коммиты, уже показанные у другого проекта (вложенные папки одного репозитория),
// и сортирует: проекты с большим числом коммитов сверху, внутри - по времени, свежие первыми.
func Group(byProject map[string][]Commit, order []string) []Project {
	seen := map[string]bool{}
	var out []Project
	for _, name := range order {
		var kept []Commit
		for _, c := range byProject[name] {
			if !seen[c.Hash] {
				seen[c.Hash] = true
				kept = append(kept, c)
			}
		}
		if len(kept) == 0 {
			continue
		}
		sort.Slice(kept, func(i, j int) bool { return kept[i].When.After(kept[j].When) })
		out = append(out, Project{Name: name, Commits: kept})
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].Commits) > len(out[j].Commits) })
	return out
}

// Since: начало периода. Сегодня - с полуночи, неделя - с понедельника этой недели.
func Since(now time.Time, week bool) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if !week {
		return day
	}
	back := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -back)
}
