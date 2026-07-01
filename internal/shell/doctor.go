package shell

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/doctor"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/rcfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// watchedTools: программы, чьи копии в разных каталогах PATH чаще всего путают.
var watchedTools = []string{"php", "python3", "node", "npm", "go", "ruby", "composer", "mysql", "psql", "git", "ssh", "curl"}

// dbFamilies: формулы brew одного клиента базы - две сразу дают путаницу версий.
var dbFamilies = map[string][]string{
	"mysql":    {"mysql", "mysql-client", "mariadb"},
	"postgres": {"postgresql", "libpq"},
}

// report: раздел за разделом печатает находки и считает их.
type report struct{ warns, errs int }

func (r *report) title(s string) { ui.Println(ui.Info(s)) }
func (r *report) ok(s string)    { ui.Println("  " + ui.Label("ок ") + s) }
func (r *report) warn(s string)  { r.warns++; ui.Println("  " + ui.Warn("!  ") + s) }
func (r *report) bad(s string)   { r.errs++; ui.Println("  " + ui.Bad("x  ") + s) }

func diagnose(env *app.Env) error {
	r := &report{}
	checkConfig(r, env.Cfg)
	checkRegistry(r, env.Cfg)
	checkShim(r)
	dirs := filepath.SplitList(os.Getenv("PATH"))
	checkPath(r, dirs)
	checkManagers(r, dirs)
	checkDB(r)
	checkServices(r)
	summary := "ошибок " + strconv.Itoa(r.errs) + ", предупреждений " + strconv.Itoa(r.warns)
	if r.errs > 0 {
		return ui.Errorf("%s", summary)
	}
	ui.Println(ui.Dim(summary))
	return nil
}

func checkConfig(r *report, cfg *config.Config) {
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

func checkRegistry(r *report, cfg *config.Config) {
	r.title("Реестр")
	if len(cfg.Projects) == 0 {
		r.warn("проектов нет: tim proj scan")
		return
	}
	aliases := map[string]bool{}
	for _, a := range sshx.Aliases() {
		aliases[a] = true
	}
	findings := doctor.Registry(cfg, aliases)
	for _, f := range findings {
		if f.Bad {
			r.bad(f.Text)
		} else {
			r.warn(f.Text)
		}
	}
	if len(findings) == 0 {
		r.ok(strconv.Itoa(len(cfg.Projects)) + " проектов, папки на месте, повторов нет")
	}
}

func checkShim(r *report) {
	r.title("Оболочка")
	if os.Getenv("TIM_SHIM") != "" {
		r.ok("шим tim init работает в этой оболочке")
		return
	}
	if path, _ := rcfile.Path(""); rcfile.Sourced(path, 2) {
		r.warn("шим подключён, но эта оболочка открыта раньше: открой новый терминал")
		return
	}
	r.warn("шим не подключён: tim init (без него не работают tim go и tim php use)")
}

func checkPath(r *report, dirs []string) {
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
	shadow := ownShadows(doctor.Shadowed(dirs, watchedTools))
	names := make([]string, 0, len(shadow))
	for n := range shadow {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		found := shadow[n]
		rest := make([]string, 0, len(found)-1)
		for _, p := range found[1:] {
			rest = append(rest, paths.Show(p))
		}
		r.warn(n + ": работает " + paths.Show(found[0]) + ", под ним " + strings.Join(rest, ", "))
	}
	if len(dups)+len(gone)+len(names) == 0 {
		r.ok(strconv.Itoa(len(dirs)) + " каталогов, повторов и затенённых программ нет")
	}
}

// ownShadows: затенения без системных копий - их держит сама macOS, это не поломка.
func ownShadows(shadow map[string][]string) map[string][]string {
	out := map[string][]string{}
	for n, found := range shadow {
		var own []string
		for _, p := range found {
			if !systemDir(filepath.Dir(p)) {
				own = append(own, p)
			}
		}
		if len(own) > 1 {
			out[n] = own
		}
	}
	return out
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

func checkManagers(r *report, dirs []string) {
	r.title("Менеджеры версий")
	env := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	managers := doctor.Managers(dirs, env)
	if len(managers) == 0 {
		r.ok("не найдены")
		return
	}
	for _, m := range managers {
		langs := strings.Join(m.Langs, " ")
		if langs == "" {
			langs = "языки не видны по PATH"
		}
		ui.Println("     " + ui.Pad(m.Name, 8) + " " + ui.Dim(langs))
	}
	conflicts := doctor.Conflicts(managers)
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

func checkDB(r *report) {
	r.title("Клиенты баз")
	if !runner.Has("brew") {
		r.ok("brew нет, проверка пропущена")
		return
	}
	formulas := brew.Formulas()
	issues := 0
	for _, fam := range []string{"mysql", "postgres"} {
		found := familyFormulas(formulas, dbFamilies[fam])
		if len(found) < 2 {
			continue
		}
		client := "mysql"
		if fam == "postgres" {
			client = "psql"
		}
		which, _ := runner.LookPath(client)
		r.warn(fam + ": формул несколько (" + strings.Join(found, " ") + "), " + client + " в PATH: " + ui.Dash(which))
		issues++
	}
	if issues == 0 {
		r.ok("по одной формуле на базу")
	}
}

func familyFormulas(formulas, prefixes []string) []string {
	var found []string
	for _, f := range formulas {
		for _, prefix := range prefixes {
			if (f == prefix || strings.HasPrefix(f, prefix+"@")) && !strings.Contains(f, "connector") {
				found = append(found, f)
				break
			}
		}
	}
	return found
}

func checkServices(r *report) {
	if runtime.GOOS == "linux" {
		r.title("Службы systemd")
		failed := sysinfo.FailedUnits()
		for _, u := range failed {
			r.bad(u + " упала: tim svc logs " + strings.TrimSuffix(u, ".service"))
		}
		if len(failed) == 0 {
			r.ok("упавших нет")
		}
		return
	}
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
