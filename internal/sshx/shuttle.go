package sshx

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
)

func ShuttlePidfile() string { return filepath.Join(StateDir(), "sshuttle.pid") }

// ShuttleArgs: весь IPv4 через сервер, кроме самого сервера, частных сетей и extra.
func ShuttleArgs(server, serverIP string, dns bool, extra []string, pidfile string) []string {
	args := []string{"-r", server, "0/0"}
	if dns {
		args = append(args, "--dns")
	}
	if serverIP != "" {
		args = append(args, "-x", serverIP+"/32")
	}
	// Частные сети остаются вне туннеля: локальная сеть и ресурсы корпоративного VPN.
	for _, n := range append(append([]string{}, netinfo.PrivateNets...), extra...) {
		args = append(args, "-x", n)
	}
	return append(args, "-D", "--pidfile", pidfile)
}

// ShuttlePID: pid sshuttle из tim tunnel all и жив ли он.
func ShuttlePID() (string, bool) {
	data, err := os.ReadFile(ShuttlePidfile())
	if err != nil {
		return "", false
	}
	pid := strings.TrimSpace(string(data))
	if _, err := runner.Output("ps", "-p", pid); err != nil {
		return pid, false
	}
	return pid, true
}
