package machine

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Защита Mac: то, что крадут и чем входят. Только чтение: состояние берётся у системных утилит, ключи
// ssh читаются, но не расшифровываются, автозапуск сравнивается с прошлым разом.

// MachineObject: свой Mac в хранилище "своего".
const MachineObject = "machine"

// dbKinds: базы, которым незачем слушать сеть на машине разработчика.
var dbKinds = []string{"MySQL", "MariaDB", "PostgreSQL", "Redis", "MongoDB", "Memcached", "Elasticsearch", "Meilisearch", "ClickHouse"}

// Protection: лампы защиты и находки.
type Protection struct {
	Lamps    []health.Check `json:"lamps"`
	Findings []health.Check `json:"findings"`
	New      []baseline.Item `json:"new,omitempty"`
}

// CheckProtection: защита этой машины; на Linux - только то, что не про macOS.
func CheckProtection(cfg *config.Config) Protection {
	var p Protection
	if runtime.GOOS == "darwin" {
		p.Lamps = macLamps()
		p.New = watchLaunch()
	}
	p.Findings = append(p.Findings, keyFindings()...)
	p.Findings = append(p.Findings, networkFindings(cfg)...)
	return p
}

func macLamps() []health.Check {
	lamp := func(name string, on bool, onText, offText string, good bool) health.Check {
		c := health.Check{Name: name, Text: offText, Level: level.OK}
		if on {
			c.Text = onText
		}
		if on != good {
			c.Level = level.Warn
		}
		return c
	}
	fv, _ := runner.Output("fdesetup", "status")
	fw, _ := runner.Output("/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate")
	gk, _ := runner.Output("spctl", "--status")
	return []health.Check{
		lamp("FileVault", strings.Contains(fv, "On"), "FileVault включён", "FileVault выключен", true),
		lamp("файрвол", strings.Contains(fw, "enabled"), "файрвол включён", "файрвол выключен", true),
		lamp("Gatekeeper", strings.Contains(gk, "enabled"), "Gatekeeper включён", "Gatekeeper выключен", true),
		lamp("удалённый вход", listens("22"), "удалённый вход включён", "удалённый вход выключен", false),
		lamp("общий экран", listens("5900"), "общий экран включён", "общий экран выключен", false),
	}
}

// listens: служба системы слушает порт; lsof без root чужих сокетов не видит, поэтому спрашиваем сам порт.
func listens(port string) bool {
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 300*time.Millisecond)
	if err == nil {
		_ = c.Close()
	}
	return err == nil
}

// keyFindings: закрытые ключи ssh без пароля - украдут файл, войдут везде, где лежит открытая часть.
func keyFindings() []health.Check {
	dir := filepath.Join(paths.Home(), ".ssh")
	files, _ := os.ReadDir(dir)
	var open []string
	for _, f := range files {
		path := filepath.Join(dir, f.Name())
		if f.IsDir() || strings.HasSuffix(f.Name(), ".pub") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.HasPrefix(data, []byte("-----BEGIN")) || encrypted(data) {
			continue
		}
		open = append(open, "~/.ssh/"+f.Name())
	}
	if len(open) == 0 {
		return nil
	}
	return []health.Check{{Name: "ключ ssh", Level: level.Warn, Text: strings.Join(open, ", "),
		Detail: strings.Join(open, ", ") + " без пароля: украдут файл - войдут на все серверы с этим ключом",
		Fix:    "ssh-keygen -p -f <ключ> задаст пароль, а ssh-add --apple-use-keychain запомнит его в связке"}}
}

// encrypted: закрытый ключ под паролем. У нового формата OpenSSH шифр записан в начале тела, у PEM -
// заголовком ENCRYPTED.
func encrypted(data []byte) bool {
	text := string(data)
	if strings.Contains(text, "ENCRYPTED") {
		return true
	}
	if !strings.Contains(text, "OPENSSH PRIVATE KEY") {
		return false
	}
	var body strings.Builder
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(l, "-----") {
			body.WriteString(strings.TrimSpace(l))
		}
	}
	raw, err := base64.StdEncoding.DecodeString(body.String())
	magic := "openssh-key-v1\x00"
	if err != nil || len(raw) < len(magic)+8 || string(raw[:len(magic)]) != magic {
		return false
	}
	n := int(raw[len(magic)])<<24 | int(raw[len(magic)+1])<<16 | int(raw[len(magic)+2])<<8 | int(raw[len(magic)+3])
	start := len(magic) + 4
	return start+n <= len(raw) && string(raw[start:start+n]) != "none"
}

// networkFindings: dev-серверы и базы, которые слушают все адреса - их видно в этой Wi-Fi.
func networkFindings(cfg *config.Config) []health.Check {
	var dev, db []string
	for _, r := range Ports(cfg) {
		if !r.Network {
			continue
		}
		switch {
		case slices.Contains(dbKinds, r.Kind):
			db = append(db, r.Kind+" :"+r.Port)
		case devServer(r):
			dev = append(dev, strings.ToLower(r.Kind)+" :"+r.Port)
		}
	}
	var out []health.Check
	if len(dev) > 0 {
		out = append(out, health.Check{Name: "в сети", Level: level.Warn, Text: strings.Join(dev, ", "),
			Detail: fmt.Sprintf("%s слушают все адреса: %s - их видно в этой Wi-Fi", ui.Count(len(dev), "dev-сервер", "dev-сервера", "dev-серверов"), strings.Join(dev, ", ")),
			Fix:    "запускать на 127.0.0.1: vite --host 127.0.0.1, php -S 127.0.0.1:8000, artisan serve --host=127.0.0.1"})
	}
	if len(db) > 0 {
		out = append(out, health.Check{Name: "базы в сети", Level: level.Warn, Text: strings.Join(db, ", "),
			Detail: strings.Join(db, ", ") + " " + ui.Plural(len(db), "слушает", "слушают", "слушают") + " все адреса - к базе можно постучаться из этой сети",
			Fix:    "bind-address = 127.0.0.1 (MySQL, MariaDB), listen_addresses = 'localhost' (PostgreSQL)"})
	}
	return out
}

// launchDirs: где лежит автозапуск - свой, всех пользователей и системные службы сторонних программ.
func launchDirs() []string {
	return []string{filepath.Join(paths.Home(), "Library", "LaunchAgents"), "/Library/LaunchAgents", "/Library/LaunchDaemons"}
}

// watchLaunch: автозапуск сравнивается с прошлым разом - новое держится, пока не отметишь своим.
func watchLaunch() []baseline.Item {
	items := []baseline.Item{}
	for _, d := range launchDirs() {
		files, _ := filepath.Glob(filepath.Join(d, "*.plist"))
		for _, f := range files {
			items = append(items, baseline.Item{Key: f, Text: paths.Show(f) + " · запускает " + launchProgram(f)})
		}
	}
	return baseline.Observe(MachineObject, baseline.Snapshot{"launch": items}, time.Now()).New
}

// launchProgram: что запускает plist - Program или первый из ProgramArguments.
func launchProgram(file string) string {
	out, err := runner.Output("plutil", "-convert", "json", "-o", "-", file)
	if err != nil {
		return "?"
	}
	var p struct {
		Program string   `json:"Program"`
		Args    []string `json:"ProgramArguments"`
	}
	if json.Unmarshal([]byte(out), &p) != nil {
		return "?"
	}
	if p.Program != "" {
		return paths.Show(p.Program)
	}
	if len(p.Args) > 0 {
		return paths.Show(p.Args[0])
	}
	return "?"
}

// PrintProtection: tim check --machine.
func PrintProtection(cfg *config.Config) error {
	p := CheckProtection(cfg)
	var lamps []string
	for _, l := range p.Lamps {
		lamps = append(lamps, level.Lamp(l.Level, l.Text))
	}
	if len(lamps) > 0 {
		ui.Println(strings.Join(lamps, "   "))
	}
	for _, it := range p.New {
		ui.Println(ui.Warn("▲ новый автозапуск ") + it.Text)
	}
	for _, f := range p.Findings {
		ui.Println(level.Lamp(f.Level, f.Detail))
		ui.Println("  " + ui.Dim("→ "+f.Fix))
	}
	if len(p.New) > 0 {
		ui.Println(ui.Dim("свои - отметить на экране Машина › Защита, space"))
	}
	if len(p.Findings) == 0 && len(p.New) == 0 {
		ui.Println(ui.Label("замечаний нет"))
	}
	return nil
}
