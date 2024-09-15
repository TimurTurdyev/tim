// Package config: личные настройки tim в ~/.config/tim/config.toml.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Project struct {
	Name   string `toml:"name"`
	Path   string `toml:"path"`
	SSH    string `toml:"ssh,omitempty"`
	Remote string `toml:"remote,omitempty"`
	Deploy string `toml:"deploy,omitempty"`
	Port   int    `toml:"port,omitzero"`
	DB     string `toml:"db,omitempty"`
}

// Host: цель обхода VPN - адрес, подсеть CIDR или домен.
type Host struct {
	Addr  string `toml:"addr"`
	Label string `toml:"label,omitempty"`
}

type Tunnel struct {
	Host string `toml:"host,omitempty"`
	Port int    `toml:"port"`
}

type Config struct {
	Dev      string    `toml:"dev"`
	Wrapper  string    `toml:"wrapper,omitempty"`
	Tunnel   Tunnel    `toml:"tunnel"`
	Projects []Project `toml:"project"`
	Hosts    []Host    `toml:"host"`
}

// Path отдаёт путь к конфигу: TIM_CONFIG или ~/.config/tim/config.toml.
func Path() string {
	if p := os.Getenv("TIM_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tim", "config.toml")
}

func Default() *Config {
	return &Config{Dev: DefaultRoot(), Tunnel: Tunnel{Port: 1080}}
}

// DefaultRoot: первая существующая из привычных папок с проектами, иначе ~/Dev.
func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	for _, d := range []string{"Dev", "Projects", "projects", "code", "src"} {
		p := filepath.Join(home, d)
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
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// ExpandHome раскрывает ~/ в начале пути.
func ExpandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
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
