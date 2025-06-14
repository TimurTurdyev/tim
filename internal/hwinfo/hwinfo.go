// Package hwinfo: железо машины и его загрузка - процессор, видеокарта, экраны, память, диск, тепло.
// Только разбор вывода системных утилит; запуск - в cmd.
package hwinfo

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// GPU: видеокарта и экраны на ней.
type GPU struct {
	Model    string
	Cores    int
	Metal    string
	Displays []Display
}

// Display: экран.
type Display struct {
	Name     string
	Pixels   string // физическое разрешение
	Looks    string // как его видит система: "1728 x 1117 @ 120.00Hz"
	Main     bool
	Internal bool
}

// ParseDisplays разбирает system_profiler SPDisplaysDataType -json.
func ParseDisplays(data []byte) []GPU {
	var doc struct {
		Items []struct {
			Model string `json:"sppci_model"`
			Name  string `json:"_name"`
			Cores string `json:"sppci_cores"`
			Metal string `json:"spdisplays_mtlgpufamilysupport"`
			Ndrvs []struct {
				Name       string `json:"_name"`
				Pixels     string `json:"_spdisplays_pixels"`
				Resolution string `json:"_spdisplays_resolution"`
				Main       string `json:"spdisplays_main"`
				Conn       string `json:"spdisplays_connection_type"`
			} `json:"spdisplays_ndrvs"`
		} `json:"SPDisplaysDataType"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	var out []GPU
	for _, it := range doc.Items {
		g := GPU{Model: it.Model, Metal: metalName(it.Metal)}
		if g.Model == "" {
			g.Model = it.Name
		}
		g.Cores, _ = strconv.Atoi(it.Cores)
		for _, d := range it.Ndrvs {
			g.Displays = append(g.Displays, Display{Name: d.Name, Pixels: d.Pixels, Looks: d.Resolution,
				Main: d.Main == "spdisplays_yes", Internal: d.Conn == "spdisplays_internal"})
		}
		out = append(out, g)
	}
	return out
}

func metalName(s string) string {
	s = strings.TrimPrefix(s, "spdisplays_")
	if n, ok := strings.CutPrefix(s, "metal"); ok && n != "" {
		return "Metal " + n
	}
	return ""
}

// GPULoad: загрузка видеокарты из ioreg IOAccelerator.
type GPULoad struct {
	Device   int // процент, как в Мониторе системы
	Renderer int
	InUse    int64 // байты видеопамяти в работе
	Valid    bool
}

var perfStat = regexp.MustCompile(`"([^"]+)"=(\d+)`)

// ParseGPULoad разбирает ioreg -r -d 1 -w 0 -c IOAccelerator.
func ParseGPULoad(out string) GPULoad {
	i := strings.Index(out, `"PerformanceStatistics" = {`)
	if i < 0 {
		return GPULoad{}
	}
	body := out[i:]
	if j := strings.Index(body, "}"); j > 0 {
		body = body[:j]
	}
	var g GPULoad
	for _, m := range perfStat.FindAllStringSubmatch(body, -1) {
		n, _ := strconv.ParseInt(m[2], 10, 64)
		switch m[1] {
		case "Device Utilization %":
			g.Device, g.Valid = int(n), true
		case "Renderer Utilization %":
			g.Renderer = int(n)
		case "In use system memory":
			g.InUse = n
		}
	}
	return g
}

// GPUClient: процесс, который держит видеокарту открытой.
type GPUClient struct {
	PID  string
	Name string
}

var clientRe = regexp.MustCompile(`"IOUserClientCreator" = "pid (\d+), ([^"]+)"`)

// ParseGPUClients разбирает ioreg -r -c AGXDeviceUserClient: кто открыл видеокарту. Сколько каждый
// её грузит, без root не узнать - только кто держит.
func ParseGPUClients(out string) []GPUClient {
	seen := map[string]bool{}
	var list []GPUClient
	for _, m := range clientRe.FindAllStringSubmatch(out, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			list = append(list, GPUClient{PID: m[1], Name: strings.TrimSpace(m[2])})
		}
	}
	return list
}

// Swap: занятый своп, байты.
type Swap struct{ Total, Used int64 }

var swapRe = regexp.MustCompile(`total = ([\d.,]+)M\s+used = ([\d.,]+)M`)

// ParseSwap разбирает sysctl -n vm.swapusage; дробная часть бывает с запятой в русской локали.
func ParseSwap(out string) (Swap, bool) {
	m := swapRe.FindStringSubmatch(out)
	if m == nil {
		return Swap{}, false
	}
	mb := func(s string) int64 {
		f, _ := strconv.ParseFloat(strings.Replace(s, ",", ".", 1), 64)
		return int64(f * (1 << 20))
	}
	return Swap{Total: mb(m[1]), Used: mb(m[2])}, true
}

// ParseFreePercent разбирает последнюю строку memory_pressure.
func ParseFreePercent(out string) (int, bool) {
	i := strings.LastIndex(out, "free percentage:")
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(out[i+len("free percentage:"):])
	if len(f) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(f[0], "%"))
	return n, err == nil
}

// ParseTherm: предупреждение о перегреве из pmset -g therm; пусто - ограничений нет.
func ParseTherm(out string) string {
	var warn []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Note:") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if k == "CPU_Speed_Limit" && v != "100" {
				warn = append(warn, "процессор ограничен до "+v+"%")
			}
			continue
		}
		if strings.Contains(strings.ToLower(line), "warning level") {
			warn = append(warn, line)
		}
	}
	return strings.Join(warn, "; ")
}

// Disk: что известно о системном диске.
type Disk struct {
	SSD      bool
	Protocol string
	SMART    string
}

// ParseDiskutil разбирает diskutil info /.
func ParseDiskutil(out string) Disk {
	var d Disk
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Solid State":
			d.SSD = v == "Yes"
		case "Protocol":
			d.Protocol = v
		case "SMART Status":
			d.SMART = v
		}
	}
	return d
}

// ParseIostatCPU: занятость процессора за последний интервал iostat -c 2 -w 1, в процентах.
// Первая строка данных - среднее с загрузки, поэтому берём последнюю.
func ParseIostatCPU(out string) (int, bool) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	header := -1
	for _, l := range lines {
		f := strings.Fields(l)
		for j, w := range f {
			if w == "id" {
				header = j
			}
		}
	}
	if header < 0 || len(lines) < 3 {
		return 0, false
	}
	f := strings.Fields(lines[len(lines)-1])
	if header >= len(f) {
		return 0, false
	}
	idle, err := strconv.Atoi(f[header])
	if err != nil {
		return 0, false
	}
	return 100 - idle, true
}
