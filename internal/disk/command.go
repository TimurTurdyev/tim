// Package disk: куда ушло место - обзор по местам, кеши программ, зависимости проектов,
// обозреватель по папкам и хвосты удалённых программ.
package disk

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
)

type options struct {
	minSize                                             string
	days                                                int
	all, fresh, clean, system, deps, browse, left, json bool
}

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	var o options
	c := &cobra.Command{
		Use:   "disk [папка] [--all] [--system] [--deps] [--clean] [--json]",
		Short: "Куда ушло место на диске: игры, виртуалки, файлы проектов, кеши - и как это освободить",
		Long: `Без флагов - обзор: занято и свободно, и самые большие места, раскрытые до того, что именно
там лежит (конкретная игра, виртуалка, storage проекта), с подсказкой, как освободить. Первый замер
домашней папки идёт полминуты, дальше 15 минут берётся из кэша (--fresh - заново).
--all - все места от 100 MB, а не только крупнейшие. --system - кеши программ по именам.
--deps [папка] - node_modules, vendor, venv и сборки по проектам; --clean - убрать заброшенные в Корзину.
-b (--browse) - обозреватель по папкам, как у чистильщиков: от всего диска вглубь, отметить и убрать.
--leftovers - что осталось от удалённых программ: папки в ~/Library, агенты автозапуска без своей
программы и процессы, чей файл уже удалён.
tim сам ничего не удаляет, кроме --clean, и то в Корзину и с вопросом.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return run(env, o, args) },
	}
	f := c.Flags()
	f.StringVar(&o.minSize, "min", "50M", "не показывать папки меньше")
	f.BoolVar(&o.clean, "clean", false, "предложить убрать в Корзину папки заброшенных проектов")
	f.IntVar(&o.days, "days", 60, "сколько дней без изменений считать заброшенным")
	f.BoolVar(&o.system, "system", false, "кеши программ по именам: npm, go, docker, IDE, модели")
	f.BoolVarP(&o.all, "all", "a", false, "все места от 100 MB, а не только крупнейшие")
	f.BoolVar(&o.fresh, "fresh", false, "померить заново, а не из кэша")
	f.BoolVar(&o.json, "json", false, "обзор в JSON")
	f.BoolVar(&o.deps, "deps", false, "зависимости и сборки по проектам")
	f.BoolVarP(&o.browse, "browse", "b", false, "разобрать по папкам: заходить внутрь, отмечать и убирать в Корзину")
	f.BoolVar(&o.left, "leftovers", false, "хвосты удалённых программ: папки в Library, агенты автозапуска, процессы")
	return []*cobra.Command{c}
}

func run(env *app.Env, o options, args []string) error {
	switch {
	case o.system:
		return printSystem()
	case o.left:
		return leftovers(env, o)
	case o.browse:
		return browse(env, args, o.fresh)
	case o.deps || o.clean:
		return deps(env, o, args)
	case app.Interactive() && !o.all && !o.json:
		s, err := Screen(env, args, o.fresh)
		if err != nil {
			return err
		}
		return env.Open(s)
	}
	return printOverview(env.Cfg, args, o.all, o.fresh, o.json)
}
