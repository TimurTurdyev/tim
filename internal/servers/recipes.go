package servers

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/recipes"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// serverRecipes: встроенные заготовки и свои для сервера; {server} в своих уже подставлен.
func serverRecipes(cfg *config.Config, target string) []srvinfo.Recipe {
	out := append([]srvinfo.Recipe{}, srvinfo.Recipes...)
	for _, r := range recipes.Own(cfg, recipes.OnServer) {
		out = append(out, srvinfo.OwnRecipe(r.Name, r.Desc, recipes.Expand(r.Run, map[string]string{"server": target})))
	}
	return out
}

func recipeNames(list []srvinfo.Recipe) string {
	var ids []string
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	return strings.Join(ids, ", ")
}

// runRecipe: заготовка на сервере. Своя перед первым запуском показывает команду и спрашивает;
// --dry-run печатает скрипт и на сервер не ходит.
func runRecipe(env *app.Env, target, name, param string) error {
	list := serverRecipes(env.Cfg, target)
	var r srvinfo.Recipe
	found := false
	for _, x := range list {
		if x.ID == name {
			r, found = x, true
		}
	}
	if !found {
		return ui.Errorf("нет заготовки %s; есть: %s", name, recipeNames(list))
	}
	script, err := r.Script(param)
	if err != nil {
		return ui.Errorf("%v", err)
	}
	if runner.DryRun {
		ui.Println(ui.Dim("dry-run: ssh " + target + " sh -s, скрипт:"))
		ui.Println(script)
		return nil
	}
	if r.Own && !recipes.Approved(script) {
		ui.Println(ui.Warn("своя заготовка "+r.ID+": tim не знает, только ли она читает") + "\n" + script)
		if !runner.Confirm("выполнить на " + target + "?") {
			return ui.Errorf("не выполнял: своя заготовка запускается с согласия, в скрипте - с -y")
		}
		_ = recipes.Approve(script)
	}
	out, err := srvinfo.RunRecipe(target, r, param)
	ui.Print(out)
	if err != nil {
		return ui.Errorf("%s: %v; маршрут и вход проверит tim ssh %s", target, err, target)
	}
	return nil
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
