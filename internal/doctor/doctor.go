// Package doctor: поиск тихих поломок окружения - PATH, менеджеры версий.
package doctor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func isExec(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// PathIssues: каталоги, встреченные в PATH повторно, и каталоги, которых нет.
func PathIssues(dirs []string) (dups, missing []string) {
	seen := map[string]bool{}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		c := filepath.Clean(d)
		if seen[c] {
			dups = append(dups, d)
			continue
		}
		seen[c] = true
		if st, err := os.Stat(c); err != nil || !st.IsDir() {
			missing = append(missing, d)
		}
	}
	return dups, missing
}

// Shadowed: для каждой программы, найденной больше чем в одном каталоге, все её пути по порядку PATH.
// Ссылки на один и тот же файл считаются одним путём.
func Shadowed(dirs, names []string) map[string][]string {
	out := map[string][]string{}
	for _, n := range names {
		var paths []string
		real := map[string]bool{}
		for _, d := range dirs {
			p := filepath.Join(d, n)
			if !isExec(p) {
				continue
			}
			r, err := filepath.EvalSymlinks(p)
			if err != nil {
				r = p
			}
			if real[r] {
				continue
			}
			real[r] = true
			paths = append(paths, p)
		}
		if len(paths) > 1 {
			out[n] = paths
		}
	}
	return out
}

// Manager: менеджер версий и языки, которые он сейчас ведёт.
type Manager struct {
	Name  string
	Langs []string
}

var single = map[string][]string{
	"pyenv": {"python"}, "nvm": {"node"}, "fnm": {"node"}, "volta": {"node"}, "rbenv": {"ruby"}, "goenv": {"go"},
}

var envHints = map[string]string{
	"PYENV_ROOT": "pyenv", "NVM_DIR": "nvm", "FNM_DIR": "fnm", "VOLTA_HOME": "volta", "RBENV_ROOT": "rbenv", "GOENV_ROOT": "goenv",
}

// Managers находит менеджеры по каталогам PATH и переменным окружения.
// Языки mise и asdf берутся из каталогов вида .../installs/<язык>/... в PATH.
func Managers(dirs []string, env map[string]string) []Manager {
	found := map[string]map[string]bool{}
	add := func(m, lang string) {
		if found[m] == nil {
			found[m] = map[string]bool{}
		}
		if lang != "" {
			found[m][lang] = true
		}
	}
	for _, d := range dirs {
		s := filepath.ToSlash(d)
		for m, langs := range single {
			if strings.Contains(s, "/."+m+"/") || strings.HasSuffix(s, "/."+m) || strings.Contains(s, "/"+m+"/shims") {
				for _, l := range langs {
					add(m, l)
				}
			}
		}
		for _, m := range []string{"mise", "asdf"} {
			if !strings.Contains(s, "/"+m+"/") && !strings.Contains(s, "/."+m+"/") {
				continue
			}
			add(m, "")
			if i := strings.Index(s, "/installs/"); i >= 0 {
				lang, _, _ := strings.Cut(s[i+len("/installs/"):], "/")
				add(m, lang)
			}
		}
	}
	for k, m := range envHints {
		if env[k] != "" {
			for _, l := range single[m] {
				add(m, l)
			}
		}
	}
	var out []Manager
	for m, langs := range found {
		mg := Manager{Name: m}
		for l := range langs {
			mg.Langs = append(mg.Langs, l)
		}
		sort.Strings(mg.Langs)
		out = append(out, mg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Conflicts: языки, которые ведут два и больше менеджера.
func Conflicts(ms []Manager) map[string][]string {
	by := map[string][]string{}
	for _, m := range ms {
		for _, l := range m.Langs {
			by[l] = append(by[l], m.Name)
		}
	}
	out := map[string][]string{}
	for l, names := range by {
		if len(names) > 1 {
			out[l] = names
		}
	}
	return out
}
