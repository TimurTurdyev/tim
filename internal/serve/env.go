package serve

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/envfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// importantEnv: ключи .env, по которым видно, куда смотрит проект.
var importantEnv = []string{"APP_NAME", "APP_ENV", "APP_DEBUG", "APP_URL", "DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE",
	"DB_USERNAME", "DATABASE_URL", "REDIS_HOST", "REDIS_PORT", "CACHE_STORE", "CACHE_DRIVER", "QUEUE_CONNECTION",
	"SESSION_DRIVER", "MAIL_MAILER", "MAIL_HOST", "MEILISEARCH_HOST", "SCOUT_DRIVER"}

// printEnv: выжимка .env без секретов и сверка с реестром.
func printEnv(env *app.Env, name string, all bool) error {
	p, err := env.Project(name)
	if err != nil {
		return err
	}
	file := filepath.Join(env.Cfg.ProjectDir(p), ".env")
	vals, err := envfile.Parse(file)
	if err != nil {
		ui.Println(ui.Dim(p.Name + ": .env нет"))
		return nil
	}
	ui.Println(ui.Label(p.Name) + "  " + ui.Dim(file))
	keys := importantEnv
	if all {
		keys = make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	for _, k := range keys {
		if v, ok := vals[k]; ok {
			ui.Println(ui.Label(ui.Pad(k, 20)) + " " + envfile.Mask(k, v))
		}
	}
	if db := vals["DB_DATABASE"]; p.DB != "" && db != "" && !strings.HasSuffix(p.DB, db) {
		ui.Println(ui.Warn("база в реестре " + p.DB + ", в .env " + db))
	}
	if port := envfile.Port(vals["APP_URL"]); p.Port > 0 && port != "" && port != strconv.Itoa(p.Port) {
		ui.Println(ui.Warn("порт в реестре " + strconv.Itoa(p.Port) + ", в APP_URL " + port))
	}
	if !all {
		ui.Println(ui.Dim("все ключи: tim env --all (секреты скрыты)"))
	}
	return nil
}

// diffEnv: .env против образца, а с server - состав ключей против .env на сервере.
func diffEnv(env *app.Env, name string, server bool) error {
	p, err := env.Project(name)
	if err != nil {
		return err
	}
	dir := env.Cfg.ProjectDir(p)
	local, err := envfile.Parse(filepath.Join(dir, ".env"))
	if err != nil {
		return ui.Errorf("у %s нет .env: tim setup %s", p.Name, p.Name)
	}
	if server {
		return diffServerEnv(p, local)
	}
	sample := exampleFile(dir)
	if sample == "" {
		return ui.Errorf("у %s нет .env.example, не с чем сравнивать", p.Name)
	}
	example, err := envfile.Parse(sample)
	if err != nil {
		return err
	}
	d := envfile.Diff(local, example)
	ui.Println(ui.Label(p.Name) + ui.Dim("  .env против "+filepath.Base(sample)))
	if d.Clean() {
		ui.Println(ui.Dim("расхождений нет"))
		return nil
	}
	printKeys("нет в .env", true, d.Missing)
	printKeys("пусто в .env", true, d.Empty)
	printKeys("секрет из образца", true, d.Sample)
	printKeys("нет в образце", false, d.Extra)
	ui.Println(ui.Dim("значения не показываем; выжимка без секретов: tim env " + p.Name))
	return nil
}

func diffServerEnv(p *config.Project, local map[string]string) error {
	remote, err := remoteEnvKeys(p)
	if err != nil {
		return err
	}
	ui.Println(ui.Label(p.Name) + ui.Dim("  .env против "+p.SSH+":"+p.Deploy+"/.env"))
	missing, extra := envfile.OnlyIn(remote, local), envfile.OnlyIn(local, remote)
	printKeys("нет локально", true, missing)
	printKeys("нет на сервере", false, extra)
	if len(missing)+len(extra) == 0 {
		ui.Println(ui.Dim("состав ключей совпадает"))
	}
	return nil
}

// exampleFile: образец .env, который ведут в репозитории.
func exampleFile(dir string) string {
	for _, name := range []string{".env.example", ".env.sample", ".env.dist"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// remoteEnvKeys: ключи .env с сервера. Значения не запрашиваем и не печатаем,
// сравниваем только состав: на сервере лежат боевые секреты.
func remoteEnvKeys(p *config.Project) (map[string]string, error) {
	if p.SSH == "" || p.Deploy == "" {
		return nil, ui.Errorf("у проекта %s нет полей ssh и deploy: tim proj add --ssh --deploy", p.Name)
	}
	path := strings.TrimSuffix(p.Deploy, "/") + "/.env"
	cmd := "sed -n 's/^[[:space:]]*\\(export[[:space:]]*\\)\\{0,1\\}\\([A-Za-z_][A-Za-z0-9_]*\\)=.*/\\2/p' " + path
	out, err := runner.Output("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", p.SSH, cmd)
	if err != nil {
		return nil, ui.Errorf("%s: не прочитал %s: %v", p.SSH, path, err)
	}
	keys := map[string]string{}
	for _, k := range strings.Fields(out) {
		keys[k] = ""
	}
	if len(keys) == 0 {
		return nil, ui.Errorf("%s: %s пуст или его нет", p.SSH, path)
	}
	return keys, nil
}

func printKeys(label string, bad bool, keys []string) {
	if len(keys) == 0 {
		return
	}
	head := ui.Warn(ui.Pad(label, 22))
	if bad {
		head = ui.Bad(ui.Pad(label, 22))
	}
	ui.Println(head + strings.Join(keys, " "))
}
