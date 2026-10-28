// Package secrets: секреты в репозитории проекта - файлы, которые нельзя коммитить, в истории git и токены
// по известным шаблонам в текущих файлах. Только чтение через git; содержимое истории не перебирается -
// это долго, а файл .env в истории уже достаточный повод сменить пароли.
package secrets

import (
	"path"
	"regexp"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

var (
	// sensitive: имена файлов, которым не место в git; образцы (.env.example) - можно.
	sensitive = regexp.MustCompile(`(^|/)(\.env(\.[a-z]+)?|id_(rsa|dsa|ecdsa|ed25519)|[^/]+\.(pem|key|p12|pfx|jks|keystore)|[^/]+\.sql(\.gz)?|[^/]+\.dump|credentials\.json|service[-_]account[^/]*\.json|auth\.json|\.pgpass)$`)
	samples   = regexp.MustCompile(`\.(example|sample|dist|template|tpl|testing|test|ci)$|(^|/)(tests?|fixtures?|testdata|stubs?)/`)
	// tokens: шаблоны ключей, которые узнаются однозначно.
	tokens = []struct {
		name string
		re   string
	}{
		{"ключ AWS", `AKIA[0-9A-Z]{16}`},
		{"токен GitHub", `gh[pousr]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{60,}`},
		{"ключ Stripe", `sk_live_[A-Za-z0-9]{20,}`},
		{"токен Slack", `xox[baprs]-[A-Za-z0-9-]{10,}`},
		{"токен бота Telegram", `[0-9]{8,10}:AA[A-Za-z0-9_-]{33}`},
		{"ключ Google", `AIza[0-9A-Za-z_-]{35}`},
		{"токен npm", `npm_[A-Za-z0-9]{36}`},
		{"закрытый ключ", `-----BEGIN (RSA |OPENSSH |EC |DSA )?PRIVATE KEY-----`},
	}
)

// Finding: что нашлось и где.
type Finding struct {
	What   string `json:"what"`   // ".env", "токен GitHub"
	Where  string `json:"where"`  // файл, у токена - файл:строка
	Commit string `json:"commit"` // у файла в истории - коммит и дата
}

// Scan: файлы-секреты в истории и токены в текущих файлах.
func Scan(dir string) []Finding {
	return append(history(dir), current(dir)...)
}

// history: секретные файлы, когда-либо добавленные в любую ветку, - по разу на путь, первым коммитом.
func history(dir string) []Finding {
	out, err := runner.Output("git", "-C", dir, "log", "--all", "--diff-filter=A", "--name-only", "--format=@@%h %ad", "--date=format:%d.%m.%Y")
	if err != nil {
		return nil
	}
	return parseHistory(out)
}

func parseHistory(out string) []Finding {
	var found []Finding
	seen := map[string]bool{}
	commit := ""
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if c, ok := strings.CutPrefix(l, "@@"); ok {
			commit = c
			continue
		}
		if l == "" || !sensitive.MatchString(l) || samples.MatchString(l) {
			continue
		}
		// git log идёт от новых к старым: последний увиденный коммит пути - тот, где файл появился.
		if seen[l] {
			for i := range found {
				if found[i].Where == l {
					found[i].Commit = commit
				}
			}
			continue
		}
		seen[l] = true
		found = append(found, Finding{What: path.Base(l), Where: l, Commit: commit})
	}
	return found
}

// current: токены в отслеживаемых файлах сейчас; lock-файлы и сборки не смотрим.
func current(dir string) []Finding {
	var out []Finding
	for _, t := range tokens {
		res, _ := runner.Output("git", "-C", dir, "grep", "-nIE", "--max-count=1", t.re, "--", ".",
			":!*.lock", ":!*.min.js", ":!*.map", ":!package-lock.json", ":!vendor/", ":!node_modules/")
		for _, l := range strings.Split(strings.TrimSpace(res), "\n") {
			file, rest, ok := strings.Cut(l, ":")
			if !ok || samples.MatchString(file) {
				continue
			}
			line, _, _ := strings.Cut(rest, ":")
			out = append(out, Finding{What: t.name, Where: file + ":" + line})
			if len(out) >= 10 {
				return out
			}
		}
	}
	return out
}
