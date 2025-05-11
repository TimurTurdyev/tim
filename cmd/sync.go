package cmd

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/fill"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/newproj"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	syncForce  bool
	syncSSH    bool
	syncRemote bool
)

// nginxDump: одна безобидная команда чтения - конфиги vhost'ов целиком.
const nginxDump = `cat /etc/nginx/sites-enabled/* /etc/nginx/conf.d/*.conf /usr/local/etc/nginx/servers/* 2>/dev/null`

// remoteDomains: для каждого ssh-алиаса один заход на сервер за списком доменов nginx.
func remoteDomains(aliases []string) map[string][]fill.Vhost {
	out := make([][]fill.Vhost, len(aliases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, a := range aliases {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			conf, err := runner.Output("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", a, nginxDump)
			if err != nil || strings.TrimSpace(conf) == "" {
				return
			}
			out[i] = fill.ParseNginx(conf)
		})
	}
	wg.Wait()
	m := map[string][]fill.Vhost{}
	for i, a := range aliases {
		if len(out[i]) > 0 {
			m[a] = out[i]
		}
	}
	return m
}

type change struct {
	project, field, from, to string
}

// aliasIPs: адрес каждого алиаса из ~/.ssh/config, чтобы сопоставить проект с сервером по IP.
func aliasIPs() map[string]string {
	aliases := sshx.Aliases()
	out := make([]string, len(aliases))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 12)
	for i, a := range aliases {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = sshx.IP(a)
		})
	}
	wg.Wait()
	m := map[string]string{}
	for i, a := range aliases {
		if out[i] != "" {
			m[a] = out[i]
		}
	}
	return m
}

var syncCmd = &cobra.Command{
	Use:   "sync [проект...] [--remote] [--ssh] [--force]",
	Short: "Заполнить поля реестра тем, что известно из самих проектов",
	Long: `Берёт из .env проекта адрес сайта (APP_URL), порт и базу и дописывает их в реестр.
Локальные адреса (localhost, .test) адресом сайта не считаются. По умолчанию заполняются только
пустые поля. С --ssh у проектов с адресом сайта определяется ssh-алиас: хост резолвится в IP
и сравнивается с адресами алиасов из ~/.ssh/config.
С --remote домен берётся у самого сервера: читаются конфиги nginx и сопоставляются с проектом
по пути деплоя или по имени. Это только чтение, ничего на сервере не меняется.`,
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Без аргументов берём весь реестр, а не проект текущей папки: sync - операция по всему хозяйству.
		var list []*config.Project
		if len(args) == 0 {
			for i := range cfg.Projects {
				list = append(list, &cfg.Projects[i])
			}
		} else {
			var err error
			if list, err = resolveProjects(args); err != nil {
				return err
			}
		}
		if len(list) == 0 {
			return ui.Errorf("реестр пуст: tim config init или tim proj scan")
		}
		var vhosts map[string][]fill.Vhost
		if syncRemote {
			seen := map[string]bool{}
			var aliases []string
			for _, p := range list {
				if p.SSH != "" && !seen[p.SSH] {
					seen[p.SSH] = true
					aliases = append(aliases, p.SSH)
				}
			}
			if len(aliases) == 0 {
				return ui.Errorf("ни у одного проекта нет ssh-алиаса в реестре")
			}
			ui.Println(ui.Dim("спрашиваю домены у " + strconv.Itoa(len(aliases)) + " серверов"))
			if runner.DryRun {
				ui.Println(ui.Dim("$ ssh <алиас> " + nginxDump))
			} else {
				vhosts = remoteDomains(aliases)
			}
		}
		var ips map[string]string
		if syncSSH {
			ui.Println(ui.Dim("резолвлю адреса ssh-алиасов"))
			ips = aliasIPs()
		}
		var changes []change
		set := func(p *config.Project, field string, cur *string, val string) {
			if val == "" || *cur == val || (*cur != "" && !syncForce) {
				return
			}
			changes = append(changes, change{p.Name, field, *cur, val})
			*cur = val
		}
		for _, p := range list {
			f := fill.FromEnv(cfg.ProjectDir(p))
			if f.URL == "" && p.SSH != "" {
				if v, ok := vhosts[p.SSH]; ok {
					if d, found := fill.MatchVhost(v, p.Remote, p.Name); found {
						f.URL = "https://" + d
					}
				}
			}
			set(p, "url", &p.URL, f.URL)
			set(p, "db", &p.DB, f.DB)
			if f.Port > 0 && (p.Port == 0 || syncForce) && p.Port != f.Port {
				changes = append(changes, change{p.Name, "port", strconv.Itoa(p.Port), strconv.Itoa(f.Port)})
				p.Port = f.Port
			}
			// Проекту, который поднимается через serve, - свой порт, чтобы двое не делили 8000.
			if p.Port == 0 && servable(cfg.ProjectDir(p)) {
				n := newproj.NextPort(registryPorts(), 8000, func(s string) bool { _, b := netinfo.PortBusy(s); return b })
				changes = append(changes, change{p.Name, "port", "", strconv.Itoa(n)})
				p.Port = n
			}
			if ips == nil || p.URL == "" {
				continue
			}
			host, _ := hostPort(p.URL, "")
			ip := netinfo.ResolveIPv4(host)
			if ip == "" {
				continue
			}
			var names []string
			for a, aip := range ips {
				if aip == ip {
					names = append(names, a)
				}
			}
			sort.Strings(names)
			if len(names) > 0 {
				set(p, "ssh", &p.SSH, names[0])
			}
		}
		if len(changes) == 0 {
			ui.Println(ui.Dim("нечего заполнять: поля уже на месте или в .env нет данных (перезаписать: --force)"))
			return nil
		}
		width := 0
		for _, c := range changes {
			width = max(width, len([]rune(c.project)))
		}
		for _, c := range changes {
			line := ui.Label(ui.Pad(c.project, width)) + "  " + ui.Pad(c.field, 5) + " " + c.to
			if c.from != "" && c.from != "0" {
				line += ui.Dim("  было " + c.from)
			}
			ui.Println(line)
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		ui.Println(ui.Dim("изменений " + strconv.Itoa(len(changes)) + "; проверить: tim proj list"))
		if !syncRemote {
			ui.Println(ui.Dim("узнать боевые домены у серверов: tim sync --remote"))
		}
		return nil
	},
}

func init() {
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "перезаписывать и непустые поля")
	syncCmd.Flags().BoolVar(&syncRemote, "remote", false, "спросить домены у серверов по ssh (конфиги nginx)")
	syncCmd.Flags().BoolVar(&syncSSH, "ssh", false, "определить ssh-алиас по адресу сайта")
	root.AddCommand(syncCmd)
}
