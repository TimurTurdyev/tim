package serve

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/recipes"
)

func runTaskCommand(env *app.Env) *cobra.Command {
	var in string
	c := &cobra.Command{
		Use:   "run [задача|заготовка] [--in проект]",
		Short: "Задача проекта из Makefile, justfile, Taskfile, package.json, composer.json или своя заготовка",
		Long: `Без аргументов - список: задачи проекта текущей папки (или --in) и свои заготовки из [[recipe]]
конфига. С именем - выполняет: make, just, task, npm/pnpm/yarn/bun run, composer run-script в папке проекта.
Своя заготовка с on = "project" выполняется в папке проекта, с on = "machine" - здесь; в команде
подставляются {project}, {path}, {port}. Что делает своя команда, tim не знает: перед первым запуском
показывает её и спрашивает, согласие помнит, пока команда не изменится; --dry-run только печатает.`,
		Args: cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var out []string
			if p, err := env.Cfg.Pick(in); err == nil {
				for _, t := range recipes.Tasks(env.Cfg.ProjectDir(p)) {
					out = append(out, t.Name+"\t"+t.Tool)
				}
			}
			for _, r := range append(recipes.Own(env.Cfg, recipes.OnProject), recipes.Own(env.Cfg, recipes.OnMachine)...) {
				out = append(out, r.Name+"\tсвоя")
			}
			return out, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error { return runTask(env, app.First(args), in) },
	}
	c.Flags().StringVar(&in, "in", "", "проект; без него - проект текущей папки")
	_ = c.RegisterFlagCompletionFunc("in", app.ProjectNames)
	return c
}
