// Package ext: свои команды tim-<имя> на любом языке и tim render, который рисует их отчёт в виде tim.
package ext

import (
	"cmp"
	"os"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

// Commands: команды раздела.
func Commands() []*cobra.Command { return []*cobra.Command{renderCommand()} }

func renderCommand() *cobra.Command {
	return app.WithJSON(&cobra.Command{
		Use:   "render",
		Short: "Отчёт в JSON со stdin - в вид tim: для своих команд и конвейеров",
		Long: `Читает отчёт со stdin и печатает его как встроенные команды tim; с --json отдаёт data.
Отчёт - объект: title, verdict {level, text}, data (что уйдёт в --json) и blocks - список блоков,
у каждого один ключ:
  heading  "заголовок группы"
  facts    [{key, value, level}]
  table    {columns: [{title, kind: text|number|bytes|duration, style: plain|key|muted}], rows: [[...]]}
           ячейка - строка, число (длительность в секундах) или {text, level}
  findings [{area, level, text, why, fix}]
  note     "мелкая строка"
  next     [{text, command}]
level - skip, info, ok, warn, bad.

Своя команда - исполняемый tim-<имя> в ` + "`~/.config/tim/commands`" + ` или в PATH: tim <имя> запускает её с теми же
аргументами. Она получает TIM_JSON=1 при --json, TIM_DRY_RUN=1 при --dry-run, TIM_CONFIG и TIM_BIN, а на
--tim-describe отвечает строкой для справки.`,
		Example: `  printf '{"title":"бэкапы","blocks":[{"facts":[{"key":"последний","value":"03:00"}]}]}' | tim render
  #!/bin/sh   (файл tim-backups, chmod +x)
  [ "$1" = --tim-describe ] && { echo "Бэкапы баз: когда и сколько весят"; exit; }
  ls -l /var/backups | awk '...' | "$TIM_BIN" render`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := report.Decode(os.Stdin)
			if err != nil {
				return ui.Errorf("%v", err)
			}
			return view.Show(r, view.AsJSON)
		},
	})
}

// Mount вешает свои команды на корень: name - только её (незнакомое слово в командной строке), пусто - все
// (справка, completion, главный экран). Встроенные имена своими не перекрываются.
func Mount(root *cobra.Command, name string) {
	var list []Command
	if name != "" {
		if c, ok := Lookup(name); ok {
			list = []Command{c}
		}
	} else {
		list = Find()
	}
	descs := make([]string, len(list))
	if name == "" {
		descs = par.Map(list, par.Limit, Describe)
	}
	for i, c := range list {
		if taken(root, c.Name) {
			continue
		}
		root.AddCommand(&cobra.Command{
			Use:                c.Name,
			Short:              cmp.Or(descs[i], "своя команда: "+c.Path),
			DisableFlagParsing: true,
			Annotations:        map[string]string{app.External: c.Path, app.JSON: "yes"},
			RunE:               func(cmd *cobra.Command, args []string) error { return Run(c, args) },
		})
	}
}

func taken(root *cobra.Command, name string) bool {
	for _, c := range root.Commands() {
		if c.Name() == name || c.HasAlias(name) {
			return true
		}
	}
	return false
}
