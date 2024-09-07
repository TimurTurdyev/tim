package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Migrate собирает конфиг из файлов zsh-версии: tools.projects, tools.hosts, tools.conf.
func Migrate(dir string) (*Config, error) {
	cfg := Default()
	if lines, err := readLines(filepath.Join(dir, "tools.projects")); err == nil {
		for _, line := range lines {
			f := strings.Split(line, "\t")
			if len(f) < 2 {
				continue
			}
			for len(f) < 7 {
				f = append(f, "-")
			}
			p := Project{Name: f[0], Path: f[1], SSH: dash(f[2]), Remote: dash(f[3]), Deploy: dash(f[4]), DB: dash(f[6])}
			p.Port, _ = strconv.Atoi(dash(f[5]))
			cfg.Projects = append(cfg.Projects, p)
		}
	}
	if lines, err := readLines(filepath.Join(dir, "tools.hosts")); err == nil {
		for _, line := range lines {
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			h := Host{Addr: f[0]}
			if len(f) > 1 {
				h.Label = f[1]
			}
			cfg.Hosts = append(cfg.Hosts, h)
		}
	}
	if lines, err := readLines(filepath.Join(dir, "tools.conf")); err == nil {
		for _, line := range lines {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			switch strings.TrimSpace(k) {
			case "TOOLS_TUNNEL_HOST":
				cfg.Tunnel.Host = v
			case "TOOLS_TUNNEL_PORT":
				cfg.Tunnel.Port, _ = strconv.Atoi(v)
			}
		}
	}
	return cfg, nil
}

func dash(s string) string {
	if s == "-" {
		return ""
	}
	return s
}

// readLines без комментариев и пустых строк.
func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t\r"))
	}
	return out, sc.Err()
}
