// Package local: локальная разработка - имена в /etc/hosts, зона *.test через dnsmasq,
// локальный домен проекту и сертификаты своим CA.
package local

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/hostsfile"
	"github.com/TimurTurdyev/tim/internal/localca"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Commands: команды раздела; cert new и cert ca встают под cert раздела sites.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{
		hostCommand(), linkCommand(env), unlinkCommand(env),
		app.Under("cert", certNewCommand()), app.Under("cert", certCACommand()),
	}
}

func hostCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "host",
		Short: "Локальные имена в /etc/hosts: профили, включение и выключение",
		Long: `tim ведёт в /etc/hosts свой блок между "# tim:begin" и "# tim:end" и не трогает ничего за его
пределами. Записи разложены по профилям: профиль целиком включается и выключается одной командой.
Перед каждой записью файл копируется в /etc/hosts.tim-backup.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return listHosts() },
	}
	var ip, profile string
	add := &cobra.Command{
		Use:   "add <имя...> [--ip 127.0.0.1] [--profile имя]",
		Short: "Добавить имена в /etc/hosts (sudo)",
		Args:  cobra.MinimumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return addHosts(args, ip, profile) },
	}
	add.Flags().StringVar(&ip, "ip", "127.0.0.1", "адрес, на который указывают имена")
	add.Flags().StringVar(&profile, "profile", hostsfile.DefaultProfile, "профиль записи")
	rm := &cobra.Command{
		Use:   "rm <имя...>",
		Short: "Убрать имена из /etc/hosts (sudo)",
		Args:  cobra.MinimumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return removeHosts(args) },
	}
	on := &cobra.Command{
		Use:   "on <профиль>",
		Short: "Включить записи профиля (sudo)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return switchProfile(args[0], true) },
	}
	off := &cobra.Command{
		Use:   "off <профиль>",
		Short: "Выключить записи профиля, не удаляя их (sudo)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return switchProfile(args[0], false) },
	}
	check := &cobra.Command{
		Use:   "check <имя...>",
		Short: "Куда на самом деле резолвится имя",
		Args:  cobra.MinimumNArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { checkHosts(args) },
	}
	c.AddCommand(add, rm, on, off, check, resolverCommand())
	return c
}

func resolverCommand() *cobra.Command {
	var off bool
	c := &cobra.Command{
		Use:   "resolver [зона] [off]",
		Short: "Весь домен вида *.test на 127.0.0.1 через dnsmasq, без записей в /etc/hosts",
		Long: `/etc/hosts знает только точные имена, поэтому поддомены вида a.shop.test в него не записать.
Резолвер решает это целиком: dnsmasq отдаёт 127.0.0.1 на любое имя в зоне, а /etc/resolver/<зона>
говорит системе спрашивать про эту зону у него. Без аргумента показывает уже настроенные зоны.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				listResolvers()
				return nil
			}
			if len(args) == 2 && args[1] != "off" {
				return ui.Errorf("после зоны может быть только off: tim host resolver %s off", args[0])
			}
			return setResolver(args[0], off || len(args) == 2)
		},
	}
	c.Flags().BoolVar(&off, "off", false, "")
	_ = c.Flags().MarkHidden("off")
	return c
}

func linkCommand(env *app.Env) *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "link [проект] [--name shop.test]",
		Short: "Локальный домен проекту: имя в /etc/hosts, сертификат и url в реестре",
		Long: `Три шага одной командой: имя (по умолчанию <проект>.test) в /etc/hosts в профиле link,
сертификат своим CA в ~/.local/share/tim/certs и url = "https://имя" в реестре.
Дальше tim serve <проект> --https отдаёт проект по этому адресу. Убрать всё обратно: tim unlink.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return link(env, app.First(args), name) },
	}
	c.Flags().StringVar(&name, "name", "", "имя вместо <проект>.test")
	return c
}

func unlinkCommand(env *app.Env) *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:               "unlink [проект] [--name shop.test]",
		Short:             "Убрать то, что сделал tim link",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return unlink(env, app.First(args), name) },
	}
	c.Flags().StringVar(&name, "name", "", "имя вместо <проект>.test")
	return c
}

func certNewCommand() *cobra.Command {
	var o certOptions
	c := &cobra.Command{
		Use:   "new <домен|адрес...> [--dir] [--days] [--exact]",
		Short: "Выпустить сертификат для локальной разработки своим CA",
		Long: `Подписывает сертификат локальным CA из ~/.local/share/tim/ca; при первом запуске CA создаётся.
К каждому домену добавляется и "*.домен", потому что wildcard не покрывает сам домен (--exact отключает).
Чтобы браузер доверял сертификату, CA должен быть в хранилище: tim cert ca install.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return issueCert(args, o) },
	}
	c.Flags().StringVar(&o.dir, "dir", "", "куда положить файлы (по умолчанию текущая папка)")
	c.Flags().IntVar(&o.days, "days", localca.LeafDays, "срок в днях")
	c.Flags().BoolVar(&o.exact, "exact", false, "только перечисленные имена, без *.домен")
	return c
}

func certCACommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "ca",
		Short: "Локальный CA: где лежит, до каких пор годен, в хранилище ли он",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { showCA() },
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Добавить локальный CA в системное хранилище доверия (sudo)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return trustCA() },
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Убрать локальный CA из системного хранилища (sudo)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return distrustCA() },
	}
	c.AddCommand(install, uninstall)
	return c
}
