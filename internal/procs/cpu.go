package procs

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// PsAll: ps по всем процессам. -A понимают и BSD ps, и procps; -Axo procps не принимает.
func PsAll(format string) []string { return []string{"-A", "-o", format} }

// clockTicks: USER_HZ в Linux, на всех распространённых сборках 100.
const clockTicks = 100

// RecentCPU: загрузка процессов за последний интервал, процент одного ядра. Только Linux:
// там pcpu в ps - среднее за всю жизнь процесса, и давно запущенный простаивающий процесс
// выглядит занятым, а свежий пожиратель - нет. На macOS ps сам даёт недавнюю загрузку - nil.
func RecentCPU(d time.Duration) map[string]float64 {
	if runtime.GOOS != "linux" {
		return nil
	}
	before := procTicks()
	time.Sleep(d)
	after := procTicks()
	out := map[string]float64{}
	for pid, t := range after {
		if t0, ok := before[pid]; ok && t >= t0 {
			out[pid] = float64(t-t0) / clockTicks / d.Seconds() * 100
		}
	}
	return out
}

func procTicks() map[string]uint64 {
	files, _ := filepath.Glob("/proc/[0-9]*/stat")
	m := make(map[string]uint64, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if t, ok := ParseStatTicks(string(data)); ok {
			m[filepath.Base(filepath.Dir(f))] = t
		}
	}
	return m
}

// ParseStatTicks: utime+stime из /proc/PID/stat. Имя процесса в скобках может содержать
// пробелы и скобки, поэтому поля считаются от последней ")".
func ParseStatTicks(stat string) (uint64, bool) {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(stat[i+1:])
	if len(f) < 13 {
		return 0, false
	}
	u, err1 := strconv.ParseUint(f[11], 10, 64)
	s, err2 := strconv.ParseUint(f[12], 10, 64)
	return u + s, err1 == nil && err2 == nil
}

// Cwds: рабочие папки процессов, nil - всех. На Linux из /proc, на macOS через lsof.
func Cwds(pids []string) map[string]string {
	if runtime.GOOS == "linux" {
		if pids == nil {
			dirs, _ := filepath.Glob("/proc/[0-9]*")
			for _, d := range dirs {
				pids = append(pids, filepath.Base(d))
			}
		}
		m := map[string]string{}
		for _, pid := range pids {
			if dir, err := os.Readlink("/proc/" + pid + "/cwd"); err == nil {
				m[pid] = dir
			}
		}
		return m
	}
	args := []string{"-a", "-d", "cwd", "-Fpn"}
	if pids != nil {
		args = append(args, "-p", strings.Join(pids, ","))
	}
	out, _ := runner.Output("lsof", args...)
	return ParseCwd(out)
}
