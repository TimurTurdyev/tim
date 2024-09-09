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
	Port   int    `toml:"port,omitempty"`
	DB     string `toml:"db,omitempty"`
}

type Host struct {
	Addr  string `toml:"addr"`
	Label string `toml:"label"`
}

type Tunnel struct {
	Host string `toml:"host"`
	Port int    `toml:"port"`
}

type Config struct {
	Dev      string    `toml:"dev"`
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
	home, _ := os.UserHomeDir()
	return &Config{Dev: filepath.Join(home, "Dev"), Tunnel: Tunnel{Port: 1080}}
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
	cfg.Dev = expandHome(cfg.Dev)
	return cfg, nil
}

func Save(cfg *Config) error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func expandHome(p string) string {
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
