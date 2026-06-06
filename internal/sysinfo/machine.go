package sysinfo

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var bootRe = regexp.MustCompile(`sec = (\d+)`)

var pageRe = regexp.MustCompile(`page size of (\d+) bytes`)

var battRe = regexp.MustCompile(`(\d+)%;\s*([^;]+);\s*(\S+)?`)

// ParseBoottime: sysctl -n kern.boottime -> время загрузки.
func ParseBoottime(out string) (time.Time, bool) {
	m := bootRe.FindStringSubmatch(out)
	if m == nil {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	return time.Unix(n, 0), err == nil
}

// ParseLoadavg: "{ 2,12 1,85 1,39 }" с sysctl или "0.52 0.58 0.59 1/389 12345" из /proc/loadavg -> три числа.
func ParseLoadavg(out string) []float64 {
	var vals []float64
	for _, f := range strings.Fields(strings.Trim(strings.TrimSpace(out), "{}")) {
		v, err := strconv.ParseFloat(strings.ReplaceAll(f, ",", "."), 64)
		if err != nil || len(vals) == 3 {
			break
		}
		vals = append(vals, v)
	}
	return vals
}

// ParseVMStat: занятая память в байтах - active, wired и сжатое.
func ParseVMStat(out string) uint64 {
	page := uint64(4096)
	if m := pageRe.FindStringSubmatch(out); m != nil {
		page, _ = strconv.ParseUint(m[1], 10, 64)
	}
	var pages uint64
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "Pages active", "Pages wired down", "Pages occupied by compressor":
			n, _ := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(v), "."), 10, 64)
			pages += n
		}
	}
	return pages * page
}

// Disk: размер, занято и доступно в байтах. Доступно меньше, чем размер минус занятое:
// в APFS остальные тома (система, VM, Preboot) делят то же место.
type Disk struct{ Size, Used, Avail uint64 }

// Percent: занято в процентах так же, как считает df: от занятого плюс доступного.
func (d Disk) Percent() int {
	if d.Used+d.Avail == 0 {
		return 0
	}
	return int((d.Used*100 + d.Used + d.Avail - 1) / (d.Used + d.Avail))
}

// ParseDF: последняя строка df -k <путь>.
func ParseDF(out string) (Disk, bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	f := strings.Fields(lines[len(lines)-1])
	if len(f) < 4 {
		return Disk{}, false
	}
	size, err1 := strconv.ParseUint(f[1], 10, 64)
	used, err2 := strconv.ParseUint(f[2], 10, 64)
	avail, err3 := strconv.ParseUint(f[3], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return Disk{}, false
	}
	return Disk{Size: size * 1024, Used: used * 1024, Avail: avail * 1024}, true
}

// Battery из pmset -g batt.
type Battery struct {
	Percent   int
	State     string
	Remaining string
	AC        bool
}

func ParsePmset(out string) (Battery, bool) {
	var b Battery
	b.AC = strings.Contains(out, "'AC Power'")
	m := battRe.FindStringSubmatch(out)
	if m == nil {
		return b, false
	}
	b.Percent, _ = strconv.Atoi(m[1])
	b.State = strings.TrimSpace(m[2])
	if len(m) > 3 && strings.Contains(m[3], ":") {
		b.Remaining = m[3]
	}
	return b, true
}

// Wifi: текущая сеть из system_profiler SPAirPortDataType -json.
type Wifi struct {
	Iface, SSID, Channel, PHY, Security string
	Rate                                int
	Signal, Noise                       int
}

func ParseWifi(data []byte) (Wifi, bool) {
	var root struct {
		Data []struct {
			Ifaces []struct {
				Name    string         `json:"_name"`
				Current map[string]any `json:"spairport_current_network_information"`
			} `json:"spairport_airport_interfaces"`
		} `json:"SPAirPortDataType"`
	}
	if json.Unmarshal(data, &root) != nil {
		return Wifi{}, false
	}
	for _, d := range root.Data {
		for _, i := range d.Ifaces {
			c := i.Current
			name, _ := c["_name"].(string)
			if name == "" {
				continue
			}
			w := Wifi{Iface: i.Name, SSID: name}
			w.Channel, _ = c["spairport_network_channel"].(string)
			w.PHY, _ = c["spairport_network_phymode"].(string)
			if r, ok := c["spairport_network_rate"].(float64); ok {
				w.Rate = int(r)
			}
			sec, _ := c["spairport_security_mode"].(string)
			w.Security = strings.ReplaceAll(strings.TrimPrefix(sec, "spairport_security_mode_"), "_", " ")
			if sn, ok := c["spairport_signal_noise"].(string); ok {
				parts := strings.Split(sn, "/")
				if len(parts) == 2 {
					w.Signal, _ = strconv.Atoi(strings.Fields(parts[0])[0])
					w.Noise, _ = strconv.Atoi(strings.Fields(parts[1])[0])
				}
			}
			return w, true
		}
	}
	return Wifi{}, false
}

// Quality: оценка сигнала по разнице с шумом (SNR).
func (w Wifi) Quality() string {
	snr := w.Signal - w.Noise
	switch {
	case w.Signal == 0:
		return "неизвестно"
	case snr >= 40:
		return "отличный"
	case snr >= 25:
		return "хороший"
	case snr >= 15:
		return "слабый"
	}
	return "плохой"
}
