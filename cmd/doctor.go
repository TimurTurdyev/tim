package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/doctor"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type report struct{ warns, errs int }

func (r *report) title(s string) { ui.Println(ui.Info(s)) }
func (r *report) ok(s string)    { ui.Println("  " + ui.Label("ок ") + s) }
func (r *report) warn(s string)  { r.warns++; ui.Println("  " + ui.Warn("!  ") + s) }
func (r *report) bad(s string)   { r.errs++; ui.Println("  " + ui.Bad("x  ") + s) }

var watchedTools = []string{"php", "python3", "node", "npm", "go", "ruby", "composer", "mysql", "psql", "git", "ssh", "curl"}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Проверка окружения: конфиг, реестр, шим, PATH, менеджеры версий, клиенты баз, сервисы",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r := &report{}
		doctorConfig(r)
		doctorRegistry(r)
		doctorShim(r)
		dirs := filepath.SplitList(os.Getenv("PATH"))
		doctorPath(r, dirs)
		doctorManagers(r, dirs)
		doctorDB(r)
		doctorServices(r)
		summary := "ошибок " + strconv.Itoa(r.errs) + ", предупреждений " + strconv.Itoa(r.warns)
		if r.errs > 0 {
			return ui.Errorf("%s", summary)
		}
		ui.Println(ui.Dim(summary))
		return nil
	},
}

func doctorConfig(r *report) {
	r.title("Конфиг")
	if _, err := os.Stat(config.Path()); err != nil {
		r.warn(config.Path() + " нет, работают значения по умолчанию: tim config init")
	} else {
		r.ok(config.Path())
	}
	if st, err := os.Stat(cfg.Dev); err != nil || !st.IsDir() {
		r.bad("корня проектов нет: " + cfg.Dev)
	} else {
		r.ok("корень проектов " + cfg.Dev)
	}
}

func doctorRegistry(r *report) {
	r.title("Реестр")
	if len(cfg.Projects) == 0 {
		r.warn("проектов нет: tim proj scan")
		return
	}
	names, paths, ports := map[string]int{}, map[string]string{}, map[int][]string{}
	aliases := map[string]bool{}
	for _, a := range sshx.Aliases() {
		aliases[a] = true
	}
	problems := 0
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		names[p.Name]++
		dir := filepath.Clean(cfg.ProjectDir(p))
		if other, ok := paths[dir]; ok {
			r.warn(p.Name + " и " + other + " указывают на одну папку")
			problems++
		}
		paths[dir] = p.Name
		if _, err := os.Stat(dir); err != nil {
			r.bad(p.Name + ": папки нет " + dir + " (tim proj rm " + p.Name + ")")
			problems++
		}
		if p.Port > 0 {
			ports[p.Port] = append(ports[p.Port], p.Name)
		}
		if p.SSH != "" && !strings.Contains(p.SSH, "@") && !aliases[p.SSH] {
			r.warn(p.Name + ": ssh-алиаса " + p.SSH + " нет в ~/.ssh/config")
			problems++
		}
	}
	for n, c := range names {
		if c > 1 {
			r.bad("имя " + n + " встречается " + strconv.Itoa(c) + " раза")
			problems++
		}
	}
	portList := make([]int, 0, len(ports))
	for p := range ports {
		portList = append(portList, p)
	}
	sort.Ints(portList)
	for _, port := range portList {
		if ps := ports[port]; len(ps) > 1 {
			r.warn("порт " + strconv.Itoa(port) + " у нескольких: " + strings.Join(ps, " "))
			problems++
		}
	}
	for _, h := range cfg.Hosts {
		if _, err := netinfo.ParseTarget(h.Addr); err != nil {
			r.bad("цель обхода " + h.Addr + ": " + err.Error())
			problems++
		}
	}
	for _, t := range cfg.Tasks {
		if t.Name == "" || t.Marker == "" || t.Run == "" {
			r.bad("задача each без name, marker или run: " + t.Name)
			problems++
		}
	}
	if problems == 0 {
		r.ok(strconv.Itoa(len(cfg.Projects)) + " проектов, папки на месте, повторов нет")
	}
}

func doctorShim(r *report) {
	r.title("Оболочка")
	if os.Getenv("TIM_SHIM") != "" {
		r.ok("шим tim init работает в этой оболочке")
		return
	}
	if path, _ := rcFile(""); sourced(path, 2) {
		r.warn("шим подключён, но эта оболочка открыта раньше: открой новый терминал")
		return
	}
	r.warn("шим не подключён: tim init (без него не работают tim go и tim php use)")
}

func doctorPath(r *report, dirs []string) {
	r.title("PATH")
	dups, missing := doctor.PathIssues(dirs)
	for _, d := range dups {
		r.warn("повтор " + d)
	}
	var gone []string
	for _, d := range missing {
		if !systemDir(d) {
			gone = append(gone, d)
			r.warn("каталога нет " + d)
		}
	}
	missing = gone
	shadow := doctor.Shadowed(dirs, watchedTools)
	for n, paths := range shadow {
		var own []string
		for _, p := range paths {
			if !systemDir(filepath.Dir(p)) {
				own = append(own, p)
			}
		}
		if len(own) > 1 {
			shadow[n] = own
		} else {
			delete(shadow, n)
		}
	}
	names := make([]string, 0, len(shadow))
	for n := range shadow {
		names = append(names, n)
	}
	sort.Strings(names)
	home := paths.Home()
	short := func(p string) string { return strings.Replace(p, home, "~", 1) }
	for _, n := range names {
		paths := shadow[n]
		rest := make([]string, 0, len(paths)-1)
		for _, p := range paths[1:] {
			rest = append(rest, short(p))
		}
		r.warn(n + ": работает " + short(paths[0]) + ", под ним " + strings.Join(rest, ", "))
	}
	if len(dups)+len(missing)+len(names) == 0 {
		r.ok(strconv.Itoa(len(dirs)) + " каталогов, повторов и затенённых программ нет")
	}
}

// systemDir: каталоги macOS, которые path_helper добавляет сам; их отсутствие и копии программ в них не поломка.
func systemDir(d string) bool {
	for _, p := range []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/System/", "/Library/Apple/", "/var/run/com.apple."} {
		if d == p || strings.HasPrefix(d, p) {
			return true
		}
	}
	return false
}

func doctorManagers(r *report, dirs []string) {
	r.title("Менеджеры версий")
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	ms := doctor.Managers(dirs, env)
	if len(ms) == 0 {
		r.ok("не найдены")
		return
	}
	for _, m := range ms {
		langs := strings.Join(m.Langs, " ")
		if langs == "" {
			langs = "языки не видны по PATH"
		}
		ui.Println("     " + ui.Pad(m.Name, 8) + " " + ui.Dim(langs))
	}
	conflicts := doctor.Conflicts(ms)
	langs := make([]string, 0, len(conflicts))
	for l := range conflicts {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, l := range langs {
		r.warn(l + " ведут сразу " + strings.Join(conflicts[l], " и ") + ": версия зависит от порядка в PATH")
	}
	if len(langs) == 0 {
		r.ok("конфликтов нет")
	}
}

var dbFamilies = map[string][]string{
	"mysql":    {"mysql", "mysql-client", "mariadb"},
	"postgres": {"postgresql", "libpq"},
}

func doctorDB(r *report) {
	r.title("Клиенты баз")
	if !runner.Has("brew") {
		r.ok("brew нет, проверка пропущена")
		return
	}
	formulas := brew.Formulas()
	issues := 0
	for _, fam := range []string{"mysql", "postgres"} {
		var found []string
		for _, f := range formulas {
			for _, prefix := range dbFamilies[fam] {
				if (f == prefix || strings.HasPrefix(f, prefix+"@")) && !strings.Contains(f, "connector") {
					found = append(found, f)
					break
				}
			}
		}
		if len(found) > 1 {
			client := map[string]string{"mysql": "mysql", "postgres": "psql"}[fam]
			which, _ := runner.Output("sh", "-c", "command -v "+client)
			r.warn(fam + ": формул несколько (" + strings.Join(found, " ") + "), " + client + " в PATH: " + dashIfEmpty(strings.TrimSpace(which)))
			issues++
		}
	}
	if issues == 0 {
		r.ok("по одной формуле на базу")
	}
}

func doctorServices(r *report) {
	r.title("Сервисы brew")
	out, err := runner.Output("launchctl", "list")
	if err != nil {
		r.ok("launchctl недоступен, пропущено")
		return
	}
	failed := 0
	for _, a := range sysinfo.ParseLaunchctl(out) {
		if !a.Running() && a.Status != 0 {
			r.bad(a.Name + " упал с кодом " + strconv.Itoa(a.Status) + ": tim svc logs " + a.Name)
			failed++
		}
	}
	if failed == 0 {
		r.ok("упавших нет")
	}
}

func init() { root.AddCommand(doctorCmd) }
