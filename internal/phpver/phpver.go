// Package phpver: версии PHP из brew и ограничения require.php из composer.json.
package phpver

import (
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Version struct{ Major, Minor, Patch int }

// Less: v младше o.
func (v Version) Less(o Version) bool { return v.less(o) }

func (v Version) String() string { return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) }

func (v Version) less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// Parse: "8", "8.3", "8.3.12"; лишние части и суффиксы отбрасываются.
func Parse(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	parts := strings.Split(s, ".")
	var n [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		digits := strings.TrimRightFunc(parts[i], func(r rune) bool { return r < '0' || r > '9' })
		v, err := strconv.Atoi(digits)
		if err != nil {
			if i == 0 {
				return Version{}, false
			}
			break
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, true
}

// Satisfies проверяет версию против ограничения composer: ^, ~, >=, >, <=, <, =, *, || и пробел как И.
func Satisfies(v Version, constraint string) bool {
	for _, alt := range strings.Split(constraint, "||") {
		if all(v, strings.Fields(strings.ReplaceAll(alt, ",", " "))) {
			return true
		}
	}
	return false
}

func all(v Version, parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if !one(v, p) {
			return false
		}
	}
	return true
}

func one(v Version, p string) bool {
	if p == "*" {
		return true
	}
	for _, op := range []string{">=", "<=", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(p, op) {
			return cmp(v, op, strings.TrimPrefix(p, op))
		}
	}
	if strings.HasSuffix(p, ".*") {
		w, ok := Parse(strings.TrimSuffix(p, ".*"))
		return ok && v.Major == w.Major && (strings.Count(p, ".") < 2 || v.Minor == w.Minor)
	}
	return cmp(v, "=", p)
}

func cmp(v Version, op, s string) bool {
	w, ok := Parse(s)
	if !ok {
		return false
	}
	dots := strings.Count(strings.TrimSpace(s), ".")
	switch op {
	case ">=":
		return !v.less(w)
	case ">":
		return w.less(v)
	case "<=":
		return !w.less(v)
	case "<":
		return v.less(w)
	case "^":
		return !v.less(w) && v.Major == w.Major
	case "~":
		if dots >= 2 {
			return !v.less(w) && v.Major == w.Major && v.Minor == w.Minor
		}
		return !v.less(w) && v.Major == w.Major
	}
	if dots == 0 {
		return v.Major == w.Major
	}
	if dots == 1 {
		return v.Major == w.Major && v.Minor == w.Minor
	}
	return v == w
}

// Matching: подходящие версии из списка, по убыванию.
func Matching(installed []Version, constraint string) []Version {
	var out []Version
	for _, v := range installed {
		if Satisfies(v, constraint) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[j].less(out[i]) })
	return out
}

// Required читает require.php из composer.json.
func Required(composerJSON string) string {
	data, err := os.ReadFile(composerJSON)
	if err != nil {
		return ""
	}
	var c struct {
		Require map[string]string `json:"require"`
	}
	if json.Unmarshal(data, &c) != nil {
		return ""
	}
	return c.Require["php"]
}
