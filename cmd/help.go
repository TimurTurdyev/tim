package cmd

import (
	"regexp"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/ui"
)

const usageTemplate = `{{h "Использование:"}}{{if .Runnable}}
  {{useline .UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} {{dim "[команда]"}}{{end}}{{if gt (len .Aliases) 0}}

{{h "Другие имена:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{h "Примеры:"}}
{{examples .Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{h "Команды:"}}{{range $cmds}}{{if .IsAvailableCommand}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{h .Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) .IsAvailableCommand)}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{h "Ещё:"}}{{range $cmds}}{{if (and (eq .GroupID "") .IsAvailableCommand)}}
  {{name (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{h "Флаги:"}}
{{flags (.LocalFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasAvailableInheritedFlags}}

{{h "Общие флаги:"}}
{{flags (.InheritedFlags.FlagUsages | trimTrailingWhitespaces)}}{{end}}{{if .HasAvailableSubCommands}}

{{dim (print "Подробнее о команде: " .CommandPath " <команда> --help")}}{{end}}
`

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

var examples = map[string]string{
	"tim go":          "  tim go                     # выбор проекта из реестра и переход\n  tim go shop .              # в папку над клоном",
	"tim proj":        "  tim proj list clients\n  tim proj scan --add\n  tim proj add . --name shop --url https://shop.example.com",
	"tim serve":       "  tim serve                  # проект текущей папки\n  tim serve lpack            # по части имени\n  tim serve shop -d          # в фоне\n  tim serve @mercurio        # группа из [groups]\n  tim serve list\n  tim serve stop all",
	"tim ssh":         "  tim ssh                    # выбор алиаса\n  tim ssh prod uptime\n  tim ssh shop               # по ssh-полю проекта",
	"tim route":       "  tim route add git.example.com --label git\n  tim route add 203.0.113.0/24\n  tim route\n  tim route clean",
	"tim vpn":         "  tim vpn\n  tim vpn exclude api.example.com\n  tim vpn fix                # после смены сети",
	"tim tunnel":      "  tim tunnel up prod          # SOCKS на 127.0.0.1:1080\n  tim tunnel all prod         # весь трафик через ssh\n  tim tunnel port 15432:5432 prod\n  tim tunnel db shop\n  tim tunnel expose 8000",
	"tim db":          "  tim db                     # клиент по .env проекта\n  tim db shop dump\n  tim db shop --via prod",
	"tim motd":        "  tim motd\n  tim motd repos versions\n  tim motd --all --time",
	"tim site":        "  tim site example.com\n  tim site https://shop.example.com --json",
	"tim mail":        "  tim mail example.com\n  tim mail example.com --selector s2024",
	"tim http":        "  tim http example.com\n  tim http -L http://example.com",
	"tim each":        "  tim each                   # задачи из конфига\n  tim each deps --list\n  tim each --marker go.mod -- go mod tidy",
	"tim new":         "  tim new shop --stack laravel --in clients\n  tim new tool --stack go\n  tim new site --stack static   # свой шаблон из конфига",
	"tim doctor":      "  tim doctor",
	"tim ports":       "  tim ports\n  tim port 8000 kill\n  tim stale --hours 4",
	"tim php":         "  tim php\n  tim php use 8.3\n  tim php exec 8.1 composer install",
	"tim disk":        "  tim disk\n  tim disk ~/Dev/clients --min 200M",
	"tim cron":        "  tim cron \"*/15 9-18 * * mon-fri\"\n  tim cron @daily -n 3",
	"tim jwt":         "  tim jwt eyJhbGciOi...\n  pbpaste | tim jwt",
	"tim config init": "  tim config init\n  tim config init --root ~/code --all -y",
}

// applyHelp: шаблон, русские подписи справочных флагов и примеры; вызывается после всех init().
func applyHelp() {
	cobra.AddTemplateFunc("h", func(s string) string { return ui.Info(s) })
	cobra.AddTemplateFunc("name", func(s string) string { return ui.Label(s) })
	cobra.AddTemplateFunc("dim", func(s string) string { return ui.Dim(s) })
	cobra.AddTemplateFunc("flags", func(s string) string { return colorFlags(s, ui.Label) })
	cobra.AddTemplateFunc("examples", colorExamples)
	cobra.AddTemplateFunc("useline", func(s string) string { return strings.Replace(s, "[flags]", ui.Dim("[флаги]"), 1) })
	root.SetUsageTemplate(usageTemplate)
	root.InitDefaultVersionFlag()
	if f := root.Flags().Lookup("version"); f != nil {
		f.Usage = "версия tim"
	}
	root.SetHelpCommand(&cobra.Command{Use: "help [команда]", Short: "Справка по команде", Hidden: true,
		Run: func(c *cobra.Command, args []string) {
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

var common = [][2]string{
	{"tim go", "перейти в проект (выбор из реестра)"},
	{"tim serve -d", "поднять dev-сервер проекта в фоне"},
	{"tim ssh", "ssh мимо VPN, выбор алиаса"},
	{"tim tunnel all <сервер>", "весь трафик через ssh"},
	{"tim route add <адрес>", "пустить адрес мимо VPN"},
	{"tim motd", "сводка: машина, проект, сеть, сервисы"},
	{"tim site <домен>", "что за сайт: IP, домен, сертификат, CMS"},
	{"tim doctor", "что не так в окружении"},
	{"tim new <имя> --stack go", "новый проект по шаблону"},
	{"tim each <задача>", "команда во всех проектах"},
}

// overview: что показать на tim без аргументов.
func overview() {
	ui.Println(ui.Label("tim") + " " + ui.Dim(resolveVersion()) + "  " + "помощник в терминале: проекты, сеть, туннели, запуск")
	ui.Println()
	width := 0
	for _, c := range common {
		width = max(width, len([]rune(c[0])))
	}
	for _, c := range common {
		ui.Println("  " + ui.Label(ui.Pad(c[0], width)) + "  " + c[1])
	}
	ui.Println()
	ui.Println(ui.Dim("все команды: tim --help, по команде: tim <команда> --help, man tim"))
}

type cobraError struct {
	re   *regexp.Regexp
	tmpl string
}

// cobraErrors компилируются только при ошибке, чтобы не тратить на это старт.
var cobraErrors = sync.OnceValue(func() []cobraError {
	return []cobraError{
		{regexp.MustCompile(`^unknown command "([^"]*)" for "([^"]*)"`), "нет команды $1 у $2: $2 --help"},
		{regexp.MustCompile(`^unknown flag: (\S+)`), "нет флага $1: посмотри --help"},
		{regexp.MustCompile(`^unknown shorthand flag: '(.)' in (\S+)`), "нет флага -$1: посмотри --help"},
		{regexp.MustCompile(`^flag needs an argument: (\S+)`), "флагу $1 нужно значение"},
		{regexp.MustCompile(`^invalid argument "([^"]*)" for "([^"]*)" flag`), "неверное значение $1 для $2"},
		{regexp.MustCompile(`^requires at least (\d+) arg\(s\), only received (\d+)`), "нужно аргументов не меньше $1, передано $2"},
		{regexp.MustCompile(`^accepts at most (\d+) arg\(s\), received (\d+)`), "аргументов не больше $1, передано $2"},
		{regexp.MustCompile(`^accepts (\d+) arg\(s\), received (\d+)`), "нужно аргументов: $1, передано $2"},
		{regexp.MustCompile(`^accepts between (\d+) and (\d+) arg\(s\), received (\d+)`), "нужно аргументов от $1 до $2, передано $3"},
		{regexp.MustCompile(`^invalid argument "([^"]*)" for "([^"]*)"`), "неверный аргумент $1 для $2"},
	}
})

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
