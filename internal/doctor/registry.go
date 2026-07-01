package doctor

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
)

// Finding: замечание к реестру; Bad - сломано, иначе - стоит поправить.
type Finding struct {
	Bad  bool
	Text string
}

// Registry: что не так в реестре проектов - папки, повторы имён и портов, неизвестные ssh-алиасы,
// цели обхода и задачи each без нужных полей. aliases - алиасы из ~/.ssh/config.
func Registry(c *config.Config, aliases map[string]bool) []Finding {
	var out []Finding
	warn := func(s string) { out = append(out, Finding{Text: s}) }
	bad := func(s string) { out = append(out, Finding{Bad: true, Text: s}) }
	names, dirs, ports := map[string]int{}, map[string]string{}, map[int][]string{}
	for i := range c.Projects {
		p := &c.Projects[i]
		names[p.Name]++
		dir := filepath.Clean(c.ProjectDir(p))
		if other, ok := dirs[dir]; ok {
			warn(p.Name + " и " + other + " указывают на одну папку")
		}
		dirs[dir] = p.Name
		if _, err := os.Stat(dir); err != nil {
			bad(p.Name + ": папки нет " + dir + " (tim proj rm " + p.Name + ")")
		}
		if p.Port > 0 {
			ports[p.Port] = append(ports[p.Port], p.Name)
		}
		if p.SSH != "" && !strings.Contains(p.SSH, "@") && !aliases[p.SSH] {
			warn(p.Name + ": ssh-алиаса " + p.SSH + " нет в ~/.ssh/config")
		}
	}
	for _, n := range sortedKeys(names) {
		if names[n] > 1 {
			bad("имя " + n + " встречается " + strconv.Itoa(names[n]) + " раза")
		}
	}
	portList := make([]int, 0, len(ports))
	for p := range ports {
		portList = append(portList, p)
	}
	sort.Ints(portList)
	for _, port := range portList {
		if ps := ports[port]; len(ps) > 1 {
			warn("порт " + strconv.Itoa(port) + " у нескольких: " + strings.Join(ps, " "))
		}
	}
	for _, h := range c.Hosts {
		if _, err := netinfo.ParseTarget(h.Addr); err != nil {
			bad("цель обхода " + h.Addr + ": " + err.Error())
		}
	}
	for _, t := range c.Tasks {
		if t.Name == "" || t.Marker == "" || t.Run == "" {
			bad("задача each без name, marker или run: " + t.Name)
		}
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
