package servers

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Глубокая проверка читает файлы сайтов и системы - минуту-две, поэтому только по c или --deep. Итог
// хранится отдельно от обычного отчёта и показывается с давностью.

func deepPath(target string) string { return cache.Path(paths.State("servers-deep"), hostKey(target)) }

// runDeep: глубокая проверка и её итог в кэш; SUID-файлы сравниваются с прошлым разом.
func runDeep(target string) (srvinfo.Deep, error) {
	d, err := srvinfo.CollectDeep(target)
	if err != nil {
		return d, err
	}
	if !runner.DryRun {
		_ = cache.Store(deepPath(target), d, 0o644)
	}
	if d.NoSudo {
		return d, nil
	}
	items := []baseline.Item{}
	for _, f := range d.SUID {
		items = append(items, baseline.Item{Key: f})
	}
	baseline.Observe(object(target), baseline.Snapshot{"suid": items}, time.Now())
	return d, nil
}

// savedDeep: последняя глубокая проверка.
func savedDeep(target string) (srvinfo.Deep, bool) {
	e, ok := cache.Load[srvinfo.Deep](deepPath(target))
	return e.Value, ok
}

// deepFindings: находки последней глубокой проверки; не было - одна строка "не проверено".
func deepFindings(target string) []health.Check {
	d, ok := savedDeep(target)
	if !ok {
		return []health.Check{{Name: "файлы сайтов", Level: level.Skip, Text: "не проверено",
			Detail: "веб-оболочки и подмена системных файлов - c проверить, около минуты"}}
	}
	return srvinfo.EvaluateDeep(d)
}

// deepAge: "глубокая проверка 3 дн назад" для строки ламп.
func deepAge(target string) string {
	d, ok := savedDeep(target)
	if !ok {
		return ""
	}
	return "глубокая проверка " + ui.Short(time.Since(d.At)) + " назад"
}

// printDeep: tim server <алиас> --deep.
func printDeep(target string) error {
	ui.Println(ui.Dim("глубокая проверка " + target + ": SUID, файлы сайтов, файлы пакетов - около минуты, только чтение"))
	d, err := runDeep(target)
	if err != nil {
		return ui.Errorf("%s: %v; маршрут и вход проверит tim ssh %s", target, err, target)
	}
	printFindings(srvinfo.EvaluateDeep(d))
	printNew(target, "")
	return nil
}
