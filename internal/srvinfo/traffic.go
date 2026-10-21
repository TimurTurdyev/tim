package srvinfo

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Трафик за окно по журналам запросов: читается хвост каждого журнала, окно режется на нашей стороне -
// по ssh едут строки, а не вывод sort|uniq, зато считается всё сразу и видно, покрыл ли хвост окно.
const (
	trafficLines = 30000
	topN         = 5
)

var (
	trafficScript = `export LC_ALL=C
` + logLoop + `  echo "@@file $f"
  $R "$f"
done
echo @@end
`
	hitRe   = regexp.MustCompile(`^(\S+) .*\[(\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4})\] "([A-Z]+) (\S+)[^"]*" (\d{3}) (\d+|-)(.*)$`)
	tailRe  = regexp.MustCompile(`"([^"]*)"\s*(\d+\.\d+)?\s*$`)
	botRe   = regexp.MustCompile(`(?i)bot|crawl|spider|slurp|curl|wget|python|go-http|java/|libwww|scan`)
	logName = []string{"-frontend.access.log", "-backend.access.log", ".access.log", "_access.log", "-access.log", "access.log", ".log"}
)

// TrafficScript: хвосты журналов запросов.
func TrafficScript() string {
	return strings.ReplaceAll(trafficScript, "__LINES__", strconv.Itoa(trafficLines))
}

// Timing: сколько в среднем отвечает путь.
type Timing struct {
	Path string        `json:"path"`
	Avg  time.Duration `json:"avg"`
	N    int           `json:"n"`
}

// SiteTraffic: запросы одного журнала за окно.
type SiteTraffic struct {
	File     string        `json:"file"`
	Name     string        `json:"name"`
	Requests int           `json:"requests"`
	From     time.Time     `json:"from,omitempty"`
	Partial  bool          `json:"partial"` // хвост журнала короче окна: цифры за его часть
	Classes  [6]int        `json:"classes"` // ответы по классам, индекс - первая цифра кода
	Bytes    int64         `json:"bytes"`
	IPs      int           `json:"ips"`
	Bots     int           `json:"bots"`
	ByIP     []Count       `json:"by_ip,omitempty"`
	ByPath   []Count       `json:"by_path,omitempty"`
	Errors   []Count       `json:"errors,omitempty"`  // пути с 5xx
	Missing  []Count       `json:"missing,omitempty"` // пути с 404
	Minutes  []int         `json:"minutes,omitempty"` // запросов по минутам окна
	Timed    bool          `json:"timed"`             // в журнале есть время ответа
	P50      time.Duration `json:"p50,omitempty"`
	P95      time.Duration `json:"p95,omitempty"`
	Slow     []Timing      `json:"slow,omitempty"`
}

// Traffic: трафик сайтов сервера за окно.
type Traffic struct {
	Since time.Duration `json:"since"`
	Sites []SiteTraffic `json:"sites"`
}

// hit: одна строка журнала - только то, что нужно сводке.
type hit struct {
	at     time.Time
	ip     string
	path   string
	status int
	bytes  int64
	bot    bool
	took   time.Duration
	timed  bool
}

// ParseTraffic разбирает вывод TrafficScript: по журналу на сайт, строки вне окна отбрасываются. Журнал
// бэкенда за фронтендом панели считает те же запросы второй раз - он пропускается.
func ParseTraffic(out string, now time.Time, since time.Duration) Traffic {
	s := Sections(out)
	t := Traffic{Since: since}
	var files []string
	for name := range s {
		if f, ok := strings.CutPrefix(name, "file "); ok {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	start := now.Add(-since)
	for _, f := range files {
		if strings.HasSuffix(f, "-backend.access.log") && s["file "+strings.TrimSuffix(f, "-backend.access.log")+"-frontend.access.log"] != nil {
			continue
		}
		t.Sites = append(t.Sites, siteTraffic(f, s["file "+f], start, now))
	}
	return t
}

// siteTraffic: сводка одного журнала.
func siteTraffic(file string, lines []string, start, now time.Time) SiteTraffic {
	st := SiteTraffic{File: file, Name: siteName(file)}
	var hits []hit
	var oldest time.Time
	for _, l := range lines {
		h, ok := parseHit(l)
		if !ok {
			continue
		}
		if oldest.IsZero() || h.at.Before(oldest) {
			oldest = h.at
		}
		if h.at.Before(start) || h.at.After(now) {
			continue
		}
		hits = append(hits, h)
	}
	st.Partial = len(lines) >= trafficLines && oldest.After(start)
	if len(hits) == 0 {
		return st
	}
	st.From = oldest
	if st.From.Before(start) {
		st.From = start
	}
	st.Requests = len(hits)
	st.Minutes = make([]int, int(now.Sub(start).Minutes())+1)
	ips, paths, errors, missing := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	var took []time.Duration
	byPath := map[string]*Timing{}
	for _, h := range hits {
		st.Classes[h.status/100]++
		st.Bytes += h.bytes
		if h.bot {
			st.Bots++
		}
		ips[h.ip]++
		paths[h.path]++
		switch {
		case h.status >= 500:
			errors[h.path]++
		case h.status == 404:
			missing[h.path]++
		}
		if m := int(h.at.Sub(start).Minutes()); m >= 0 && m < len(st.Minutes) {
			st.Minutes[m]++
		}
		if h.timed {
			took = append(took, h.took)
			tm := byPath[h.path]
			if tm == nil {
				tm = &Timing{Path: h.path}
				byPath[h.path] = tm
			}
			tm.Avg += h.took
			tm.N++
		}
	}
	st.IPs = len(ips)
	st.ByIP, st.ByPath, st.Errors, st.Missing = top(ips), top(paths), top(errors), top(missing)
	if len(took) > 0 {
		st.Timed = true
		sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
		st.P50, st.P95 = took[len(took)/2], took[len(took)*95/100]
		for _, tm := range byPath {
			tm.Avg /= time.Duration(tm.N)
			st.Slow = append(st.Slow, *tm)
		}
		sort.Slice(st.Slow, func(i, j int) bool { return st.Slow[i].Avg > st.Slow[j].Avg })
		st.Slow = st.Slow[:min(topN, len(st.Slow))]
	}
	return st
}

// parseHit: строка комбинированного формата nginx или apache; время ответа - если журнал его пишет
// последним полем.
func parseHit(line string) (hit, bool) {
	m := hitRe.FindStringSubmatch(line)
	if m == nil {
		return hit{}, false
	}
	at, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[2])
	if err != nil {
		return hit{}, false
	}
	h := hit{at: at, ip: m[1], path: queryRe.ReplaceAllString(m[4], "")}
	h.status, _ = strconv.Atoi(m[5])
	h.bytes, _ = strconv.ParseInt(m[6], 10, 64)
	if tail := tailRe.FindStringSubmatch(m[7]); tail != nil {
		h.bot = botRe.MatchString(tail[1])
		if tail[2] != "" {
			sec, _ := strconv.ParseFloat(tail[2], 64)
			h.took, h.timed = time.Duration(sec*float64(time.Second)), true
		}
	}
	return h, true
}

// top: самые частые ключи.
func top(m map[string]int) []Count {
	out := make([]Count, 0, len(m))
	for k, n := range m {
		out = append(out, Count{Key: k, N: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Key < out[j].Key
	})
	return out[:min(topN, len(out))]
}

// siteName: домен из имени журнала; общий журнал зовётся по папке.
func siteName(file string) string {
	base := filepath.Base(file)
	for _, suffix := range logName {
		if n, ok := strings.CutSuffix(base, suffix); ok {
			base = n
			break
		}
	}
	if base == "" {
		return filepath.Base(filepath.Dir(file))
	}
	return base
}

// Share: доля в процентах, 0 при пустом целом.
func Share(part, whole int) int {
	if whole == 0 {
		return 0
	}
	return part * 100 / whole
}

// PerMinute: запросов в минуту за окно, с десятыми при редком трафике.
func (s SiteTraffic) PerMinute() string {
	minutes := max(len(s.Minutes), 1)
	v := float64(s.Requests) / float64(minutes)
	if v < 10 {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return fmt.Sprintf("%.0f", v)
}
