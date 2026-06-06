// Package hook: приёмник вебхуков - принять, показать разобранным, сохранить и повторить.
package hook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"
)

// MaxBody: больше вебхуки не бывают, а память приёмника не резиновая.
const MaxBody = 4 << 20

// Keep: сколько последних запросов держать в журнале.
const Keep = 200

// skipHeaders: то, что добавляет любой клиент и прокси, - шум при чтении.
var skipHeaders = map[string]bool{"Accept-Encoding": true, "Connection": true, "Content-Length": true,
	"X-Forwarded-For": true, "X-Forwarded-Proto": true, "Cf-Ray": true, "Cf-Visitor": true,
	"Cf-Connecting-Ip": true, "Cdn-Loop": true, "Cf-Ipcountry": true, "Cf-Warp-Tag-Id": true}

// Record: принятый запрос.
type Record struct {
	N       int         `json:"n"`
	At      time.Time   `json:"at"`
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Remote  string      `json:"remote"`
	Headers http.Header `json:"headers"`
	Body    []byte      `json:"body"`
}

// Journal: файл с последними запросами, по одному JSON на строку.
type Journal struct {
	Path string
	mu   sync.Mutex
}

func DefaultPath() string {
	return paths.State("hooks.jsonl")
}

func (j *Journal) Load() []Record {
	f, err := os.Open(j.Path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 2*MaxBody)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return out
}

// Add дописывает запрос, присваивает номер и обрезает журнал до Keep.
func (j *Journal) Add(r Record) (Record, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	all := j.Load()
	r.N = 1
	if len(all) > 0 {
		r.N = all[len(all)-1].N + 1
	}
	all = append(all, r)
	if len(all) > Keep {
		all = all[len(all)-Keep:]
	}
	var b bytes.Buffer
	for _, x := range all {
		line, err := json.Marshal(x)
		if err != nil {
			return r, err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(j.Path), 0o755); err != nil {
		return r, err
	}
	return r, os.WriteFile(j.Path, b.Bytes(), 0o600)
}

// Find: запрос по номеру.
func (j *Journal) Find(n int) (Record, bool) {
	for _, r := range j.Load() {
		if r.N == n {
			return r, true
		}
	}
	return Record{}, false
}

// Handler принимает всё подряд, отвечает status и body и отдаёт запрос в got.
func Handler(status int, body []byte, got func(Record)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, _ := io.ReadAll(io.LimitReader(req.Body, MaxBody))
		got(Record{At: time.Now(), Method: req.Method, Path: req.URL.RequestURI(), Remote: req.RemoteAddr,
			Headers: req.Header.Clone(), Body: data})
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

// Pretty: запрос в читаемом виде - важные заголовки и тело, JSON с отступами, форма по полям.
func Pretty(r Record) string {
	var b strings.Builder
	keys := make([]string, 0, len(r.Headers))
	for k := range r.Headers {
		if !skipHeaders[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("  " + k + ": " + strings.Join(r.Headers[k], ", ") + "\n")
	}
	if len(r.Body) == 0 {
		return b.String()
	}
	b.WriteString("\n")
	ct := r.Headers.Get("Content-Type")
	switch {
	case strings.Contains(ct, "json") || json.Valid(r.Body):
		var out bytes.Buffer
		if json.Indent(&out, r.Body, "  ", "  ") == nil {
			b.WriteString("  " + out.String() + "\n")
			return b.String()
		}
	case strings.Contains(ct, "x-www-form-urlencoded"):
		if vals, err := url.ParseQuery(string(r.Body)); err == nil {
			names := make([]string, 0, len(vals))
			for k := range vals {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				b.WriteString("  " + k + " = " + strings.Join(vals[k], ", ") + "\n")
			}
			return b.String()
		}
	}
	text := string(r.Body)
	if len(text) > 2000 {
		text = text[:2000] + "\n  ..."
	}
	b.WriteString("  " + strings.ReplaceAll(text, "\n", "\n  ") + "\n")
	return b.String()
}

// Replay повторяет запрос по адресу base с тем же методом, путём, заголовками и телом.
func Replay(r Record, base string, client *http.Client) (*http.Response, error) {
	u := strings.TrimSuffix(base, "/") + r.Path
	req, err := http.NewRequest(r.Method, u, bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	for k, v := range r.Headers {
		if k == "Host" || k == "Content-Length" {
			continue
		}
		req.Header[k] = v
	}
	return client.Do(req)
}
