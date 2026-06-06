// Package config: личные настройки tim в ~/.config/tim/config.toml.
package config

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/TimurTurdyev/tim/internal/paths"
)

// backedUp: копия .bak делается один раз за запуск, иначе вторая запись затрёт исходный файл с комментариями.
var backedUp bool

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

// Task: команда, которую tim each выполняет во всех проектах с файлом-маркером.
type Task struct {
	Name       string `toml:"name"`
	Marker     string `toml:"marker"`
	Run        string `toml:"run"`
	VersionKey string `toml:"version_key,omitempty"`
	VersionCmd string `toml:"version_cmd,omitempty"`
	Scan       bool   `toml:"scan,omitempty"`
}

func (c *Config) Task(name string) (*Task, bool) {
	for i := range c.Tasks {
		if c.Tasks[i].Name == name {
			return &c.Tasks[i], true
		}
	}
	return nil, false
}

// Template: свои шаги создания проекта для tim new; {dir}, {name}, {parent} подставляются.
type Template struct {
	Name  string   `toml:"name"`
	Steps []string `toml:"steps"`
	Port  bool     `toml:"port,omitempty"`
}

// Shell: что tim init добавляет в оболочку.
type Shell struct {
	Motd    bool              `toml:"motd,omitempty"`
	CdInfo  *bool             `toml:"cd_info,omitempty"`
	Aliases map[string]string `toml:"aliases,omitempty"`
}

// CdInfoOn: подсказка о проекте при входе в папку, по умолчанию включена.
func (s Shell) CdInfoOn() bool { return s.CdInfo == nil || *s.CdInfo }

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
	Tunnel    Tunnel              `toml:"tunnel,omitempty"`
	Projects  []Project           `toml:"project"`
	Hosts     []Host              `toml:"host"`
}

// Path отдаёт путь к конфигу: TIM_CONFIG, иначе $XDG_CONFIG_HOME/tim/config.toml или ~/.config/tim/config.toml.
// Под sudo домашняя папка - того, кто вызвал sudo: у root свой пустой конфиг, и sudo tim route add
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
	if strings.HasPrefix(p, "~/") {
		home := paths.Home()
		return filepath.Join(home, p[2:])
	}
	return p
}

func (c *Config) Project(name string) (*Project, bool) {
	for i := range c.Projects {
		if c.Projects[i].Name == name {
			return &c.Projects[i], true
		}
	}
	return nil, false
}

// ProjectDir: абсолютный путь к клону проекта.
func (c *Config) ProjectDir(p *Project) string {
	if filepath.IsAbs(p.Path) {
		return p.Path
	}
	return filepath.Join(c.Dev, p.Path)
}

// ProjectHere ищет проект, в чью папку входит cwd, самый глубокий из подходящих.
func (c *Config) ProjectHere(cwd string) (*Project, bool) {
	cwd, _ = filepath.EvalSymlinks(cwd)
	var best *Project
	bestLen := 0
	for i := range c.Projects {
		dir, err := filepath.EvalSymlinks(c.ProjectDir(&c.Projects[i]))
		if err != nil {
			continue
		}
		if cwd == dir || strings.HasPrefix(cwd, dir+string(filepath.Separator)) {
			if len(dir) > bestLen {
				best, bestLen = &c.Projects[i], len(dir)
			}
		}
	}
	return best, best != nil
}

// Relative: путь от корня проектов, если проект внутри него, иначе абсолютный.
func (c *Config) Relative(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	if rel, err := filepath.Rel(c.Dev, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return abs
}

func (c *Config) AddProject(p Project) error {
	if _, ok := c.Project(p.Name); ok {
		return errors.New("проект " + p.Name + " уже есть в реестре")
	}
	c.Projects = append(c.Projects, p)
	return nil
}

func (c *Config) RemoveProject(name string) bool {
	for i := range c.Projects {
		if c.Projects[i].Name == name {
			c.Projects = append(c.Projects[:i], c.Projects[i+1:]...)
			return true
		}
	}
	return false
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

// Pick: проект по имени, а без имени по текущей папке.
func (cfg *Config) Pick(name string) (*Project, error) {
	if name != "" {
		p, ok := cfg.Project(name)
		if !ok {
			return nil, fmt.Errorf("нет проекта %s в конфиге: tim proj list", name)
		}
		return p, nil
	}
	if len(cfg.Projects) == 0 {
		return nil, fmt.Errorf("реестр пуст: tim config init или tim proj scan")
	}
	cwd, _ := os.Getwd()
	if p, ok := cfg.ProjectHere(cwd); ok {
		return p, nil
	}
	return nil, fmt.Errorf("укажи проект или зайди в его папку: tim proj list")
}
