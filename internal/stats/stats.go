// Package stats: журнал замеров машины или сервера, график в терминале и прогноз места.
package stats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Sample: один замер. Нули - "не измерено", а не "ноль".
type Sample struct {
	At        time.Time `json:"at"`
	DiskFree  int64     `json:"disk_free"`
	DiskTotal int64     `json:"disk_total"`
	MemUsed   int64     `json:"mem_used,omitempty"`
	MemTotal  int64     `json:"mem_total,omitempty"`
	Load      float64   `json:"load,omitempty"`
	Battery   int       `json:"battery,omitempty"`
}

// Keep: сколько хранить замеры.
const Keep = 90 * 24 * time.Hour

// Dir: где лежат журналы.
func Dir() string {
	return paths.State("stats")
}

// Path: журнал по имени: local для этой машины, ssh-алиас для сервера.
func Path(name string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, name)
	return filepath.Join(Dir(), safe+".jsonl")
}

// Load читает журнал; битые строки пропускает.
func Load(path string) []Sample {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []Sample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Sample
		if json.Unmarshal(sc.Bytes(), &s) == nil && !s.At.IsZero() {
			out = append(out, s)
		}
	}
	return out
}

// Append дописывает замер и раз в сутки переписывает журнал без старого.
func Append(path string, s Sample) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	all := append(Load(path), s)
	if len(all) > 1 && all[0].At.Before(s.At.Add(-Keep-24*time.Hour)) {
		var b strings.Builder
		for _, x := range Since(all, s.At.Add(-Keep)) {
			line, _ := json.Marshal(x)
			b.Write(line)
			b.WriteByte('\n')
		}
		return runner.WriteAtomic(path, []byte(b.String()), 0o600)
	}
	line, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Since: замеры не раньше from.
func Since(list []Sample, from time.Time) []Sample {
	var out []Sample
	for _, s := range list {
		if !s.At.Before(from) {
			out = append(out, s)
		}
	}
	return out
}

// Forecast: скорость изменения свободного места в байтах в сутки по линейной регрессии
// и сколько суток до нуля. ok = false, если данных мало: меньше суток или меньше 6 замеров.
// Регрессия, а не разница первого и последнего: один большой скачок (скачал образ, почистил кеш)
// не должен переворачивать прогноз.
func Forecast(list []Sample) (perDay float64, daysLeft float64, ok bool) {
	var pts []Sample
	for _, s := range list {
		if s.DiskFree > 0 {
			pts = append(pts, s)
		}
	}
	if len(pts) < 6 || pts[len(pts)-1].At.Sub(pts[0].At) < 24*time.Hour {
		return 0, 0, false
	}
	t0 := pts[0].At
	var sx, sy, sxx, sxy float64
	n := float64(len(pts))
	for _, p := range pts {
		x := p.At.Sub(t0).Hours() / 24
		y := float64(p.DiskFree)
		sx, sy, sxx, sxy = sx+x, sy+y, sxx+x*x, sxy+x*y
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, 0, false
	}
	perDay = (n*sxy - sx*sy) / den
	last := float64(pts[len(pts)-1].DiskFree)
	if perDay >= 0 {
		return perDay, math.Inf(1), true
	}
	return perDay, last / -perDay, true
}

// ForecastLine: прогноз словами и насколько он тревожен; ok = false, если замеров мало.
func ForecastLine(list []Sample) (text string, lvl level.Level, ok bool) {
	perDay, left, ok := Forecast(list)
	switch {
	case !ok:
		return "", level.OK, false
	case math.IsInf(left, 1) || perDay > -float64(100<<20):
		return "место не убывает, " + signedBytes(perDay) + " в сутки", level.OK, true
	}
	text = fmt.Sprintf("уходит %s в сутки, кончится через %.0f дн", ui.Bytes(int64(-perDay)), left)
	switch {
	case left < 14:
		return text, level.Bad, true
	case left < 60:
		return text, level.Warn, true
	}
	return text, level.OK, true
}

func signedBytes(v float64) string {
	if v < 0 {
		return "-" + ui.Bytes(int64(-v))
	}
	return "+" + ui.Bytes(int64(v))
}

// Sparkline: строка из столбиков разной высоты (U+2581-U+2588) шириной width по значениям. Пустые значения
// (нули) пропускаются - это "не измерено", остальное усредняется по корзинам.
func Sparkline(values []float64, width int) string {
	var vals []float64
	for _, v := range values {
		if v > 0 {
			vals = append(vals, v)
		}
	}
	return Bars(vals, width)
}

// Bars: то же, но нули - настоящие нули: минуты без запросов рисуются провалом.
func Bars(vals []float64, width int) string {
	bars := []rune{'\u2581', '\u2582', '\u2583', '\u2584', '\u2585', '\u2586', '\u2587', '\u2588'}
	if len(vals) == 0 || width <= 0 {
		return ""
	}
	if len(vals) > width {
		buckets := make([]float64, width)
		for i := range buckets {
			from, to := i*len(vals)/width, (i+1)*len(vals)/width
			var sum float64
			for _, v := range vals[from:to] {
				sum += v
			}
			buckets[i] = sum / float64(to-from)
		}
		vals = buckets
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	// Шкала не уже 2% от значения: иначе колебание в килобайт на диске в сотни гигабайт
	// рисуется как обрыв с верха до низа.
	if span := hi * 0.02; hi-lo < span {
		mid := (hi + lo) / 2
		lo, hi = mid-span/2, mid+span/2
	}
	out := make([]rune, len(vals))
	for i, v := range vals {
		k := 0
		if hi > lo {
			k = int((v - lo) / (hi - lo) * float64(len(bars)-1))
		}
		out[i] = bars[k]
	}
	return string(out)
}
