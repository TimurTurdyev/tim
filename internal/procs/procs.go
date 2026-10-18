// Package procs: сведения о процессах macOS - рабочая папка, время работы, команда.
package procs

import (
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/runner"
)

type Proc struct {
	PID     string
	PPID    string
	Command string
	Args    string // команда целиком, с аргументами
	RSS     int64  // память, байты
	Uptime  time.Duration
	Cwd     string
}

// ParseCwd разбирает lsof -a -d cwd -Fpn: строки p<pid> и n<путь>.
func ParseCwd(out string) map[string]string {
	m := map[string]string{}
	pid := ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid = line[1:]
		case 'n':
			if pid != "" {
				m[pid] = line[1:]
			}
		}
	}
	return m
}

// ParseEtime: [[дд-]чч:]мм:сс из ps -o etime.
func ParseEtime(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	days := 0
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var nums []int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		nums = append(nums, n)
	}
	for len(nums) < 3 {
		nums = append([]int{0}, nums...)
	}
	d := time.Duration(days)*24*time.Hour + time.Duration(nums[0])*time.Hour +
		time.Duration(nums[1])*time.Minute + time.Duration(nums[2])*time.Second
	return d, true
}

// ParsePs разбирает ps -o pid=,ppid=,rss=,etime=,comm=; rss у ps в килобайтах.
func ParsePs(out string) map[string]Proc {
	m := map[string]Proc{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		up, _ := ParseEtime(f[3])
		kb, _ := strconv.ParseInt(f[2], 10, 64)
		m[f[0]] = Proc{PID: f[0], PPID: f[1], RSS: kb * 1024, Uptime: up, Command: strings.Join(f[4:], " ")}
	}
	return m
}

// Info собирает сведения о процессах: ps, ps с командой целиком и lsof - разом.
func Info(pids []string) map[string]Proc {
	if len(pids) == 0 {
		return nil
	}
	var ps, args string
	var cwds map[string]string
	par.All(
		func() { ps = psEach(pids, "pid=,ppid=,rss=,etime=,comm=") },
		func() { args = psEach(pids, "pid=,args=") },
		func() { cwds = Cwds(pids) },
	)
	out := ParsePs(ps)
	for pid, a := range ParseArgs(args) {
		if p, ok := out[pid]; ok {
			p.Args = a
			out[pid] = p
		}
	}
	for pid, cwd := range cwds {
		p := out[pid]
		p.PID, p.Cwd = pid, cwd
		out[pid] = p
	}
	return out
}

// psEach: ps по каждому процессу отдельно и параллельно. ps -p со списком на macOS перебирает все
// процессы системы - около 50 мс, а с одним pid - 2 мс.
func psEach(pids []string, format string) string {
	outs := par.Map(pids, 8, func(pid string) string {
		out, _ := runner.Output("ps", "-o", format, "-p", pid)
		return out
	})
	return strings.Join(outs, "\n")
}

// System: процесс macOS, а не то, что запустил разработчик.
func System(command string) bool {
	for _, p := range []string{"/System/", "/usr/libexec/", "/usr/sbin/", "/sbin/"} {
		if strings.HasPrefix(command, p) {
			return true
		}
	}
	return false
}

// Started: когда запущен процесс, с точностью до секунды.
func Started(pid int) (time.Time, bool) {
	out, err := runner.Output("ps", "-o", "etime=", "-p", strconv.Itoa(pid))
	if err != nil {
		return time.Time{}, false
	}
	age, ok := ParseEtime(out)
	if !ok {
		return time.Time{}, false
	}
	return time.Now().Add(-age), true
}

// Zombies: какие из процессов уже завершились, но не прибраны родителем (state Z).
func Zombies(pids []int) map[int]bool {
	args := make([]string, len(pids))
	for i, p := range pids {
		args[i] = strconv.Itoa(p)
	}
	out, _ := runner.Output("ps", "-o", "pid=,state=", "-p", strings.Join(args, ","))
	z := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.HasPrefix(f[1], "Z") {
			if n, err := strconv.Atoi(f[0]); err == nil {
				z[n] = true
			}
		}
	}
	return z
}
