package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/envfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var envDiffServer bool

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

func printKeys(label, color string, keys []string) {
	if len(keys) == 0 {
		return
	}
	head := ui.Pad(label, 22)
	switch color {
	case "bad":
		head = ui.Bad(head)
	default:
		head = ui.Warn(head)
	}
	ui.Println(head + strings.Join(keys, " "))
}

var envDiffCmd = &cobra.Command{
	Use:   "diff [проект] [--server]",
	Short: "Чем .env отличается от образца, а с --server - от .env на сервере",
	Long: `Без флага сравнивает .env с .env.example: каких ключей не хватает, какие лишние,
где значение пустое и где секрет остался из образца. С --server сравнивается только состав
ключей с .env на сервере проекта: значения с сервера не запрашиваются.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := projectFor(first(args))
		if err != nil {
			return err
		}
		dir := cfg.ProjectDir(p)
		env, err := envfile.Parse(filepath.Join(dir, ".env"))
		if err != nil {
			return ui.Errorf("у %s нет .env: tim setup %s", p.Name, p.Name)
		}
		if envDiffServer {
			remote, err := remoteEnvKeys(p)
			if err != nil {
				return err
			}
			ui.Println(ui.Label(p.Name) + ui.Dim("  .env против "+p.SSH+":"+p.Deploy+"/.env"))
			missing, extra := envfile.OnlyIn(remote, env), envfile.OnlyIn(env, remote)
			printKeys("нет локально", "bad", missing)
			printKeys("нет на сервере", "warn", extra)
			if len(missing)+len(extra) == 0 {
				ui.Println(ui.Dim("состав ключей совпадает"))
			}
			return nil
		}
		sample := exampleFile(dir)
		if sample == "" {
			return ui.Errorf("у %s нет .env.example, не с чем сравнивать", p.Name)
		}
		example, err := envfile.Parse(sample)
		if err != nil {
			return err
		}
		d := envfile.Diff(env, example)
		ui.Println(ui.Label(p.Name) + ui.Dim("  .env против "+filepath.Base(sample)))
		if d.Clean() {
			ui.Println(ui.Dim("расхождений нет"))
			return nil
		}
		printKeys("нет в .env", "bad", d.Missing)
		printKeys("пусто в .env", "bad", d.Empty)
		printKeys("секрет из образца", "bad", d.Sample)
		printKeys("нет в образце", "warn", d.Extra)
		ui.Println(ui.Dim("значения не показываем; выжимка без секретов: tim env " + p.Name))
		return nil
	},
}

func init() {
	envDiffCmd.Flags().BoolVar(&envDiffServer, "server", false, "сравнить состав ключей с .env на сервере проекта")
	envCmd.AddCommand(envDiffCmd)
}
