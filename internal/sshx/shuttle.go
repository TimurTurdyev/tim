package sshx

import "path/filepath"

// privateNets остаются вне туннеля: локальная сеть и ресурсы корпоративного VPN.
var privateNets = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

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
	for _, n := range append(append([]string{}, privateNets...), extra...) {
		args = append(args, "-x", n)
	}
	return append(args, "-D", "--pidfile", pidfile)
}
