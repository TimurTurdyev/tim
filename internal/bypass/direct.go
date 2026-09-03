package bypass

import (
	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// directOptions: флаги tim direct - цель в конфиге и как смотреть за сайтом.
type directOptions struct {
	add     addOptions
	unblock unblockOptions
}

// direct: одна команда на весь обход VPN. Что пустить напрямую, видно по аргументу: сеть по форме,
// приложение по тому, что оно установлено, остальное с точкой - сайт.
func direct(env *app.Env, args []string, o directOptions) error {
	if len(args) == 0 {
		return directStatus(env)
	}
	switch Classify(args[0], AppExists) {
	case KindApp:
		if len(args) > 1 {
			return ui.Errorf("приложение - по одному: tim direct %s", args[0])
		}
		return AppLaunch(args[0])
	case KindSite:
		a := o.add
		if len(args) > 1 || a.owner || a.noUp || a.force || a.label != "" {
			return addTargets(env, args, a)
		}
		return unblock(env, args[0], o.unblock)
	case KindNet:
		return addTargets(env, args, o.add)
	}
	return ui.Errorf("не понял %s: сайт пишется с точкой, приложение - как оно называется в системе, сеть - as:N, адрес или подсеть", args[0])
}

// directStatus: что сейчас идёт напрямую - приложения под правилами и цели с маршрутами.
func directStatus(env *app.Env) error {
	if err := AppStatus(); err != nil {
		return err
	}
	ui.Println()
	printRoutes(env.Cfg, false)
	return nil
}
