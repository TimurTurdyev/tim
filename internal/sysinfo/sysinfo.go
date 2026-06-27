// Package sysinfo: быстрые сведения о машине для сводки: сервисы launchd, git, процессы.
package sysinfo

import (
	"strconv"
	"strings"
)

// Agent: сервис brew в launchd.
type Agent struct {
	Name   string
	PID    string
	Status int
}

func (a Agent) Running() bool { return a.PID != "-" && a.PID != "" }

// ParseLaunchctl оставляет только сервисы brew (homebrew.mxcl.*, sh.brew.*) из launchctl list.
func ParseLaunchctl(out string) []Agent {
	var list []Agent
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		var name string
		switch {
		case strings.HasPrefix(f[2], "homebrew.mxcl."):
			name = strings.TrimPrefix(f[2], "homebrew.mxcl.")
		case strings.HasPrefix(f[2], "sh.brew."):
			name = strings.TrimPrefix(f[2], "sh.brew.")
		default:
			continue
		}
		code, _ := strconv.Atoi(f[1])
		list = append(list, Agent{Name: name, PID: f[0], Status: code})
	}
	return list
}
