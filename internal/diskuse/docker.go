package diskuse

import (
	"strconv"
	"strings"
)

// DockerReclaim: сколько вернёт docker system prune -a (образы, остановленные контейнеры, кэш сборки) и
// сколько лежит в томах без контейнеров - их prune без --volumes не трогает, а там бывают данные баз.
// out - docker system df --format '{{.Type}}\t{{.Reclaimable}}'.
func DockerReclaim(out string) (prune, volumes int64) {
	for _, line := range strings.Split(out, "\n") {
		kind, val, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(val)
		if len(f) == 0 {
			continue
		}
		n := dockerSize(f[0])
		if kind == "Local Volumes" {
			volumes += n
		} else {
			prune += n
		}
	}
	return prune, volumes
}

// dockerSize: размер в записи docker - десятичные единицы: 136.3MB, 12.5kB, 0B.
func dockerSize(s string) int64 {
	mult := map[string]float64{"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i <= 0 {
		return 0
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	return int64(f * mult[s[i:]])
}
