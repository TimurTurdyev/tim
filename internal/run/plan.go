package run

import (
	"path/filepath"
	"strconv"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/envfile"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/phpver"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Plan: как запустить проект - папка, порт, команда и пояснение (стек или версия php).
type Plan struct {
	Project *config.Project
	Dir     string
	Port    string
	Cmd     Command
	Note    string
}

// PHPEnv: если активный php не подходит под require.php проекта, PATH с подходящей версией из brew.
func PHPEnv(dir, stack string) ([]string, string) {
	if stack != "laravel" && stack != "php" {
		return nil, ""
	}
	req := phpver.Required(filepath.Join(dir, "composer.json"))
	if req == "" {
		return nil, ""
	}
	if active, err := runner.Output("php", "-r", "echo PHP_VERSION;"); err == nil {
		if v, ok := phpver.Parse(active); ok && phpver.Satisfies(v, req) {
			return nil, ""
		}
	}
	for _, i := range phpver.Installs() {
		if phpver.Satisfies(i.Version, req) {
			return []string{"PATH=" + i.PathEnv()}, "php " + i.Version.String() + " под require.php " + req
		}
	}
	return nil, "подходящего под " + req + " php нет: " + phpver.Hint("<версия>")
}

// ProjectPort: порт из реестра, иначе из APP_URL в .env, иначе netinfo.DefaultPort.
func ProjectPort(p *config.Project, dir string) string {
	if p.Port > 0 {
		return strconv.Itoa(p.Port)
	}
	if u, ok := envfile.Get(filepath.Join(dir, ".env"), "APP_URL"); ok {
		if port := envfile.Port(u); port != "" {
			return port
		}
	}
	return netinfo.Port(netinfo.DefaultPort)
}

// NewPlan: команда запуска по стеку и полю serve, с подходящим php в PATH.
func NewPlan(p *config.Project, dir, port string) (Plan, error) {
	facts := Detect(dir)
	facts.Custom = p.Serve
	c, err := Serve(facts, port)
	if err != nil {
		return Plan{}, ui.Errorf("%s: %v", p.Name, err)
	}
	env, note := PHPEnv(dir, facts.Stack)
	c.Env = append(c.Env, env...)
	switch {
	case note != "":
	case p.Serve != "":
		note = "своя команда"
	default:
		note = facts.Stack
	}
	return Plan{Project: p, Dir: dir, Port: port, Cmd: c, Note: note}, nil
}

// Servable: проект можно поднять через serve - только таким нужен свой порт.
func Servable(dir string) bool {
	_, err := Serve(Detect(dir), netinfo.Port(netinfo.DefaultPort))
	return err == nil
}
