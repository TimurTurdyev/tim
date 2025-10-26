package hwinfo

import (
	"strconv"
	"strings"
)

// Источники Linux: /proc, /sys и nvidia-smi.

// CPUTimes: счётчики /proc/stat по всем ядрам - сколько всего и сколько в простое.
type CPUTimes struct{ Total, Idle uint64 }

// ParseProcStat: первая строка "cpu  user nice system idle iowait irq softirq steal ...".
func ParseProcStat(out string) (CPUTimes, bool) {
	line, _, _ := strings.Cut(out, "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return CPUTimes{}, false
	}
	var t CPUTimes
	for i, v := range f[1:] {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return CPUTimes{}, false
		}
		// guest и guest_nice уже входят в user и nice.
		if i >= 8 {
			break
		}
		t.Total += n
		if i == 3 || i == 4 { // idle и iowait
			t.Idle += n
		}
	}
	return t, true
}

// Busy: загрузка в процентах между двумя замерами.
func Busy(a, b CPUTimes) int {
	if b.Total <= a.Total {
		return 0
	}
	return int((b.Total - a.Total - (b.Idle - a.Idle)) * 100 / (b.Total - a.Total))
}

// ParseMeminfoSwap: SwapTotal и SwapFree из /proc/meminfo.
func ParseMeminfoSwap(out string) (Swap, bool) {
	var total, free int64
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		f := strings.Fields(v)
		if !ok || len(f) == 0 {
			continue
		}
		n, _ := strconv.ParseInt(f[0], 10, 64)
		switch k {
		case "SwapTotal":
			total = n * 1024
		case "SwapFree":
			free = n * 1024
		}
	}
	return Swap{Total: total, Used: total - free}, total > 0
}

// ParseLscpuModel: "Model name:" из lscpu; в виртуалках там бывает "-", тогда хотя бы производитель.
func ParseLscpuModel(out string) string {
	fields := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	for _, k := range []string{"Model name", "Vendor ID"} {
		if v := fields[k]; v != "" && v != "-" {
			return v
		}
	}
	return ""
}

// NvidiaGPU: видеокарта из nvidia-smi.
type NvidiaGPU struct {
	Name              string
	Util              int
	MemUsed, MemTotal int64 // байты
}

// ParseNvidiaSmi: nvidia-smi --query-gpu=name,utilization.gpu,memory.used,memory.total --format=csv,noheader,nounits.
func ParseNvidiaSmi(out string) []NvidiaGPU {
	var list []NvidiaGPU
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, ",")
		if len(f) < 4 {
			continue
		}
		util, _ := strconv.Atoi(strings.TrimSpace(f[1]))
		used, _ := strconv.ParseInt(strings.TrimSpace(f[2]), 10, 64)
		total, _ := strconv.ParseInt(strings.TrimSpace(f[3]), 10, 64)
		list = append(list, NvidiaGPU{Name: strings.TrimSpace(f[0]), Util: util, MemUsed: used << 20, MemTotal: total << 20})
	}
	return list
}

// HotZone: самая горячая зона из /sys/class/thermal, градусы; temps - содержимое файлов temp в миллиградусах.
func HotZone(temps []string) int {
	hot := 0
	for _, t := range temps {
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && n/1000 > hot {
			hot = n / 1000
		}
	}
	return hot
}
