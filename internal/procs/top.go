package procs

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Sample: память и CPU одного процесса.
type Sample struct {
	PID  string
	RSS  int64 // байты
	CPU  float64
	Comm string
}

// ParseTop разбирает ps -Axo pid=,rss=,pcpu=,comm=. Доля CPU приходит с запятой в русской локали.
func ParseTop(out string) []Sample {
	var list []Sample
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		kb, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(strings.Replace(f[2], ",", ".", 1), 64)
		list = append(list, Sample{PID: f[0], RSS: kb * 1024, CPU: cpu, Comm: strings.Join(f[3:], " ")})
	}
	return list
}

// AppName: приложение, которому принадлежит процесс. Помощники внутри .app считаются за само
// приложение - иначе браузер расползается на десятки строк.
func AppName(comm string) string {
	if i := strings.Index(comm, ".app/"); i >= 0 {
		return filepath.Base(comm[:i])
	}
	if strings.HasSuffix(comm, ".app") {
		return strings.TrimSuffix(filepath.Base(comm), ".app")
	}
	return filepath.Base(comm)
}

// Group: сумма по ключу. Процессы с пустым ключом не учитываются.
type Group struct {
	Name  string
	RSS   int64
	CPU   float64
	Count int
}

// GroupBy складывает процессы по ключу и сортирует: по памяти или по CPU.
func GroupBy(list []Sample, key func(Sample) string, byCPU bool) []Group {
	m := map[string]*Group{}
	for _, s := range list {
		k := key(s)
		if k == "" {
			continue
		}
		g := m[k]
		if g == nil {
			g = &Group{Name: k}
			m[k] = g
		}
		g.RSS += s.RSS
		g.CPU += s.CPU
		g.Count++
	}
	out := make([]Group, 0, len(m))
	for _, g := range m {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if byCPU && out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		if out[i].RSS != out[j].RSS {
			return out[i].RSS > out[j].RSS
		}
		return out[i].Name < out[j].Name
	})
	return out
}
