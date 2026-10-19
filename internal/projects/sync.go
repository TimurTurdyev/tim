package projects

import (
	"context"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/fill"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/newproj"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// nginxDump: одна безобидная команда чтения - конфиги vhost'ов целиком.
const nginxDump = `cat /etc/nginx/sites-enabled/* /etc/nginx/conf.d/*.conf /usr/local/etc/nginx/servers/* 2>/dev/null`

// change: поле проекта, которое sync заполнил или перезаписал.
type change struct {
	project, field, from, to string
}

// syncing: что известно о серверах для заполнения реестра.
type syncing struct {
	cfg    *config.Config
	o      syncOptions
	vhosts map[string][]fill.Vhost // алиас -> сайты nginx на нём
	ips    map[string]string       // алиас -> его адрес
	out    []change
}

// syncRegistry: поля реестра из .env проектов, а с флагами - домены с серверов и ssh-алиасы.
func syncRegistry(env *app.Env, args []string, o syncOptions) error {
	list, err := syncTargets(env, args)
	if err != nil {
		return err
	}
	s := &syncing{cfg: env.Cfg, o: o}
	if o.remote {
		if err := s.askServers(list); err != nil {
			return err
		}
	}
	if o.ssh {
		ui.Println(ui.Dim("резолвлю адреса ssh-алиасов"))
		s.ips = aliasIPs()
	}
	for _, p := range list {
		s.fill(p)
	}
	if len(s.out) == 0 {
		ui.Println(ui.Dim("нечего заполнять: поля уже на месте или в .env нет данных (перезаписать: --force)"))
		return nil
	}
	printChanges(s.out)
	if err := env.Save(); err != nil {
		return err
	}
	ui.Println(ui.Dim("изменений " + strconv.Itoa(len(s.out)) + "; проверить: tim proj list"))
	if !o.remote {
		ui.Println(ui.Dim("узнать боевые домены у серверов: tim sync --remote"))
	}
	return nil
}

// syncTargets: без аргументов весь реестр, а не проект текущей папки: sync - операция по всему хозяйству.
func syncTargets(env *app.Env, args []string) ([]*config.Project, error) {
	var list []*config.Project
	var err error
	if len(args) == 0 {
		list, err = env.Named(nil)
	} else {
		list, err = env.Projects(args)
	}
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ui.Errorf("реестр пуст: tim config init или tim proj scan")
	}
	return list, nil
}

// askServers: для каждого ssh-алиаса проектов один заход на сервер за списком доменов nginx.
func (s *syncing) askServers(list []*config.Project) error {
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
		return nil
	}
	// Серверов немного, а ssh-рукопожатие тяжёлое: четыре разом.
	confs := par.Map(aliases, 4, func(a string) []fill.Vhost {
		conf, err := runner.Output("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", a, nginxDump)
		if err != nil || strings.TrimSpace(conf) == "" {
			return nil
		}
		return fill.ParseNginx(conf)
	})
	s.vhosts = map[string][]fill.Vhost{}
	for i, a := range aliases {
		if len(confs[i]) > 0 {
			s.vhosts[a] = confs[i]
		}
	}
	return nil
}

// fill: поля одного проекта - url, база, порт, ssh.
func (s *syncing) fill(p *config.Project) {
	dir := s.cfg.ProjectDir(p)
	f := fill.FromEnv(dir)
	if f.URL == "" && p.SSH != "" {
		if d, found := fill.MatchVhost(s.vhosts[p.SSH], p.Remote, p.Name); found {
			f.URL = "https://" + d
		}
	}
	if d := fill.DomainOfPath(p.Remote); f.URL == "" && d != "" && resolves(d) {
		f.URL = "https://" + d
	}
	s.set(p, "url", &p.URL, f.URL)
	s.set(p, "db", &p.DB, f.DB)
	if f.Port > 0 && (p.Port == 0 || s.o.force) && p.Port != f.Port {
		s.out = append(s.out, change{p.Name, "port", strconv.Itoa(p.Port), strconv.Itoa(f.Port)})
		p.Port = f.Port
	}
	// Проекту, который поднимается через serve, - свой порт, чтобы двое не делили 8000.
	if p.Port == 0 && run.Servable(dir) {
		n := newproj.NextPort(s.cfg.Ports(), netinfo.DefaultPort, netinfo.BusyPorts().Busy)
		s.out = append(s.out, change{p.Name, "port", "", strconv.Itoa(n)})
		p.Port = n
	}
	if alias := s.aliasOf(p.URL); alias != "" {
		s.set(p, "ssh", &p.SSH, alias)
	}
}

func (s *syncing) set(p *config.Project, field string, cur *string, val string) {
	if val == "" || *cur == val || (*cur != "" && !s.o.force) {
		return
	}
	s.out = append(s.out, change{p.Name, field, *cur, val})
	*cur = val
}

// resolves: у домена есть адрес - папка названа живым доменом, а не просто с точкой в имени.
func resolves(host string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	return err == nil && len(addrs) > 0
}

// aliasOf: ssh-алиас, чей адрес совпадает с адресом сайта.
func (s *syncing) aliasOf(url string) string {
	if s.ips == nil || url == "" {
		return ""
	}
	host, _ := netinfo.HostPort(url, "")
	ip := netinfo.ResolveIPv4(host)
	if ip == "" {
		return ""
	}
	var names []string
	for a, aip := range s.ips {
		if aip == ip {
			names = append(names, a)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return names[0]
}

// aliasIPs: адрес каждого алиаса из ~/.ssh/config, чтобы сопоставить проект с сервером по IP.
func aliasIPs() map[string]string {
	aliases := sshx.Aliases()
	ips := par.Map(aliases, 12, sshx.IP)
	m := map[string]string{}
	for i, a := range aliases {
		if ips[i] != "" {
			m[a] = ips[i]
		}
	}
	return m
}

func printChanges(changes []change) {
	width := 0
	for _, c := range changes {
		width = max(width, ui.Width(c.project))
	}
	for _, c := range changes {
		line := ui.Label(ui.Pad(c.project, width)) + "  " + ui.Pad(c.field, 5) + " " + c.to
		if c.from != "" && c.from != "0" {
			line += ui.Dim("  было " + c.from)
		}
		ui.Println(line)
	}
}
