package fill

import (
	"path/filepath"
	"strings"
)

// Vhost: что nginx отдаёт с этого сервера.
type Vhost struct {
	Names []string
	Root  string
	TLS   bool
}

// ParseNginx вытаскивает из конфигов server_name, root и наличие ssl по блокам server.
// Полноценный разбор языка nginx не нужен: интересны три директивы и вложенность фигурных скобок.
func ParseNginx(conf string) []Vhost {
	var out []Vhost
	var cur *Vhost
	depth, serverAt := 0, -1
	for _, raw := range strings.Split(conf, "\n") {
		line := strings.TrimSpace(raw)
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		open := strings.Count(line, "{")
		closed := strings.Count(line, "}")
		if strings.HasPrefix(line, "server") && open > 0 && serverAt < 0 {
			cur = &Vhost{}
			serverAt = depth
		}
		if cur != nil && depth == serverAt+1 {
			fields := strings.Fields(strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(line, "{")), ";"))
			if len(fields) > 1 {
				switch fields[0] {
				case "server_name":
					for _, n := range fields[1:] {
						n = strings.TrimPrefix(strings.ToLower(n), "*.")
						if n != "" && n != "_" && strings.Contains(n, ".") {
							cur.Names = append(cur.Names, n)
						}
					}
				case "root":
					cur.Root = strings.Trim(fields[1], `"'`)
				case "listen":
					for _, f := range fields[1:] {
						if f == "ssl" || strings.HasSuffix(f, ":443") || f == "443" {
							cur.TLS = true
						}
					}
				}
			}
		}
		depth += open - closed
		if cur != nil && depth <= serverAt {
			if len(cur.Names) > 0 {
				out = append(out, *cur)
			}
			cur, serverAt = nil, -1
		}
	}
	return out
}

// MatchVhost ищет домен для проекта: сперва по совпадению пути, затем по имени проекта в домене.
// TLS-хосты предпочитаются: сайт обычно обслуживается по https, а http-блок только редиректит.
func MatchVhost(vhosts []Vhost, remotePath, name string) (string, bool) {
	best, bestScore := "", 0
	for _, v := range vhosts {
		score := 0
		if remotePath != "" && v.Root != "" && sameTree(v.Root, remotePath) {
			score += 10
		}
		if n := bareName(name); n != "" {
			for _, d := range v.Names {
				if strings.HasPrefix(d, n+".") || strings.Contains(d, n) {
					score += 4
					break
				}
			}
		}
		if score == 0 {
			continue
		}
		if v.TLS {
			score++
		}
		if score > bestScore {
			best, bestScore = shortest(v.Names), score
		}
	}
	return best, best != ""
}

// sameTree: один путь лежит внутри другого (root часто указывает на public внутри пути деплоя).
func sameTree(a, b string) bool {
	a, b = strings.TrimSuffix(filepath.Clean(a), "/"), strings.TrimSuffix(filepath.Clean(b), "/")
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// bareName: имя проекта без приставки группы, чтобы wv-site искался как site.
func bareName(name string) string {
	if _, after, ok := strings.Cut(name, "-"); ok && len(after) >= 3 {
		return after
	}
	return name
}

// shortest: из имён одного блока берём самое короткое - обычно это основной домен, остальное www и алиасы.
func shortest(names []string) string {
	best := names[0]
	for _, n := range names[1:] {
		if len(n) < len(best) {
			best = n
		}
	}
	return best
}
