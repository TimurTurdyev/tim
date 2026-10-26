package srvinfo

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
)

// deepScript: глубокая проверка - читает файлы, поэтому только по просьбе. SUID-файлы для сравнения
// с прошлым разом, php в папках загрузок и с приметами веб-оболочек в папках сайтов, изменённые
// файлы пакетов ssh, ps, ss, ls. Всё под nice и ionice, файлов сайтов не больше 5000.
const deepScript = `export LC_ALL=C
S=; sudo -n true 2>/dev/null && S="sudo -n"
IO=; command -v ionice >/dev/null 2>&1 && IO="ionice -c3"
echo @@suid; if [ -n "$S" ] || [ "$(id -u)" = 0 ]; then $S nice -n 19 $IO find / -xdev -perm -4000 -type f 2>/dev/null | sort; else echo nosudo; fi
echo @@php
L=$(mktemp /tmp/tim-deep.XXXXXX) || exit 1
trap 'rm -f "$L"' EXIT
for d in /var/www /srv /home; do [ -d "$d" ] && $S nice -n 19 $IO find "$d" -xdev -type f -name '*.php' \( -path '*/upload*' -o -mtime -7 \) -size -2M 2>/dev/null; done | head -5000 > "$L"
grep -E '/uploads?/' "$L" | head -50 | sed 's/^/upload /'
echo "changed $(wc -l < "$L")"
tr '\n' '\0' < "$L" | $S xargs -0 -r nice -n 19 grep -lE 'eval[[:space:]]*\((base64_decode|gzinflate|gzuncompress|str_rot13)|(assert|eval)[[:space:]]*\([[:space:]]*\$_(POST|GET|REQUEST|COOKIE)|\$_(POST|GET|REQUEST|COOKIE)[[:space:]]*\[[^]]*\][[:space:]]*\(' 2>/dev/null | head -50 | sed 's/^/sig /'
echo @@dpkg; if command -v dpkg >/dev/null 2>&1; then $S dpkg --verify openssh-server openssh-client procps iproute2 coreutils util-linux login passwd 2>/dev/null | grep -vE '^\S+ +c ' ; [ -z "$S" ] && echo nosudo; else echo nodpkg; fi
echo @@end
`

// Deep: итог глубокой проверки.
type Deep struct {
	At         time.Time `json:"at"`
	SUID       []string  `json:"suid,omitempty"`
	UploadPHP  []string  `json:"upload_php,omitempty"`
	Signatures []string  `json:"signatures,omitempty"`
	Changed    int       `json:"changed"` // php в загрузках и изменённых за неделю
	Modified   []string  `json:"modified,omitempty"`
	NoDpkg     bool      `json:"no_dpkg"`
	NoSudo     bool      `json:"no_sudo"`
}

// CollectDeep: глубокая проверка по ssh; на большом сервере идёт минуту-две.
func CollectDeep(target string) (Deep, error) {
	out, err := remote(target, deepScript, 15*time.Second, 5*time.Minute)
	if err != nil {
		return Deep{}, err
	}
	return ParseDeep(out, time.Now()), nil
}

// ParseDeep разбирает вывод глубокой проверки.
func ParseDeep(out string, now time.Time) Deep {
	s := Sections(out)
	d := Deep{At: now, SUID: s["suid"]}
	// Без root видна только часть SUID-файлов: такой список сравнивать нельзя.
	if len(d.SUID) == 1 && d.SUID[0] == "nosudo" {
		d.SUID, d.NoSudo = nil, true
	}
	for _, l := range s["php"] {
		switch kind, rest, _ := strings.Cut(l, " "); kind {
		case "upload":
			d.UploadPHP = append(d.UploadPHP, rest)
		case "sig":
			d.Signatures = append(d.Signatures, rest)
		case "changed":
			d.Changed, _ = strconv.Atoi(strings.TrimSpace(rest))
		}
	}
	for _, l := range s["dpkg"] {
		switch f := strings.Fields(l); {
		case l == "nodpkg":
			d.NoDpkg = true
		case l == "nosudo":
			d.NoSudo = true
		case len(f) >= 2 && strings.Contains(f[0], "5"):
			d.Modified = append(d.Modified, f[len(f)-1])
		}
	}
	return d
}

// EvaluateDeep: находки глубокой проверки.
func EvaluateDeep(d Deep) []health.Check {
	var f findings
	if len(d.Signatures) > 0 {
		f.add("веб-оболочка", level.Bad, d.Signatures[0], fmt.Sprintf("%d php с приметами веб-оболочки (eval от base64 или от $_POST): %s",
			len(d.Signatures), list(d.Signatures, 3)), "открой файл и посмотри; чужой - сайт взломан: убери, обнови CMS и плагины, смени пароли")
	}
	if len(d.UploadPHP) > 0 {
		f.add("файлы сайтов", level.Warn, "php в загрузках", fmt.Sprintf("php в папке загрузок: %s - туда кладут картинки, а не код",
			list(d.UploadPHP, 3)), "запрети php в uploads в nginx (location ~ /uploads/.*\\.php$ { deny all; }) и посмотри файлы")
	}
	if len(d.Modified) > 0 {
		f.add("файлы пакетов", level.Bad, list(d.Modified, 2), "файлы пакетов изменены не через пакетный менеджер: "+list(d.Modified, 5),
			"так подменяют ps, ss и sshd, чтобы спрятаться; переустанови пакет (apt install --reinstall) и считай сервер скомпрометированным")
	}
	if d.NoSudo {
		f.add("файлы пакетов", level.Skip, "не проверено", "dpkg --verify и SUID-файлы читает root: нужен sudo без пароля", "")
	}
	if len(f) == 0 {
		f.add("файлы сайтов", level.OK, "чисто", fmt.Sprintf("веб-оболочек нет, системные файлы целы; php в загрузках и изменённых за неделю: %d", d.Changed), "")
	}
	return f
}

func list(items []string, n int) string {
	out := strings.Join(items[:min(n, len(items))], ", ")
	if len(items) > n {
		out += fmt.Sprintf(" и ещё %d", len(items)-n)
	}
	return out
}
