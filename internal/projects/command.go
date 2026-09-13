// Package projects: реестр проектов - переход, список, поиск клонов, создание, первая настройка,
// заполнение полей, команды по всем проектам и отчёт о сделанном.
package projects

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/newproj"
)

// initOptions: как собирать первый конфиг.
type initOptions struct {
	root, wrapper   string
	all, yes, merge bool
	wrapperSet      bool
}

// newOptions: из какого шаблона и куда создавать проект.
type newOptions struct {
	stack, in   string
	noGit, open bool
}

// setupOptions: куда клонировать и спрашивать ли.
type setupOptions struct {
	yes      bool
	in, name string
}

// syncOptions: что заполнять в реестре.
type syncOptions struct{ force, ssh, remote bool }

// eachOptions: где и как выполнять задачу.
type eachOptions struct {
	list, all, scan, verbose bool
	marker                   string
}

// todayOptions: за какой период и чьи коммиты.
type todayOptions struct {
	week, md, anyone bool
	since            string
}

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{projCommand(env), goCommand(env), configCommand(env), newCommand(env), setupCommand(env),
		syncCommand(env), eachCommand(env), todayCommand(env)}
}

func projCommand(env *app.Env) *cobra.Command {
	c := &cobra.Command{Use: "proj", Short: "Реестр проектов"}
	var addAll bool
	scanCmd := &cobra.Command{
		Use:   "scan [--add]",
		Short: "Найти git-клоны под корнем, которых нет в реестре",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return scanRegistry(env, addAll) },
	}
	scanCmd.Flags().BoolVar(&addAll, "add", false, "добавить все найденные")
	var name, url string
	add := &cobra.Command{
		Use:   "add [путь] [--name имя] [--url адрес]",
		Short: "Добавить папку в реестр (по умолчанию текущую)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return addProject(env, app.First(args), name, url) },
	}
	add.Flags().StringVar(&name, "name", "", "имя в реестре")
	add.Flags().StringVar(&url, "url", "", "адрес сайта проекта (для tim check и tim cert)")
	c.AddCommand(&cobra.Command{
		Use:   "list [группа]",
		Short: "Список проектов, можно по началу имени или пути",
		Run:   func(cmd *cobra.Command, args []string) { printProjects(env.Cfg, app.First(args)) },
	}, &cobra.Command{
		Use:               "info [имя]",
		Short:             "Путь, стек, ssh, деплой, порт, база, ветка",
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return printInfo(env, app.First(args)) },
	}, &cobra.Command{
		Use:   "here",
		Short: "Имя проекта для текущей папки",
		RunE:  func(cmd *cobra.Command, args []string) error { return printHere(env.Cfg) },
	}, scanCmd, add, removeCommand(env))
	return c
}

func removeCommand(env *app.Env) *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:               "rm <имя> [-y]",
		Short:             "Убрать проект из реестра (папка остаётся)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return removeProject(env, args[0], yes) },
	}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "не спрашивать подтверждения")
	return c
}

func goCommand(env *app.Env) *cobra.Command {
	var printOnly bool
	c := &cobra.Command{
		Use:               "go [имя] [.]",
		Short:             "Перейти в проект; с точкой в папку над клоном, если задан wrapper",
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return goTo(env, args, printOnly) },
	}
	c.Flags().BoolVar(&printOnly, "print", false, "только напечатать путь (для шима)")
	return c
}

func configCommand(env *app.Env) *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Где лежит конфиг и что в нём",
		RunE:  func(cmd *cobra.Command, args []string) error { return printConfig(env.Cfg) },
	}
	var o initOptions
	initCmd := &cobra.Command{
		Use:   "init [--root папка] [--wrapper app] [--all] [--merge] [-y]",
		Short: "Мастер первого конфига: корень проектов, поиск клонов, реестр",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			o.wrapperSet = cmd.Flags().Changed("wrapper")
			return initConfig(env, o)
		},
	}
	f := initCmd.Flags()
	f.StringVar(&o.root, "root", "", "корень проектов")
	f.StringVar(&o.wrapper, "wrapper", "", "подпапка с клоном внутри папки проекта, например app")
	f.BoolVar(&o.all, "all", false, "добавить все найденные клоны")
	f.BoolVarP(&o.yes, "yes", "y", false, "без вопросов")
	f.BoolVar(&o.merge, "merge", false, "дополнить существующий конфиг")
	c.AddCommand(initCmd)
	return c
}

func newCommand(env *app.Env) *cobra.Command {
	var o newOptions
	c := &cobra.Command{
		Use:   "new <имя> [--stack laravel|php|go|node|vite|python|empty|свой] [--in папка] [--no-git] [--open]",
		Short: "Создать проект по шаблону стека, git init и запись в реестр",
		Long: `Папка: <корень>/<in>/<имя>, а при заданном wrapper клон в <корень>/<in>/<имя>/<wrapper>.
Свои шаблоны - [[template]] в конфиге: name, steps (команды sh, подстановки {dir} {name} {parent}),
port = true, если проекту нужен порт для serve. Свой шаблон с именем встроенного его заменяет.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return newProject(env, args[0], o) },
	}
	c.Flags().StringVar(&o.stack, "stack", "empty", "шаблон: "+strings.Join(newproj.Stacks(), ", ")+" или свой из конфига")
	c.Flags().StringVar(&o.in, "in", "projects", "папка внутри корня проектов")
	c.Flags().BoolVar(&o.noGit, "no-git", false, "без git init")
	c.Flags().BoolVar(&o.open, "open", false, "открыть в редакторе после создания")
	_ = c.RegisterFlagCompletionFunc("stack", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		names := newproj.Stacks()
		for _, t := range env.Cfg.Templates {
			names = append(names, t.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
	return c
}

func setupCommand(env *app.Env) *cobra.Command {
	var o setupOptions
	c := &cobra.Command{
		Use:   "setup [проект|адрес репозитория]",
		Short: "От клона до работающего локально: зависимости, .env, ключ, база",
		Long: `Разбирает, чего проекту не хватает, и доделывает: копирует .env из примера, ставит
зависимости php, js, go и python, у Laravel генерирует ключ, создаёт файл базы sqlite,
накатывает миграции и ставит ссылку на хранилище. Уже сделанное не повторяется.
Адрес репозитория вместо имени сначала клонирует его по раскладке реестра и добавляет проект.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return setupProject(env, app.First(args), o) },
	}
	c.Flags().BoolVarP(&o.yes, "yes", "y", false, "не спрашивать подтверждения")
	c.Flags().StringVar(&o.in, "in", "projects", "папка внутри корня проектов при клонировании")
	c.Flags().StringVar(&o.name, "name", "", "имя проекта при клонировании")
	return c
}

func syncCommand(env *app.Env) *cobra.Command {
	var o syncOptions
	c := &cobra.Command{
		Use:   "sync [проект...] [--remote] [--ssh] [--force]",
		Short: "Заполнить поля реестра тем, что известно из самих проектов",
		Long: `Берёт из .env проекта адрес сайта (APP_URL), порт и базу и дописывает их в реестр.
Локальные адреса (localhost, .test) адресом сайта не считаются. По умолчанию заполняются только
пустые поля. С --ssh у проектов с адресом сайта определяется ssh-алиас: хост резолвится в IP
и сравнивается с адресами алиасов из ~/.ssh/config.
С --remote домен берётся у самого сервера: читаются конфиги nginx и сопоставляются с проектом
по пути деплоя или по имени. Это только чтение, ничего на сервере не меняется.`,
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return syncRegistry(env, args, o) },
	}
	c.Flags().BoolVar(&o.force, "force", false, "перезаписывать и непустые поля")
	c.Flags().BoolVar(&o.remote, "remote", false, "спросить домены у серверов по ssh (конфиги nginx)")
	c.Flags().BoolVar(&o.ssh, "ssh", false, "определить ssh-алиас по адресу сайта")
	return c
}

func eachCommand(env *app.Env) *cobra.Command {
	var o eachOptions
	c := &cobra.Command{
		Use:   "each [задача] [проект...] [--list] [--all] [--scan] | each --marker файл -- команда",
		Short: "Выполнить команду во всех проектах, где есть файл-маркер; задачи - [[task]] в конфиге",
		Long: `Задача в конфиге:

  [[task]]
  name = "deps"
  marker = "composer.json"      # где выполнять
  run = "composer outdated --direct"
  version_key = "version"       # необязательно: ключ версии в JSON-маркере
  version_cmd = "tool --version" # и с чем сравнивать; тогда по умолчанию только устаревшие

Без задачи - список задач. Разово: tim each --marker go.mod -- go mod tidy.`,
		RunE: func(cmd *cobra.Command, args []string) error { return runEach(env, args, cmd.ArgsLenAtDash(), o) },
	}
	f := c.Flags()
	f.BoolVar(&o.list, "list", false, "только показать, где и в каких версиях")
	f.BoolVar(&o.all, "all", false, "во всех, даже свежих")
	f.BoolVar(&o.scan, "scan", false, "искать и вне реестра, под корнем проектов")
	f.StringVar(&o.marker, "marker", "", "разовый запуск: файл-маркер, команда после --")
	f.BoolVarP(&o.verbose, "verbose", "v", false, "печатать вывод команды")
	return c
}

func todayCommand(env *app.Env) *cobra.Command {
	var o todayOptions
	c := &cobra.Command{
		Use:   "today [проект...] [--week] [--since 2025-10-01] [--md]",
		Short: "Что сделано сегодня или за неделю: свои коммиты по всем проектам",
		Long: `Собирает свои коммиты по всем проектам реестра (или по названным) с полуночи,
с понедельника (--week) или с даты (--since). Автор - user.email каждого репозитория.
--md печатает список для вставки в отчёт или письмо.`,
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return today(env, args, o) },
	}
	c.Flags().BoolVar(&o.week, "week", false, "с понедельника этой недели")
	c.Flags().StringVar(&o.since, "since", "", "с даты, например 2025-10-01")
	c.Flags().BoolVar(&o.md, "md", false, "списком для отчёта")
	c.Flags().BoolVar(&o.anyone, "anyone", false, "коммиты всех авторов, а не только свои")
	return c
}
