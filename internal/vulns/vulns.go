// Package vulns: известные уязвимости зависимостей проекта по lock-файлам через OSV.dev - бесплатно и без
// ключа. Наружу уходят только имена и версии пакетов; ответ хранится сутки по содержимому lock-файлов.
package vulns

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
)

var client = &http.Client{Timeout: 30 * time.Second}

// Package: зависимость из lock-файла.
type Package struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"` // Packagist, npm, Go
}

// Vuln: уязвимость в зависимости.
type Vuln struct {
	Package  Package `json:"package"`
	ID       string  `json:"id"`
	Summary  string  `json:"summary,omitempty"`
	Severity string  `json:"severity,omitempty"` // LOW, MODERATE, HIGH, CRITICAL
	Fixed    string  `json:"fixed,omitempty"`
}

// Lockfiles: зависимости из composer.lock, package-lock.json и go.mod; raw - содержимое для ключа кэша.
func Lockfiles(dir string) (pkgs []Package, raw []byte) {
	if data, err := os.ReadFile(filepath.Join(dir, "composer.lock")); err == nil {
		pkgs, raw = append(pkgs, parseComposer(data)...), append(raw, data...)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package-lock.json")); err == nil {
		pkgs, raw = append(pkgs, parseNpm(data)...), append(raw, data...)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		pkgs, raw = append(pkgs, parseGoMod(data)...), append(raw, data...)
	}
	return pkgs, raw
}

func parseComposer(data []byte) []Package {
	var raw struct {
		Packages []struct{ Name, Version string } `json:"packages"`
		Dev      []struct{ Name, Version string } `json:"packages-dev"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	var out []Package
	for _, p := range append(raw.Packages, raw.Dev...) {
		if !strings.HasPrefix(p.Version, "dev-") {
			out = append(out, Package{p.Name, strings.TrimPrefix(p.Version, "v"), "Packagist"})
		}
	}
	return out
}

func parseNpm(data []byte) []Package {
	var raw struct {
		Packages     map[string]struct{ Version string } `json:"packages"`
		Dependencies map[string]struct{ Version string } `json:"dependencies"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Package
	add := func(name, version string) {
		if name != "" && version != "" && !seen[name+"@"+version] {
			seen[name+"@"+version] = true
			out = append(out, Package{name, version, "npm"})
		}
	}
	for key, p := range raw.Packages {
		if i := strings.LastIndex(key, "node_modules/"); i >= 0 {
			add(key[i+len("node_modules/"):], p.Version)
		}
	}
	for name, p := range raw.Dependencies {
		add(name, p.Version)
	}
	return out
}

func parseGoMod(data []byte) []Package {
	var out []Package
	block := false
	for sc := bufio.NewScanner(bytes.NewReader(data)); sc.Scan(); {
		line, _, _ := strings.Cut(sc.Text(), "//")
		f := strings.Fields(line)
		switch {
		case len(f) == 2 && f[0] == "require" && f[1] == "(":
			block = true
		case block && len(f) == 1 && f[0] == ")":
			block = false
		case block && len(f) == 2:
			out = append(out, Package{f[0], f[1], "Go"})
		case len(f) == 3 && f[0] == "require":
			out = append(out, Package{f[1], f[2], "Go"})
		}
	}
	return out
}

// Check: уязвимости зависимостей проекта; ни одного lock-файла - nil без ошибки.
func Check(dir string) ([]Vuln, error) {
	pkgs, raw := Lockfiles(dir)
	if len(pkgs) == 0 {
		return nil, nil
	}
	h := sha256.Sum256(raw)
	return cache.Get(cache.Path(paths.State("osv"), hex.EncodeToString(h[:8])), 24*time.Hour, func() ([]Vuln, error) { return query(pkgs) })
}

func query(pkgs []Package) ([]Vuln, error) {
	type q struct {
		Package struct {
			Name      string `json:"name"`
			Ecosystem string `json:"ecosystem"`
		} `json:"package"`
		Version string `json:"version"`
	}
	var found []Vuln
	for start := 0; start < len(pkgs); start += 1000 {
		batch := pkgs[start:min(len(pkgs), start+1000)]
		var body struct {
			Queries []q `json:"queries"`
		}
		for _, p := range batch {
			var x q
			x.Package.Name, x.Package.Ecosystem, x.Version = p.Name, p.Ecosystem, p.Version
			body.Queries = append(body.Queries, x)
		}
		var res struct {
			Results []struct {
				Vulns []struct{ ID string } `json:"vulns"`
			} `json:"results"`
		}
		if err := post("https://api.osv.dev/v1/querybatch", body, &res); err != nil {
			return nil, err
		}
		for i, r := range res.Results {
			for _, v := range r.Vulns {
				if i < len(batch) {
					found = append(found, Vuln{Package: batch[i], ID: v.ID})
				}
			}
		}
	}
	// Подробности - по отдельному запросу на каждую; больше 20 человеку всё равно не прочитать.
	detailed := par.Map(found[:min(20, len(found))], 8, details)
	return append(detailed, found[min(20, len(found)):]...), nil
}

func details(v Vuln) Vuln {
	resp, err := client.Get("https://api.osv.dev/v1/vulns/" + v.ID)
	if err != nil {
		return v
	}
	defer func() { _ = resp.Body.Close() }()
	var d struct {
		Summary  string `json:"summary"`
		Severity struct {
			Level string `json:"severity"`
		} `json:"database_specific"`
		Affected []struct {
			Package struct{ Name string } `json:"package"`
			Ranges  []struct {
				Events []struct{ Fixed string } `json:"events"`
			} `json:"ranges"`
		} `json:"affected"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&d) != nil {
		return v
	}
	v.Summary, v.Severity = d.Summary, strings.ToUpper(d.Severity.Level)
	for _, a := range d.Affected {
		if a.Package.Name != v.Package.Name {
			continue
		}
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" && newer(e.Fixed, v.Package.Version) && (v.Fixed == "" || newer(v.Fixed, e.Fixed)) {
					v.Fixed = e.Fixed
				}
			}
		}
	}
	return v
}

// newer: версия a старше b - грубо по числам через точку; для "исправлено в" этого хватает.
func newer(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			_, _ = fmt.Sscan(pa[i], &x)
		}
		if i < len(pb) {
			_, _ = fmt.Sscan(pb[i], &y)
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func post(u string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := client.Post(u, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("osv.dev: %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(out)
}
