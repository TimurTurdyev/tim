package procs

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ParseArgs разбирает ps -Axo pid=,args=: командная строка каждого процесса.
func ParseArgs(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		pid, args, ok := strings.Cut(line, " ")
		if ok {
			m[pid] = strings.TrimSpace(args)
		}
	}
	return m
}

func argAfter(args, flag string) string {
	f := strings.Fields(args)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == flag {
			return f[i+1]
		}
	}
	return ""
}

// Describe: понятное имя процесса, который по имени файла не узнать. По имени файла видно только
// qemu-system-aarch64-headless, а по аргументам - что это Android-эмулятор конкретного телефона.
// ok=false - процесс обычный, имя даёт AppName.
func Describe(comm, args string) (name, note string, ok bool) {
	base := filepath.Base(comm)
	switch {
	case strings.HasPrefix(base, "qemu-system"):
		if avd := argAfter(args, "-avd"); avd != "" {
			name = "Android-эмулятор " + avd
		} else {
			name = "виртуальная машина qemu"
		}
		if strings.Contains(args, "-no-window") || strings.Contains(base, "headless") {
			note = "без окна"
		}
		return name, note, true
	case strings.Contains(args, "GradleDaemon") || strings.Contains(args, "org.gradle.launcher.daemon"):
		return "демон Gradle", "живёт после сборки", true
	case strings.Contains(args, "KotlinCompileDaemon"):
		return "демон Kotlin", "живёт после сборки", true
	case base == "mds_stores" || strings.HasPrefix(base, "mdworker") || base == "mdbulkimport":
		return "Spotlight", "индексирует диск", true
	case base == "launchd_sim" || strings.Contains(comm, "CoreSimulator"):
		return "iOS-симулятор", "", true
	case base == "gopls":
		return "gopls", "сервер языка Go", true
	case strings.Contains(args, "intelephense"):
		return "intelephense", "сервер языка PHP", true
	case strings.Contains(args, "tsserver") || strings.Contains(args, "typescript-language-server"):
		return "tsserver", "сервер языка TypeScript", true
	case base == "com.docker.backend" || base == "com.docker.virtualization":
		return "Docker Desktop", "", true
	}
	return "", "", false
}

// Name: под каким именем показывать и складывать процесс.
func Name(comm, args string) string {
	if n, _, ok := Describe(comm, args); ok {
		return n
	}
	return AppName(comm)
}

// Hog: процесс, который давно и много ест CPU.
type Hog struct {
	Full
	Name string
	Note string
}

// Hogs: кто ест больше minCPU процентов (100 - целое ядро) дольше minAge. Такое легко забыть на дни:
// эмулятор без окна, зависший скрипт, демон сборки. Системные процессы не в счёт - с ними ничего не сделать.
func Hogs(list []Full, minCPU float64, minAge time.Duration) []Hog {
	var out []Hog
	for _, p := range list {
		if p.CPU < minCPU || p.Uptime < minAge || Protected(p) || p.UID == 0 {
			continue
		}
		name, note, ok := Describe(p.Comm, p.Args)
		if !ok {
			name = AppName(p.Comm)
		}
		out = append(out, Hog{Full: p, Name: name, Note: note})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPU > out[j].CPU })
	return out
}

// Cores: доля CPU в ядрах для человека: 489% - "почти 5 ядер".
func Cores(cpu float64) string {
	n := cpu / 100
	if n < 1.5 {
		return "ядро"
	}
	whole := int(n + 0.5)
	prefix := ""
	if frac := n - float64(int(n)); frac > 0.7 {
		whole, prefix = int(n)+1, "почти "
	}
	return prefix + strconv.Itoa(whole) + " " + coresWord(whole)
}

func coresWord(n int) string {
	switch {
	case n%100 >= 11 && n%100 <= 14:
		return "ядер"
	case n%10 == 1:
		return "ядро"
	case n%10 >= 2 && n%10 <= 4:
		return "ядра"
	}
	return "ядер"
}
