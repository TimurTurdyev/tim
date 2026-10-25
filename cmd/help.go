package cmd

import (
	"regexp"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const usageTemplate = `{{h "Использование"}}{{if .Runnable}}
  {{useline .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} {{dim "[команда]"}}{{end}}{{if gt (len .Aliases) 0}}

{{h "Другие имена"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{h "Примеры"}}
{{examples .Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{h "Команды"}}{{range $cmds}}{{if .IsAvailableCommand}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{h .Title}}{{if eq .ID "util"}}
  {{utilLine}}{{else}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) .IsAvailableCommand)}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{h "Ещё"}}{{range $cmds}}{{if (and (eq .GroupID "") .IsAvailableCommand)}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{h "Флаги"}}
{{flags (.LocalFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasAvailableInheritedFlags}}

{{h "Общие флаги"}}
{{flags (.InheritedFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if not .HasParent}}

{{h "Соглашения"}}
  up/down - туннели и маршруты, on/off - переключатели, start/stop - службы, kill - процессы
  -y отвечает "да" на все вопросы, --no-input - "нет"; --dry-run печатает команды, ничего не делая
  сроки пишутся как 30m, 24h, 3d, 1w; --since принимает и дату 2025-10-01
  у отчётов есть --json - для скриптов и агентов{{end}}{{if .HasAvailableSubCommands}}

{{dim (print "Подробнее о команде: " .CommandPath " <команда> --help")}}{{end}}
`

// helpOnce: русская справка настраивается один раз за запуск.
var helpOnce sync.Once

var examples = map[string]string{
	"tim go":          "  tim go                     # выбор проекта из реестра и переход\n  tim go shop .              # в папку над клоном",
	"tim proj":        "  tim proj list clients\n  tim proj scan --add\n  tim proj add . --name shop --url https://shop.example.com",
	"tim serve":       "  tim serve                  # проект текущей папки\n  tim serve shop             # по части имени\n  tim serve shop -d          # в фоне\n  tim serve @work            # группа из [groups]\n  tim serve list\n  tim serve stop all",
	"tim ssh":         "  tim ssh                    # выбор алиаса\n  tim ssh prod uptime\n  tim ssh shop               # по ssh-полю проекта",
	"tim direct":      "  tim direct example.org     # сайт целиком: плеер, видео, счётчики\n  tim direct Firefox         # всё приложение\n  tim direct git.example.com --label git\n  tim direct example.org --owner   # все сети владельца\n  tim direct                 # что сейчас идёт напрямую\n  tim direct rm example.org\n  tim direct off             # снять правила приложений",
	"tim route":       "  tim route                  # цели и их маршруты\n  tim route fix              # после смены сети\n  sudo tim route install     # страж: чинит обход сам\n  tim route status",
	"tim vpn":         "  tim vpn                    # туннель, страж и цели одним экраном",
	"tim tunnel":      "  tim tunnel up prod          # SOCKS на 127.0.0.1:1080\n  tim tunnel all prod         # весь трафик через ssh\n  tim tunnel port 15432:5432 prod\n  tim tunnel db shop\n  tim tunnel expose 8000",
	"tim db":          "  tim db                     # клиент по .env проекта\n  tim db shop dump\n  tim db shop --via prod",
	"tim status":      "  tim status\n  tim status repos versions\n  tim status --all --time",
	"tim site":        "  tim site example.com\n  tim site https://shop.example.com --json",
	"tim audit":       "  tim audit example.com\n  tim audit shop          # проект с адресом сайта в реестре\n  tim audit example.com --json",
	"tim mail":        "  tim mail example.com\n  tim mail example.com --selector s2024",
	"tim http":        "  tim http example.com\n  tim http -L http://example.com",
	"tim each":        "  tim each                   # задачи из конфига\n  tim each deps --list\n  tim each --marker go.mod -- go mod tidy",
	"tim new":         "  tim new shop --stack laravel --in clients\n  tim new tool --stack go\n  tim new site --stack static   # свой шаблон из конфига",
	"tim doctor":      "  tim doctor",
	"tim host":        "  tim host                   # записи tim в /etc/hosts\n  tim host add shop.test --profile dev\n  tim host off dev           # выключить профиль, не удаляя\n  tim host resolver test     # вся зона *.test на 127.0.0.1",
	"tim cert":        "  tim cert example.com       # чужой сертификат\n  tim cert new shop.test     # свой, для локальной разработки\n  tim cert ca install        # чтобы браузер ему доверял",
	"tim env":         "  tim env shop\n  tim env diff shop          # против .env.example\n  tim env diff shop --server # состав ключей против сервера",
	"tim kill":        "  tim kill chrome            # Cmd+Q, не помогло - спросит про принудительно\n  tim kill goglehrome        # опечатки прощаются\n  tim kill 4242              # по номеру\n  tim kill node --force      # сразу SIGKILL",
	"tim hw":          "  tim hw                     # паспорт машины и загрузка\n  tim hw --watch             # обновлять раз в секунду",
	"tim ps":          "  tim ps chrome              # что запущено и как закрыть",
	"tim today":       "  tim today                  # свои коммиты за сегодня\n  tim today --week\n  tim today shop --since 2025-10-01 --md",
	"tim link":        "  tim link shop              # shop.test: hosts, сертификат, url\n  tim serve shop --https     # https://shop.test:8443\n  tim unlink shop",
	"tim setup":       "  tim setup shop             # доделать склонированный проект\n  tim setup git@github.com:me/shop.git\n  tim setup shop --dry-run   # посмотреть, что будет сделано",
	"tim check":       "  tim check                  # все проекты реестра\n  tim check shop             # один проект\n  tim check example.com      # домен не из реестра\n  tim check --local          # без сети\n  tim check --servers        # и серверы проектов по ssh",
	"tim server":      "  tim server prod            # алиас из ~/.ssh/config или проект\n  tim server prod --attacks  # кто ломится, за сутки\n  tim server prod --traffic  # кто ходит на сайты, за час\n  tim server prod --traffic --since 1d",
	"tim servers":     "  tim servers                # серверы проектов из реестра\n  tim servers --all          # все алиасы ~/.ssh/config\n  tim servers install        # замер раз в 6 часов для прогноза диска",
	"tim sync":        "  tim sync                   # порт и база из .env\n  tim sync --remote          # боевые домены из nginx на серверах\n  tim sync shop --force",
	"tim ports":       "  tim ports\n  tim port 5173          # карточка: что это, папка, кто запустил\n  tim port 8000 kill\n  tim ports --old --older 4h",
	"tim php":         "  tim php\n  tim php use 8.3\n  tim php exec 8.1 composer install",
	"tim disk":        "  tim disk                   # куда ушло место\n  tim disk --all\n  tim disk ~/Dev\n  tim disk --deps ~/Dev/clients --min 200M",
	"tim cron":        "  tim cron \"*/15 9-18 * * mon-fri\"\n  tim cron @daily -n 3",
	"tim jwt":         "  tim jwt eyJhbGciOi...\n  pbpaste | tim jwt",
	"tim config init": "  tim config init\n  tim config init --root ~/code --all -y",
}

// overviewSections: самое ходовое по разделам - то, что видно на tim без аргументов.
var overviewSections = []struct {
	title string
	cmds  [][2]string
}{
	{"Проекты", [][2]string{
		{"tim go", "перейти в проект, недавние сверху"},
		{"tim serve -d", "поднять dev-сервер в фоне"},
		{"tim setup <проект>", "от клона до работающего локально"},
		{"tim check", "обход всех проектов одной таблицей"},
		{"tim today --week", "что сделано за неделю"},
	}},
	{"Сеть и серверы", [][2]string{
		{"tim ssh", "ssh мимо VPN, выбор алиаса"},
		{"tim direct <сайт|приложение>", "пустить мимо VPN целиком"},
		{"tim tunnel all <сервер>", "весь трафик через ssh"},
		{"tim server <алиас>", "что с сервером: место, порты, sshd"},
		{"tim servers", "все серверы одной таблицей"},
	}},
	{"Сайты", [][2]string{
		{"tim site <домен>", "что за сайт: адреса, домен, сертификат"},
		{"tim audit <домен|проект>", "аудит снаружи: открытые файлы, заголовки, куки"},
		{"tim mail <домен>", "почта: SPF, DKIM, DMARC"},
		{"tim og <url>", "превью ссылки в мессенджерах"},
	}},
	{"Машина", [][2]string{
		{"tim status", "сводка: машина, проект, сеть"},
		{"tim top", "кто съел память и CPU"},
		{"tim disk", "куда ушло место и что можно убрать"},
		{"tim hw", "железо и загрузка: процессор, видеокарта, память"},
		{"tim kill <имя>", "закрыть зависшее приложение"},
		{"tim doctor", "что не так в окружении"},
	}},
}

// renamed: прежние имена команд и нынешние замены - старая привычка не должна упираться в "нет команды".
var renamed = []struct{ old, now string }{
	{"vpn exclude", "direct"}, {"route add", "direct"}, {"route clear", "direct rm"}, {"vpn clear", "direct rm"},
	{"vpn fix", "route fix"}, {"ssl", "cert"}, {"wifi", "net wifi"}, {"battery", "hw battery"}, {"stale", "ports --old"},
}

// cobraErrors компилируются только при ошибке, чтобы не тратить на это старт.
var cobraErrors = sync.OnceValue(func() []cobraError {
	return []cobraError{
		{regexp.MustCompile(`^unknown command "([^"]*)" for "([^"]*)"`), "нет команды $1 у $2: $2 --help"},
		{regexp.MustCompile(`^unknown flag: (\S+)`), "нет флага $1, посмотри --help"},
		{regexp.MustCompile(`^unknown shorthand flag: '(.)' in (\S+)`), "нет флага -$1, посмотри --help"},
		{regexp.MustCompile(`^flag needs an argument: (\S+)`), "флагу $1 нужно значение"},
		{regexp.MustCompile(`^invalid argument "([^"]*)" for "([^"]*)" flag`), "неверное значение $1 для $2"},
		{regexp.MustCompile(`^requires at least (\d+) arg\(s\), only received (\d+)`), "нужно аргументов не меньше $1, передано $2"},
		{regexp.MustCompile(`^accepts at most (\d+) arg\(s\), received (\d+)`), "аргументов не больше $1, передано $2"},
		{regexp.MustCompile(`^accepts (\d+) arg\(s\), received (\d+)`), "нужно аргументов: $1, передано $2"},
		{regexp.MustCompile(`^accepts between (\d+) and (\d+) arg\(s\), received (\d+)`), "нужно аргументов от $1 до $2, передано $3"},
		{regexp.MustCompile(`^invalid argument "([^"]*)" for "([^"]*)"`), "неверный аргумент $1 для $2"},
	}
})

// colorFlags подсвечивает сами флаги в строках FlagUsages, описания оставляет как есть.
func colorFlags(s string, color func(string) string) string {
	flagLine := regexp.MustCompile(`^(\s*)(-\S.*?)(\s{2,})(.*)$`)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if m := flagLine.FindStringSubmatch(l); m != nil {
			lines[i] = m[1] + color(m[2]) + m[3] + m[4]
		}
	}
	return strings.Join(lines, "\n")
}

// colorExamples: строки примеров, начинающиеся с команды, зелёные, комментарии после # серые.
func colorExamples(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		cmd, comment, hasComment := strings.Cut(l, "#")
		out := ui.Label(strings.TrimRight(cmd, " "))
		if hasComment {
			out += strings.Repeat(" ", len(cmd)-len(strings.TrimRight(cmd, " "))) + ui.Dim("#"+comment)
		}
		lines[i] = out
	}
	return strings.Join(lines, "\n")
}

// applyHelp: шаблон, русские подписи справочных флагов и примеры; вызывается после всех init().
// Один раз за запуск: экран зовёт её перед каждой справкой изнутри.
func applyHelp() { helpOnce.Do(setupHelp) }

func setupHelp() {
	cobra.AddTemplateFunc("h", ui.Strong)
	cobra.AddTemplateFunc("name", ui.Label)
	cobra.AddTemplateFunc("dim", ui.Dim)
	cobra.AddTemplateFunc("flags", func(s string) string { return colorFlags(s, ui.Label) })
	cobra.AddTemplateFunc("examples", colorExamples)
	cobra.AddTemplateFunc("utilLine", func() string {
		names := make([]string, len(utilCmds))
		for i, n := range utilCmds {
			names[i] = ui.Label(n)
		}
		return strings.Join(names, " ") + ui.Dim("  - что каждая делает: tim help utils")
	})
	cobra.AddTemplateFunc("useline", func(s string) string { return strings.Replace(s, "[flags]", ui.Dim("[флаги]"), 1) })
	root.SetUsageTemplate(usageTemplate)
	root.InitDefaultVersionFlag()
	if f := root.Flags().Lookup("version"); f != nil {
		f.Usage = "версия tim"
	}
	root.SetHelpCommand(&cobra.Command{Use: "help [команда]", Short: "Справка по команде", Hidden: true,
		Run: func(c *cobra.Command, args []string) {
			if len(args) == 1 && args[0] == "utils" {
				printUtils()
				return
			}
			target, _, err := root.Find(args)
			if err != nil || target == nil {
				target = root
			}
			_ = target.Help()
		}})
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.InitDefaultHelpFlag()
		if f := c.Flags().Lookup("help"); f != nil {
			f.Usage = "справка"
		}
		if ex, ok := examples[c.CommandPath()]; ok && c.Example == "" {
			c.Example = ex
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
}

// splash: лок-ап, как у --version; в трубе - две первые строки без значка.
func splash() {
	lines := versionLines()
	if on, _ := ui.Colors(); !on {
		lines = lines[:2]
	}
	for _, l := range lockup(lines) {
		ui.Println(l)
	}
}

// overview: что показать на tim без аргументов.
func overview() {
	splash()
	width := 0
	for _, s := range overviewSections {
		for _, c := range s.cmds {
			width = max(width, len([]rune(c[0])))
		}
	}
	for _, s := range overviewSections {
		ui.Println()
		ui.Println("  " + ui.Title(s.title))
		for _, c := range s.cmds {
			// "tim" одинаков во всех строках - приглушён, чтобы глаз цеплялся за саму команду.
			rest := strings.TrimPrefix(c[0], "tim ")
			ui.Println("    " + ui.Dim("tim ") + ui.Label(ui.Pad(rest, width-4)) + "  " + c[1])
		}
	}
	ui.Println()
	ui.Println(ui.Dim("  все команды: tim --help  \u2022  по команде: tim <команда> --help  \u2022  man tim"))
}

type cobraError struct {
	re   *regexp.Regexp
	tmpl string
}

// translateError переводит типовые ошибки cobra; подсказку "Did you mean" сохраняет по-русски.
func translateError(msg string) string {
	head, rest, _ := strings.Cut(msg, "\n")
	for _, e := range cobraErrors() {
		if e.re.MatchString(head) {
			head = e.re.ReplaceAllString(e.re.FindString(head), e.tmpl)
			break
		}
	}
	if _, sugg, ok := strings.Cut(rest, "Did you mean this?\n"); ok {
		var names []string
		for _, l := range strings.Split(sugg, "\n") {
			if n := strings.TrimSpace(l); n != "" {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			head += "; может, " + strings.Join(names, " или ") + "?"
		}
	}
	return head
}

// printUtils: полный список мелких утилит, свёрнутых в общей справке.
func printUtils() {
	ui.Println(ui.Info("Утилиты:"))
	for _, n := range utilCmds {
		if c, _, err := root.Find([]string{n}); err == nil && c != root {
			ui.Println("  " + ui.Label(ui.Pad(n, 6)) + " " + c.Short)
		}
	}
	ui.Println()
	ui.Println(ui.Dim("подробнее: tim <утилита> --help"))
}

// unknownHint: на неизвестную команду - куда она переехала, что это проект или на что она похожа.
// Пусто - подсказать нечего, остаётся обычная ошибка.
func unknownHint(args []string) string {
	var words []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			words = append(words, a)
		}
	}
	if len(words) == 0 {
		return ""
	}
	if hint := renamedHint(words); hint != "" {
		return hint
	}
	if hint := projectHint(words[0]); hint != "" {
		return hint
	}
	if sugg := similarCommands(words[0]); len(sugg) > 0 {
		return "нет команды " + words[0] + "; может, " + strings.Join(sugg, " или ") + "?"
	}
	return ""
}

func renamedHint(words []string) string {
	typed := strings.Join(words, " ")
	for _, r := range renamed {
		if typed == r.old || strings.HasPrefix(typed, r.old+" ") {
			return "tim " + r.old + " теперь tim " + r.now + ", подробнее: tim " + strings.Fields(r.now)[0] + " --help"
		}
	}
	return ""
}

// projectHint: вместо команды набрано имя проекта или его часть. Конфиг читается только здесь, на ошибке.
func projectHint(name string) string {
	cfg, err := config.Load()
	if err != nil || len(name) < 3 {
		return ""
	}
	if _, ok := cfg.Project(name); ok {
		return name + " - это проект: tim go " + name + " - перейти, tim proj info " + name + " - карточка, tim audit " + name + " - сайт"
	}
	like := cfg.ProjectsLike(name)
	if head, _, ok := strings.Cut(name, "-"); ok && len(like) == 0 && len(head) >= 3 {
		like = cfg.ProjectsLike(head)
	}
	if len(like) == 0 {
		return ""
	}
	if len(like) > 6 {
		like = append(like[:6], "...")
	}
	return "нет команды " + name + "; похожие проекты: " + strings.Join(like, ", ") + " - tim go <проект>"
}

// similarCommands: команды и псевдонимы с тем же началом или в одной-двух правках, самые близкие.
func similarCommands(typed string) []string {
	var names []string
	for _, c := range root.Commands() {
		if !c.Hidden {
			names = append(names, c.Name())
			names = append(names, c.Aliases...)
		}
	}
	best := 3
	for _, n := range names {
		best = min(best, editDistance(typed, n))
	}
	var out []string
	for _, n := range names {
		if strings.HasPrefix(n, typed) || len(typed) > 1 && best <= 2 && editDistance(typed, n) == best {
			out = append(out, n)
		}
	}
	return bestSuggestions(typed, out, 3)
}

// fail: ошибка в stderr - суть красным со знаком, под ней пример команды серым. Вывод команды остаётся
// в stdout чистым для трубы.
func fail(c *cobra.Command, msg string) {
	ui.UseStderr()
	applyHelp()
	head, rest, _ := strings.Cut(msg, "\n")
	ui.Println(ui.Bad("\u2715 ") + head)
	for _, l := range strings.Split(rest, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			ui.Println("  " + ui.Dim(l))
		}
	}
	if c == nil || c == root || rest != "" {
		return
	}
	for i, ex := range strings.Split(strings.TrimSpace(c.Example), "\n") {
		if i == 2 {
			break
		}
		if ex = strings.TrimSpace(ex); ex != "" {
			ex, _, _ = strings.Cut(ex, "  #")
			ui.Println("  " + ui.Dim(strings.TrimSpace(ex)))
		}
	}
}

// helpWithHeader: русская справка всегда, даже когда cobra зовёт её сама (команда без аргументов);
// у корня вместо абзаца - лок-ап.
func helpWithHeader(def func(*cobra.Command, []string)) func(*cobra.Command, []string) {
	return func(c *cobra.Command, args []string) {
		applyHelp()
		if c != root {
			def(c, args)
			return
		}
		for _, l := range lockup(helpLines()) {
			ui.Println(l)
		}
		ui.Println()
		long, short := root.Long, root.Short
		root.Long, root.Short = "", ""
		def(c, args)
		root.Long, root.Short = long, short
	}
}

// helpLines: имя и сборка; что умеет; где конфиг и как спросить про команду.
func helpLines() [][]string {
	place := []string{ui.Dim("конфиг " + paths.Show(config.Path()))}
	cfg := env.Cfg
	if cfg == nil {
		cfg, _ = config.Load()
	}
	if cfg != nil {
		place = append(place, ui.Dim(ui.Count(len(cfg.Projects), "проект", "проекта", "проектов")))
	}
	place = append(place, ui.Dim("по команде: tim <команда> --help"))
	return [][]string{{ui.Strong(brand.Name) + "  " + ui.Dim(env.Build())},
		{ui.Dim("помощник в терминале: проекты, сеть и туннели, машина, сайты, утилиты")}, place}
}
