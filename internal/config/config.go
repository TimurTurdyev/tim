// Package config: личные настройки tim в ~/.config/tim/config.toml.
package config

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/TimurTurdyev/tim/internal/paths"
)

// backedUp: копия .bak делается один раз за запуск, иначе вторая запись затрёт исходный файл с комментариями.
var backedUp bool

// Project: запись старого реестра проектов. tim её больше не читает, но и не теряет: при записи конфига
// она уходит обратно как была, а url пока служит списком сайтов.
type Project struct {
	Name   string `toml:"name"`
	Path   string `toml:"path"`
	SSH    string `toml:"ssh,omitempty"`
	Remote string `toml:"remote,omitempty"`
	Deploy string `toml:"deploy,omitempty"`
	Port   int    `toml:"port,omitzero"`
	DB     string `toml:"db,omitempty"`
	URL    string `toml:"url,omitempty"`
	Serve  string `toml:"serve,omitempty"`
}

// Task: задача старого tim each; хранится как была.
type Task struct {
	Name       string `toml:"name"`
	Marker     string `toml:"marker"`
	Run        string `toml:"run"`
	VersionKey string `toml:"version_key,omitempty"`
	VersionCmd string `toml:"version_cmd,omitempty"`
	Scan       bool   `toml:"scan,omitempty"`
}

// Template: шаблон старого tim new; хранится как был.
type Template struct {
	Name  string   `toml:"name"`
	Steps []string `toml:"steps"`
	Port  bool     `toml:"port,omitempty"`
}

// Recipe: своя заготовка сервера для палитры и tim server --run; в run подставляются {server} и {unit}.
// on = "project" и "machine" остались от tim run и не работают.
type Recipe struct {
	Name string `toml:"name"`
	Desc string `toml:"desc,omitempty"`
	Run  string `toml:"run"`
	On   string `toml:"on,omitempty"`
}

// Notify: куда слать итог обхода: бот Telegram (токен и chat) и ntfy (адрес с темой).
type Notify struct {
	Telegram string `toml:"telegram,omitempty"`
	Chat     string `toml:"chat,omitempty"`
	Ntfy     string `toml:"ntfy,omitempty"`
}

// Shell: что tim init добавляет в оболочку.
type Shell struct {
	Motd    bool              `toml:"motd,omitempty"`
	CdInfo  *bool             `toml:"cd_info,omitempty"`
	Aliases map[string]string `toml:"aliases,omitempty"`
}

// Host: цель обхода VPN - адрес, подсеть CIDR или домен.
type Host struct {
	Addr  string `toml:"addr"`
	Label string `toml:"label,omitempty"`
}

type Tunnel struct {
	Host string `toml:"host,omitempty"`
	Port int    `toml:"port,omitzero"`
}

type Config struct {
	Dev       string              `toml:"dev"`
	Wrapper   string              `toml:"wrapper,omitempty"`
	Editor    string              `toml:"editor,omitempty"`
	Motd      []string            `toml:"motd,omitempty"`
	Shell     Shell               `toml:"shell,omitempty"`
	Tasks     []Task              `toml:"task,omitempty"`
	Groups    map[string][]string `toml:"groups,omitempty"`
	Templates []Template          `toml:"template,omitempty"`
	Recipes   []Recipe            `toml:"recipe,omitempty"`
	Notify    Notify              `toml:"notify,omitempty"`
	Tunnel    Tunnel              `toml:"tunnel,omitempty"`
	Sites     []string            `toml:"sites,omitempty"`
	Projects  []Project           `toml:"project"`
	Hosts     []Host              `toml:"host"`
}

// Path отдаёт путь к конфигу: TIM_CONFIG, иначе $XDG_CONFIG_HOME/tim/config.toml или ~/.config/tim/config.toml.
// Под sudo домашняя папка - того, кто вызвал sudo: у root свой пустой конфиг, и sudo tim direct
// иначе записал бы цель не туда.
func Path() string {
	if p := os.Getenv("TIM_CONFIG"); p != "" {
		return p
	}
	home := sudoHome()
	if home == "" {
		if x := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
			return filepath.Join(x, "tim", "config.toml")
		}
		home = paths.Home()
	}
	return filepath.Join(home, ".config", "tim", "config.toml")
}

func sudoHome() string {
	if os.Geteuid() != 0 {
		return ""
	}
	name := os.Getenv("SUDO_USER")
	if name == "" || name == "root" {
		return ""
	}
	u, err := user.Lookup(name)
	if err != nil {
		return ""
	}
	return u.HomeDir
}

// giveBack: файл, записанный под sudo, возвращается тому, кто вызвал sudo.
func giveBack(paths ...string) {
	if os.Geteuid() != 0 {
		return
	}
	uid, err1 := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, err2 := strconv.Atoi(os.Getenv("SUDO_GID"))
	if err1 != nil || err2 != nil {
		return
	}
	for _, p := range paths {
		_ = os.Chown(p, uid, gid)
	}
}

func Default() *Config {
	return &Config{Dev: DefaultRoot(), Tunnel: Tunnel{Port: 1080}}
}

// DefaultRoot: первая существующая из привычных папок с проектами, иначе ~/Dev.
func DefaultRoot() string {
	home := paths.Home()
	for _, d := range []string{"Dev", "Projects", "projects", "code", "src"} {
		p := filepath.Join(home, d)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	// На сервере сайты обычно не в домашней папке.
	for _, p := range []string{"/var/www", "/srv"} {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return filepath.Join(home, "Dev")
}

// Load читает конфиг; отсутствие файла не ошибка, тогда значения по умолчанию.
func Load() (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.Dev == "" {
		cfg.Dev = Default().Dev
	}
	if cfg.Tunnel.Port == 0 {
		cfg.Tunnel.Port = 1080
	}
	cfg.Dev = ExpandHome(cfg.Dev)
	return cfg, nil
}

func Save(cfg *Config) error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Кодировщик TOML не сохраняет комментарии: прежний файл остаётся рядом копией.
	if old, err := os.ReadFile(path); err == nil && !backedUp {
		if err := os.WriteFile(path+".bak", old, 0o600); err != nil {
			return err
		}
		backedUp = true
	}
	// В файл - как писал человек: ~ вместо домашней папки и без порта по умолчанию.
	out := *cfg
	out.Dev = collapseHome(cfg.Dev)
	if out.Tunnel.Port == 1080 {
		out.Tunnel.Port = 0
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := toml.NewEncoder(f).Encode(&out); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	giveBack(path, path+".bak")
	return nil
}

// ExpandHome раскрывает ~/ в начале пути.
func collapseHome(p string) string {
	home := paths.Home()
	if home != "" && strings.HasPrefix(p, home+"/") {
		return "~/" + p[len(home)+1:]
	}
	return p
}

func ExpandHome(p string) string {
	if p == "~" {
		return paths.Home()
	}
	if strings.HasPrefix(p, "~/") {
		home := paths.Home()
		return filepath.Join(home, p[2:])
	}
	return p
}

// SiteList: сайты для Сайтов и обхода - список sites, а пока он пуст, хосты из url старых [[project]].
func (c *Config) SiteList() []string {
	if len(c.Sites) > 0 {
		return c.Sites
	}
	var out []string
	for _, p := range c.Projects {
		host := strings.TrimPrefix(strings.TrimPrefix(p.URL, "https://"), "http://")
		host, _, _ = strings.Cut(host, "/")
		host, _, _ = strings.Cut(host, ":")
		if host != "" && !slices.Contains(out, host) {
			out = append(out, host)
		}
	}
	return out
}

// Legacy: разделы конфига, которые tim больше не читает: "62 [[project]], 3 [[task]]"; пусто - таких нет.
func (c *Config) Legacy() string {
	var parts []string
	add := func(n int, name string) {
		if n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+name)
		}
	}
	add(len(c.Projects), "[[project]]")
	add(len(c.Tasks), "[[task]]")
	add(len(c.Templates), "[[template]]")
	add(len(c.Groups), "[groups]")
	local := 0
	for _, r := range c.Recipes {
		if r.On != "" && r.On != "server" {
			local++
		}
	}
	add(local, "[[recipe]] не для сервера")
	if c.Wrapper != "" {
		parts = append(parts, "wrapper")
	}
	if c.Editor != "" {
		parts = append(parts, "editor")
	}
	if c.Shell.CdInfo != nil {
		parts = append(parts, "shell.cd_info")
	}
	return strings.Join(parts, ", ")
}

// AddHost добавляет цель обхода; повтор адреса обновляет пометку, если она задана. false - уже была.
func (c *Config) AddHost(h Host) bool {
	for i := range c.Hosts {
		if c.Hosts[i].Addr == h.Addr {
			if h.Label != "" {
				c.Hosts[i].Label = h.Label
			}
			return false
		}
	}
	c.Hosts = append(c.Hosts, h)
	return true
}

// RemoveHost убирает цель по адресу или пометке и возвращает убранные.
func (c *Config) RemoveHost(want string) []Host {
	var kept, removed []Host
	for _, h := range c.Hosts {
		if h.Addr == want || h.Label == want {
			removed = append(removed, h)
		} else {
			kept = append(kept, h)
		}
	}
	c.Hosts = kept
	return removed
}

func (c *Config) HostLabel(addr string) string {
	for _, h := range c.Hosts {
		if h.Addr == addr {
			return h.Label
		}
	}
	return ""
}

// ResolveHost: пометка или адрес из списка хостов.
func (c *Config) ResolveHost(want string) (string, bool) {
	for _, h := range c.Hosts {
		if h.Addr == want || h.Label == want {
			return h.Addr, true
		}
	}
	return "", false
}

// HasComments: в файле конфига есть строки-комментарии, которые запись потеряет.
func HasComments() bool {
	data, err := os.ReadFile(Path())
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			return true
		}
	}
	return false
}
