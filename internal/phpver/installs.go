package phpver

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Установленные версии: brew на macOS, /usr/bin/php8.x на Linux.

// Install: установленная версия PHP и папка с её бинарником.
type Install struct {
	Version Version
	Bin     string
	php     string // Linux: настоящий /usr/bin/php8.3, на него смотрит php в папке-шиме Bin
}

// PathEnv: PATH, в котором эта версия первая. На Linux перед этим заводится папка-шим.
func (i Install) PathEnv() string {
	if i.php != "" {
		i.shim()
	}
	return Path(i.Bin)
}

func (i Install) shim() {
	link := filepath.Join(i.Bin, "php")
	if _, err := os.Lstat(link); err == nil {
		return
	}
	if runner.DryRun {
		runner.Note("завёл бы " + link + " -> " + i.php)
		return
	}
	if os.MkdirAll(i.Bin, 0o755) == nil {
		_ = os.Symlink(i.php, link)
	}
}

// Installs: версии PHP из brew; у формулы php версия спрашивается у самого бинарника.
// На Linux - /usr/bin/php8.3 и соседи (Debian, Ubuntu, PPA ondrej).
func Installs() []Install {
	if runtime.GOOS == "linux" {
		return linuxInstalls()
	}
	if !runner.Has("brew") {
		return nil
	}
	prefix := brew.Prefix()
	var out []Install
	for _, f := range brew.Formulas() {
		if f != "php" && !strings.HasPrefix(f, "php@") {
			continue
		}
		bin := filepath.Join(prefix, "opt", f, "bin")
		var v Version
		var ok bool
		if f == "php" {
			s, _ := runner.Output(filepath.Join(bin, "php"), "-r", "echo PHP_VERSION;")
			v, ok = Parse(s)
		} else {
			v, ok = Parse(strings.TrimPrefix(f, "php@"))
		}
		if ok {
			out = append(out, Install{Version: v, Bin: bin})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[j].Version.Less(out[i].Version) })
	return out
}

// linuxInstalls: версии из /usr/bin. Бинарник там называется php8.3, а не php, поэтому для запуска
// нужна папка-шим с php внутри: её ставим первой в PATH, а заводит её PathEnv.
func linuxInstalls() []Install {
	bins, _ := filepath.Glob("/usr/bin/php[0-9]*.[0-9]*")
	var out []Install
	for _, b := range bins {
		v, ok := Parse(strings.TrimPrefix(filepath.Base(b), "php"))
		if !ok {
			continue
		}
		out = append(out, Install{Version: v, Bin: paths.State("php", v.String()), php: b})
	}
	sort.Slice(out, func(i, j int) bool { return out[j].Version.Less(out[i].Version) })
	return out
}

func Hint(v string) string { return runner.InstallHint("php@"+v, "php"+v+"-cli") }

// Find: установленный php нужной версии, сравнение по major.minor.
func Find(want string) (Install, error) {
	w, ok := Parse(want)
	if !ok {
		return Install{}, ui.Errorf("версия вида 8.3: %s", want)
	}
	for _, i := range Installs() {
		if i.Version.Major == w.Major && i.Version.Minor == w.Minor {
			return i, nil
		}
	}
	return Install{}, ui.Errorf("php %s не установлен: %s", w, Hint(w.String()))
}

// Path: PATH без каталогов других версий brew php и с нужной версией первой.
func Path(bin string) string {
	keep := []string{bin, filepath.Join(filepath.Dir(bin), "sbin")}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.Contains(d, "/opt/php") || strings.Contains(d, "/Cellar/php") {
			continue
		}
		keep = append(keep, d)
	}
	return strings.Join(keep, string(os.PathListSeparator))
}
