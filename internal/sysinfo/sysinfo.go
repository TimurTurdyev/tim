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

// GitState: ветка и расхождения из git status -sb --porcelain.
type GitState struct {
	Branch  string
	Changes int
	Ahead   int
	Behind  int
	NoUp    bool
}

func ParseGitStatusSB(out string) GitState {
	var g GitState
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "## ") {
		return g
	}
	head := strings.TrimPrefix(lines[0], "## ")
	g.Changes = len(lines) - 1
	if strings.HasPrefix(head, "No commits yet on ") {
		g.Branch, g.NoUp = strings.TrimPrefix(head, "No commits yet on "), true
		return g
	}
	branch, rest, hasUp := strings.Cut(head, "...")
	if !hasUp {
		g.Branch, g.NoUp = strings.Fields(head)[0], true
		return g
	}
	g.Branch = branch
	if i := strings.Index(rest, "["); i >= 0 {
		for _, part := range strings.Split(strings.Trim(rest[i:], "[]"), ", ") {
			k, v, _ := strings.Cut(part, " ")
			n, _ := strconv.Atoi(v)
			switch k {
			case "ahead":
				g.Ahead = n
			case "behind":
				g.Behind = n
			}
		}
	}
	return g
}

// Proc: процесс и его доля CPU.
type Proc struct {
	CPU  float64
	Name string
}

// ParsePsTop разбирает ps -Ao pcpu=,comm= -r и отдаёт первые n с коротким именем.
func ParsePsTop(out string, n int) []Proc {
	var list []Proc
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		cpu, err := strconv.ParseFloat(strings.ReplaceAll(f[0], ",", "."), 64)
		if err != nil {
			continue
		}
		name := strings.Join(f[1:], " ")
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		list = append(list, Proc{cpu, name})
		if len(list) == n {
			break
		}
	}
	return list
}
