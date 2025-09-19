// Package sshx: алиасы из ~/.ssh/config и туннели через ControlMaster.
package sshx

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/TimurTurdyev/tim/internal/paths"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// Aliases перечисляет Host без масок из ~/.ssh/config.
func Aliases() []string {
	home, _ := os.UserHomeDir()
	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "Host" {
			continue
		}
		for _, a := range fields[1:] {
			if !strings.ContainsAny(a, "*?!") {
				out = append(out, a)
			}
		}
	}
	return out
}

func IsAlias(name string) bool {
	for _, a := range Aliases() {
		if a == name {
			return true
		}
	}
	return false
}

// Target: что ssh на самом деле подставит для алиаса или user@host.
type Target struct{ Host, User, Port string }

func Resolve(target string) Target {
	out, _ := runner.Output("ssh", "-G", target)
	var t Target
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch k {
		case "hostname":
			t.Host = v
		case "user":
			t.User = v
		case "port":
			t.Port = v
		}
	}
	return t
}

// IP адреса назначения: сам адрес или через DNS.
func IP(target string) string {
	t := Resolve(target)
	if t.Host == "" {
		return ""
	}
	return netinfo.ResolveIPv4(t.Host)
}

// StateDir для сокетов и описаний туннелей.
func StateDir() string {
	d := paths.State()
	_ = os.MkdirAll(d, 0o755)
	return d
}

func sockPath(name string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "tim-"+name+".sock")
}

func statePath(name string) string { return filepath.Join(StateDir(), "tunnel-"+name) }

var keepAlive = []string{"-o", "ExitOnForwardFailure=yes", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-o", "ConnectTimeout=15"}

// Tunnel: фоновый ssh с control-сокетом.
type Tunnel struct {
	Name string
	Host string
	Desc string
}

func (t Tunnel) Alive() bool {
	if _, err := os.Stat(sockPath(t.Name)); err != nil {
		return false
	}
	_, err := runner.Output("ssh", "-S", sockPath(t.Name), "-O", "check", t.Host)
	return err == nil
}

// Start поднимает туннель с переданными аргументами проброса (-D, -L, -R).
func (t Tunnel) Start(forward ...string) error {
	if t.Alive() {
		return nil
	}
	_ = os.Remove(sockPath(t.Name))
	args := append([]string{"-f", "-N", "-M", "-S", sockPath(t.Name)}, keepAlive...)
	args = append(args, forward...)
	args = append(args, t.Host)
	if err := runner.Run("ssh", args...); err != nil {
		return err
	}
	if runner.DryRun {
		return nil
	}
	return os.WriteFile(statePath(t.Name), []byte(t.Host+"\t"+t.Desc+"\n"), 0o600)
}

func (t Tunnel) Stop() {
	if _, err := os.Stat(sockPath(t.Name)); err == nil {
		_, _ = runner.Output("ssh", "-S", sockPath(t.Name), "-O", "exit", t.Host)
	}
	_ = os.Remove(sockPath(t.Name))
	_ = os.Remove(statePath(t.Name))
}

// Known: туннели, у которых есть описание в state.
func Known() []Tunnel {
	entries, _ := filepath.Glob(filepath.Join(StateDir(), "tunnel-*"))
	var list []Tunnel
	for _, e := range entries {
		data, err := os.ReadFile(e)
		if err != nil {
			continue
		}
		host, desc, _ := strings.Cut(strings.TrimSpace(string(data)), "\t")
		list = append(list, Tunnel{Name: strings.TrimPrefix(filepath.Base(e), "tunnel-"), Host: host, Desc: desc})
	}
	return list
}
